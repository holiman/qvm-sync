package packer

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"time"
)

const (
	// The original qvm-copy uses LongMax (9223372036854775807 = 9223 PB) as
	// max. I choose something smaller, 1TB ought to suffice
	MaxTransfer = 1e12
)

// fixTimesAndPerms set permissions on a the given file/directory according to
// the fileHeader
//
// Setting permissions doesn't work on symlinks. Chmod docs:
//
// > If the file is a symbolic link, it changes the mode of the link's target.
//
// And similarly, it's not possible to do ChTimes on a symlink, as golang will always
// resolve the symlinks, see https://github.com/golang/go/issues/3951
//
// - Invoking os.Chtimes on a symlink that resolves to some existing file will
//   in actuality change the other file.
// - Invoking os.Chtimes on a symlink that doesn't resolve to an existing file at
//   all, will return an error (no such file or directory).
func fixTimesAndPerms(hdr *fileHeader) error {
	if err := os.Chmod(hdr.path, os.FileMode(hdr.Data.Mode&07777)); err != nil {
		return err
	}
	atime := time.Unix(int64(hdr.Data.Atime), int64(hdr.Data.AtimeNsec))
	mtime := time.Unix(int64(hdr.Data.Mtime), int64(hdr.Data.MtimeNsec))
	return os.Chtimes(hdr.path, atime, mtime)
}

type Receiver struct {
	in  io.Reader
	out io.Writer

	useTempFile bool // Should it unpack into tempfiles first?
	verbose     bool // verbose output

	totalBytes uint64 // counter for total bytes received
	totalFiles uint64 // counter for total files received

	filesLimit int    // a limit on the number of files to receive
	byteLimit  uint64 // limit on the number of bytes to receive

	index       uint32   // index count,for requesting
	requestList []uint32 // list of files (indexes) to request

	dirStack []string // stack of directories we visit/create

	// place to store stuff in. Defaults to empty string, as we're normally
	// root-jailed, but is used for testing
	root string
}

// NewReceiver creates a new receiver
func NewReceiver(in io.Reader, out io.Writer, verbose bool) *Receiver {
	return &Receiver{
		in:          in,
		out:         out,
		filesLimit:  -1,
		useTempFile: true,
		verbose:     verbose,
	}
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
	if !localFile.Eq(hdr) {
		r.request(r.index)
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
	if r.visitDir(header.path) {
		//abs, _ := filepath.Abs(header.path)
		// first visit
		if stat, err := os.Lstat(header.path); err == nil {
			// directory already exists -- make sure it's a dir -- otherwise delete
			if stat.IsDir() {
				return nil // TODO: consider if we should change perms to 0700 here..?
			}
			if err := RemoveIfExist(header.path); err != nil {
				return err
			}
		}
		// Dir did not exist (or was removed)
		return os.Mkdir(header.path, 0700)
	}
	log.Printf("Fixing perms for %v", header.path)
	// second visit
	// we fix the perms after we're done with it
	return fixTimesAndPerms(header)
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
		// TODO, handle if file already exist
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
		fixTimesAndPerms(hdr)
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
	return fixTimesAndPerms(hdr)
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
	//if r.verbose && len(hdr.path) > 0 {
	//	log.Printf("%s", hdr.path)
	//}
	return err
}

func (r *Receiver) ReceiveMetadata() error {
	var lastName string
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
		if err := r.processItemMetadata(hdr); err != nil {
			return err
		} else {
			lastName = hdr.path
		}
	}
	return r.sendStatusAndCrc(0, lastName)
}

func (r *Receiver) ReceiveFullData() error {
	lastName := ""
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
		log.Printf("Got file %d (%v)", index, lastName)
	}
	return r.sendStatusAndCrc(0, lastName)

}

func (r *Receiver) sendStatusAndCrc(code int, lastFilename string) error {
	result := resultHeader{
		ErrorCode: uint32(code),
		Pad:       0,
		Crc32:     0xdeadc0de,
	}
	if err := result.marshallBinary(r.out); err != nil {
		return err
	}
	if len(lastFilename) == 0 {
		return nil
	}
	extension := &resultHeaderExt{
		LastNameLen: uint32(len(lastFilename)) + 1,
		LastName:    lastFilename,
	}
	if err := extension.marshallBinary(r.out); err != nil {
		return fmt.Errorf("failed sending result extension: %v", err)
	}
	return nil
}

func (r *Receiver) RequestFiles() error {
	log.Printf("Requesting files %d", r.requestList)
	if err := binary.Write(r.out, binary.LittleEndian, uint32(len(r.requestList))); err != nil {
		return err
	}
	if err := binary.Write(r.out, binary.LittleEndian, r.requestList); err != nil {
		return err
	}
	return nil
}
