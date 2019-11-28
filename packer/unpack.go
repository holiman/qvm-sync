package packer

import (
	"encoding/binary"
	"fmt"
	"github.com/golang/snappy"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

const (
	// The original qvm-copy uses LongMax (9223372036854775807 = 9223 PB) as
	// max. I choose something smaller, 1TB ought to suffice
	MaxTransfer = 1e12
)

type Receiver struct {
	in  io.Reader
	out BufferedWriter

	useTempFile bool // Should it unpack into tempfiles first?

	totalBytes uint64 // counter for total bytes received
	totalFiles uint64 // counter for total files received

	filesLimit int    // a limit on the number of files to receive
	byteLimit  uint64 // limit on the number of bytes to receive

	index       uint32              // index count,for requesting
	requestList []uint32            // list of files (indexes) to request
	toDelete    map[string]struct{} // list of local files to delete

	dirStack []string // stack of directories we visit/create

	// place to store stuff in. Defaults to empty string, as we're normally
	// root-jailed, but is used for testing
	root string

	opts *Options
}

// NewReceiver creates a new receiver
func NewReceiver(in io.Reader, out io.Writer) (*Receiver, error) {
	v := versionHeader{}
	if err := binary.Read(in, binary.LittleEndian, &v); err != nil {
		return nil, err
	}
	if v.Version != 0 {
		return nil, fmt.Errorf("unsupported version: %d", v.Version)
	}
	opts := &Options{
		Verbosity:   int(v.Verbosity),
		CrcUsage:    int(v.FileCrcUsage),
		Compression: int(v.Compression),
	}
	if opts.Compression > CompressionSnappy {
		return nil, fmt.Errorf("Unsupported compression format %d", opts.Compression)
	}
	if opts.Compression == CompressionSnappy {
		in = snappy.NewReader(in)
	}
	if opts.Verbosity >= 3 {
		log.Printf("protocol version: %d, verbosity %d, snappy: %v, crc: %d",
			v.Version, opts.Verbosity, opts.Compression != 0, opts.CrcUsage)
	}
	return &Receiver{
		in:          in,
		out:         NewConfigurableWriter(opts.Compression == CompressionSnappy, out),
		filesLimit:  -1,
		useTempFile: true,
		opts:        opts,
		toDelete:    make(map[string]struct{}),
	}, nil
}

func (r *Receiver) Sync() error {
	// Receive directories + metadata
	if err := r.receiveMetadata(); err != nil {
		return fmt.Errorf("Error during phase 0 receive : %v", err)
	}
	// Request files
	if err := r.requestFiles(); err != nil {
		return fmt.Errorf("Error during phase 2 file request: %v", err)
	}
	// Receive data content
	if err := r.receiveFullData(); err != nil {
		return fmt.Errorf("Error during file reception: %v", err)
	}
	if r.opts.Verbosity >= 3 {
		if cm, ok := r.out.(*ConfigurableWriter); ok {
			r, c := cm.Stats()
			log.Printf("Data sent, raw: %d, compresed: %d", r, c)
		}
	}
	for f, _ := range r.toDelete {
		info, err := os.Lstat(f)
		if err != nil {
			log.Printf("Error during deletion: %v", err)
			continue
		}
		if info.IsDir() {
			os.RemoveAll(f)
			if r.opts.Verbosity >= 4 {
				log.Printf("Removed directory %v", f)
			}
		} else {
			if err := os.Remove(f); err != nil {
				if r.opts.Verbosity > 0 {
					log.Printf("Failed to delete %v: %v", f, err)
				}
			}
			if r.opts.Verbosity >= 4 {
				log.Printf("Removed %v", f)
			}
		}
	}
	return nil
}

// request schedules a certain index for later retrieval
func (r *Receiver) request(index uint32) {
	r.requestList = append(r.requestList, r.index)
}

// countBytes verifies that the length is within limits, and updates bytecounter
func (r *Receiver) countBytes(length uint64, update bool) error {
	if length > MaxTransfer {
		return fmt.Errorf("file too large, %d", length)
	}
	if r.byteLimit != 0 && r.totalBytes > uint64(r.byteLimit)-length {
		return fmt.Errorf("file too large, %d", length)
	}
	if update {
		r.totalBytes += length
	}
	return nil
}

// receiveFileMetadata handles stage-1 metadata for files and symlinks
func (r *Receiver) receiveFileMetadata(hdr *fileHeader) error {
	defer func() { r.index++ }()
	// Check sizes
	if err := r.countBytes(hdr.Data.FileLen, false); err != nil {
		return err
	}
	localFileInfo, err := os.Lstat(hdr.path)
	if err != nil && os.IsNotExist(err) {
		r.request(r.index)
		return nil
	}
	localFile := newFileHeaderFromStat(hdr.path, localFileInfo)
	if diff := localFile.Diff(hdr); len(diff) > 0 {
		if r.opts.Verbosity >= 4 {
			log.Printf("file diffs for %v: %v", hdr.path, diff)
		}
		r.request(r.index)
	}
	if r.opts.CrcUsage == FileCrcAtimeNsecMetadata ||
		r.opts.CrcUsage == FileCrcAtimeNsec {
		crc, err := CrcFile(hdr.path, localFileInfo)
		if err != nil {
			return err
		}
		if crc != hdr.Data.AtimeNsec {
			if r.opts.Verbosity >= 3 {
				log.Printf("crc diff on %v (local %d, remote %d)",
					hdr.path, crc, hdr.Data.AtimeNsec)
			}
			r.request(r.index)
		}
	}
	return nil
}

// receiveDirMetadata handles directories (stage 1). Since qvm-sync, as opposed to qvm-copy,
// cannot rely on the destination being empty, we need to handle various
// corner cases (e.g directory exists but is file, or vice versa)
func (r *Receiver) receiveDirMetadata(header *fileHeader) error {
	// qvm-copy operates on a 'clean' empty destination, so that one can
	// safely assume that if it already exists, this is the second time they
	// visit it (backing out), and set the final perms that time around.
	// We can't do that, but instead maintain a stack of directories. We
	// can consult it to find it if
	// 1. we're now backing out of a dir, or,
	// 2. We're visiting/creating one for the first time
	if r.visitDir(header.path) { // first visit
		if stat, err := os.Lstat(header.path); err == nil {
			// directory already exists -- make sure it's a dir -- otherwise delete
			if stat.IsDir() {
				// remember the files that were there
				if err := r.snapshotFiles(header.path, false); err != nil {
					return err
				}
				return nil // TODO: consider if we should change perms to 0700 here..?
			}
			// It was a file, on the local system
			if err := RemoveIfExist(header.path); err != nil {
				return err
			}
		}
		// Dir did not exist (or was removed)
		return os.Mkdir(header.path, 0700)
	}
	if r.opts.Verbosity >= 5 {
		log.Printf("Fixing perms for %v", header.path)
	}
	// second visit
	// we fix the perms after we're done with it
	return header.fixTimesAndPerms()
}

func (r *Receiver) receiveRegularFileFullData(hdr *fileHeader) error {
	// Check sizes
	if err := r.countBytes(hdr.Data.FileLen, true); err != nil {
		return err
	}
	var (
		fdOut *os.File
		err   error
	)
	if !r.useTempFile {
		if fdOut, err = os.OpenFile(hdr.path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0); err != nil {
			return err
		}
		// we can't do deferred fdOut.Close, because we need to fix perms
		// _after_ file has been closed
		if err := CopyFile(r.in, fdOut, int(hdr.Data.FileLen)); err != nil {
			fdOut.Close()
			return err
		}
		fdOut.Close()
		if err := hdr.fixTimesAndPerms(); err != nil {
			return err
		}
	}
	// Create tempfile
	if fdOut, err = ioutil.TempFile(".", "qvm-*"); err != nil {
		return err
	}
	defer fdOut.Close()
	defer os.Remove(fdOut.Name()) // defer cleanup
	if err := CopyFile(r.in, fdOut, int(hdr.Data.FileLen)); err != nil {
		return err
	}
	// This file may already exist.
	if err := RemoveIfExist(hdr.path); err != nil {
		return err
	}
	if err := os.Link(fdOut.Name(), hdr.path); err != nil {
		return fmt.Errorf("unable to link file : %v", err)
	}
	return hdr.fixTimesAndPerms()
}

func (r *Receiver) receiveSymlinkFullData(hdr *fileHeader) error {
	fileSize := hdr.Data.FileLen
	if fileSize > MaxPathLength-1 {
		return fmt.Errorf("symlink link-name too long (%d characters)", fileSize)
	}
	if err := r.countBytes(fileSize, true); err != nil {
		return err
	}
	// a symlink should be small enough to not use CopyFile (buffered)
	buf := make([]byte, fileSize)
	if _, err := io.ReadFull(r.in, buf); err != nil {
		return fmt.Errorf("symlink content read err: %v", err)
	}
	content := string(buf)
	// This file may already exist.
	RemoveIfExist(hdr.path)
	if err := os.Symlink(content, hdr.path); err != nil {
		return err
	}
	// OBS! We can't set perms _nor_ times on symlinks. See documentation
	// on the methods fixTimesAndPerms and fixTimes
	return nil
}

// visitDir either push the path to the stack, or, if the topmost item
// is identical to this path, it pops one item from the stack.
// @return true if this is a new path (push), false if it's the second time around (pop)
func (r *Receiver) visitDir(path string) bool {
	if len(r.dirStack) == 0 {
		r.dirStack = append(r.dirStack, path)
		return true
	}
	if r.dirStack[len(r.dirStack)-1] != path {
		r.dirStack = append(r.dirStack, path)
		return true
	}
	r.dirStack = r.dirStack[:len(r.dirStack)-1]
	return false
}

func (r *Receiver) processItemMetadata(hdr *fileHeader) error {
	var err error
	if hdr.isDir() {
		err = r.receiveDirMetadata(hdr)
	} else if hdr.isSymlink() || hdr.isRegular() {
		err = r.receiveFileMetadata(hdr)
	} else {
		return fmt.Errorf("unknown file Mode %x", hdr.Data.Mode)
	}
	return err
}

func (r *Receiver) snapshotFiles(dir string, checkRoot bool) error {
	// Build up the list of existing files (on the current directory level)
	files, err := ioutil.ReadDir(dir)
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, f := range files {
		fullPath, err := filepath.Abs(filepath.Join(dir, f.Name()))
		if err != nil {
			return err
		}
		r.toDelete[fullPath] = struct{}{}
	}
	// We are supposed to be chrooted, and therefore unable to actually
	// delete files arbitrarily. However, better safe than sorry, so this
	// program will simply throw an error if it "looks like" we're not in a
	// chroot but in an actual root
	if checkRoot {
		blackList := []string{
			"bin", "boot", "dev", "etc", "home", "lost+found",
			"media", "mnt", "opt", "proc", "root",
			"sbin", "srv", "sys", "usr", "var",
		}
		for _, nope := range blackList {
			if _, exist := r.toDelete[filepath.Join(dir, nope)]; exist {
				return fmt.Errorf("file %v in receiver root, bailing out", nope)
			}
		}
	}
	return nil
}

func (r *Receiver) removeSnapshot(path string) error {
	fullpath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	delete(r.toDelete, fullpath)
	return nil
}

func (r *Receiver) receiveMetadata() error {
	var lastName string
	if err := r.snapshotFiles("./", true); err != nil {
		return fmt.Errorf("snapshot failed: %v", err)
	}
	for {
		hdr, err := unMarshallBinary(r.in)
		if err != nil {
			return err
		}
		// Check for end of transfer marker
		if hdr.Data.NameLen == 0 {
			break
		}
		r.totalFiles++
		if r.filesLimit > 0 && int(r.totalFiles) > r.filesLimit {
			return fmt.Errorf("number of files (%d) exceeded limit (%d)", r.totalFiles, r.filesLimit)
		}
		r.removeSnapshot(hdr.path)
		if err := r.processItemMetadata(hdr); err != nil {
			return fmt.Errorf("error processing metadata for %v: %v", hdr.path, err)
		} else {
			lastName = hdr.path
		}
	}
	if err := r.sendStatusAndCrc(0, lastName); err != nil {
		return err
	}
	return r.out.Flush()
}

func (r *Receiver) receiveFullData() error {
	var lastName string
	for _, index := range r.requestList {
		hdr, err := unMarshallBinary(r.in)
		if err != nil {
			return err
		}
		if hdr.isRegular() {
			err = r.receiveRegularFileFullData(hdr)
		} else if hdr.isSymlink() {
			err = r.receiveSymlinkFullData(hdr)
		}
		if err != nil {
			return err
		}
		lastName = hdr.path
		if r.opts.Verbosity >= 4 {
			log.Printf("Got file %d (%v)", index, lastName)
		}
	}
	if err := r.sendStatusAndCrc(0, lastName); err != nil {
		return err
	}
	return r.out.Flush()
}

func (r *Receiver) sendStatusAndCrc(code int, lastFilename string) error {
	result := &resultHeader{
		ErrorCode: uint32(code),
	}
	if err := result.marshallBinary(r.out); err != nil {
		return err
	}
	extension := &resultHeaderExt{
		LastNameLen: uint32(len(lastFilename)) + 1,
		LastName:    lastFilename,
	}
	if len(lastFilename) == 0 {
		extension.LastNameLen = 0
	}
	if err := extension.marshallBinary(r.out); err != nil {
		return fmt.Errorf("failed sending result extension: %v", err)
	}
	return nil
}

func (r *Receiver) requestFiles() error {
	if r.opts.Verbosity >= 3 {
		log.Printf("Requesting files %d", r.requestList)
	}
	if err := binary.Write(r.out, binary.LittleEndian, uint32(len(r.requestList))); err != nil {
		return err
	}
	if err := binary.Write(r.out, binary.LittleEndian, r.requestList); err != nil {
		return err
	}
	return r.out.Flush()
}
