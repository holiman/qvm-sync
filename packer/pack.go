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

type Sender struct {
	out      BufferedWriter
	in       io.Reader
	sendList []string
	root     string

	// Options
	opts *Options

	// stats
	rawCounter  *MeteredWriter
	snapCounter *MeteredWriter
}

const regularOrSymlink = os.ModeDir | os.ModeNamedPipe | os.ModeSocket |
	os.ModeDevice | os.ModeIrregular

func NewSender(out io.Writer, in io.Reader, opts *Options) (*Sender, error) {
	if opts == nil {
		opts = DefaultOptions
	}
	if opts.CrcUsage > FileCrcAtimeNsecMetadata {
		return nil, fmt.Errorf("Unsupported crc usage: %d", opts.CrcUsage)
	}
	if opts.Compression > CompressionSnappy {
		return nil, fmt.Errorf("Unsupported compression format %d", opts.Compression)
	}
	var sender = &Sender{
		opts: opts,
		out:  NewConfigurableWriter(opts.Compression == CompressionSnappy, out),
	}
	// We still have the un-modified 'out', and can send the first packet
	// without compression
	v := newVersionHeader(opts.Compression, opts.CrcUsage, opts.Verbosity)
	if err := v.marshallBinary(out); err != nil {
		return nil, err
	}
	if opts.Compression == CompressionSnappy {
		in = snappy.NewReader(in)
	}
	sender.in = in
	return sender, nil
}

func (s *Sender) Sync(path string) error {
	if err := s.transmitDirectory(path); err != nil {
		return fmt.Errorf("phase 0 send error: %v", err)
	}
	if err := s.waitForResult(); err != nil {
		return fmt.Errorf("phase 1 wait error: %v", err)
	}
	if err := s.handleFileList(); err != nil {
		return fmt.Errorf("phase 2 list error: %v", err)
	}
	if err := s.waitForResult(); err != nil {
		return fmt.Errorf("phase 3 wait error: %v", err)
	}
	if s.opts.Verbosity >= 3 {
		if cm, ok := s.out.(*ConfigurableWriter); ok {
			r, c := cm.Stats()
			log.Printf("Data sent, raw: %d, compresed: %d", r, c)
		}
	}
	return nil
}

// sendItemMetadata sends the list of files and directories
// it remembers the paths of each file sent
func (s *Sender) sendItemMetadata(path string, info os.FileInfo) error {
	header := newFileHeaderFromStat(path, info)

	// Possibly replace atimensec with crc32
	if !header.isDir() {
		fullPath := filepath.Join(s.root, path)
		if s.opts.CrcUsage == FileCrcAtimeNsec ||
			s.opts.CrcUsage == FileCrcAtimeNsecMetadata {
			crc, err := CrcFile(fullPath, info)
			if err != nil {
				return fmt.Errorf("crc failed: %v", err)
			}
			header.Data.AtimeNsec = crc
		}
	}
	header.marshallBinary(s.out)
	if info.Mode()&regularOrSymlink == 0 {
		// Files and symlinks can be requested later
		s.sendList = append(s.sendList, path)
	}
	return nil
}

// sendItem transmits the actual file content of the file at the
// given index. It transmits the file with the full header,
// not just the content.
func (s *Sender) sendItem(index uint32) error {
	if index >= uint32(len(s.sendList)) {
		return fmt.Errorf("index %d not in list (length %d)", index, len(s.sendList))
	}
	var (
		filename  = s.sendList[index]
		path      = filepath.Join(s.root, filename)
		info, err = os.Lstat(path)
	)
	if err != nil {
		return fmt.Errorf("file %v no longer available: %v", filename, err)
	}
	if s.opts.Verbosity >= 4 {
		log.Printf("Sending file %v", filename)
	}
	header := newFileHeaderFromStat(filename, info)
	// Possibly replace atimensec with crc32
	if header.isRegular() && s.opts.CrcUsage == FileCrcAtimeNsec {
		crc, err := CrcFile(path, info)
		if err != nil {
			return err
		}
		header.Data.AtimeNsec = crc
	}
	if err := header.marshallBinary(s.out); err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		var data string
		data, err = os.Readlink(filepath.Join(s.root, filename))
		if err != nil {
			return err
		}
		_, err = s.out.Write([]byte(data))
	} else if info.Mode().IsRegular() {
		// file Data
		var file *os.File
		file, err = os.Open(filepath.Join(s.root, filename))
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(s.out, file)
	}
	return err
}

// transmitDirectory resolves the given dirname to a directory, and syncs that directory
func (s *Sender) transmitDirectory(dirname string) error {

	absPath, _ := filepath.Abs(filepath.Clean(dirname))
	root, path := filepath.Split(absPath)
	if s.opts.Verbosity >= 3 {
		log.Printf("Root: %v, sync dir: %v", root, path)
	}
	stat, err := os.Lstat(absPath)
	if err != nil {
		return err
	}
	// Check that it actually is a directory
	if !stat.IsDir() {
		return fmt.Errorf("%v is not a directory", dirname)
	}
	s.root = root
	if err := s.osWalk(path, stat); err != nil {
		return err
	}
	// send ending
	if s.opts.Verbosity >= 5 {
		log.Print("Sending EOD (2)")
	}
	if _, err = s.out.Write(make([]byte, 32)); err != nil {
		return err
	}
	if err := s.out.Flush(); err != nil {
		return err
	}
	if cm, ok := s.out.(*ConfigurableWriter); ok {
		r, c := cm.Stats()
		log.Printf("Data sent, raw: %d, compressed: %d", r, c)
	}
	return nil
}

func (s *Sender) osWalk(path string, stat os.FileInfo) error {

	if s.opts.IgnoreSymlinks && (stat.Mode()&os.ModeSymlink != 0) {
		return nil
	}
	if s.opts.Verbosity >= 5 {
		log.Printf("Sending metadata for %v", path)
	}
	if err := s.sendItemMetadata(path, stat); err != nil {
		return err
	}
	if !stat.IsDir() {
		return nil
	}
	files, err := ioutil.ReadDir(filepath.Join(s.root, path))
	if err != nil {
		return err
	}
	for _, finfo := range files {
		fName := filepath.Join(path, finfo.Name())
		if err := s.osWalk(fName, finfo); err != nil {
			return err
		}
	}
	// resend directory info
	if s.opts.Verbosity >= 5 {
		log.Printf("Sending metadata (2) for %v", path)
	}
	stat, _ = os.Lstat(filepath.Join(s.root, path))
	if err = s.sendItemMetadata(path, stat); err != nil {
		return err
	}
	return nil
}

func (s *Sender) waitForResult() error {
	hdr := new(resultHeader)
	if err := hdr.unMarshallBinary(s.in); err != nil {
		return err
	}
	hdrExt := new(resultHeaderExt)
	if err := hdrExt.unMarshallBinary(s.in); err != nil {
		return err
	}
	if hdr.ErrorCode != 0{
		return fmt.Errorf("sync error, code: %v , last file: %v", hdr.ErrorCode, hdrExt.LastName)
	}
	if s.opts.Verbosity >= 3 {
		log.Printf("Got result ACK, last file %v",  hdrExt.LastName)
	}
	return nil
}

func (s *Sender) handleFileList() error {

	var listLen uint32
	if err := binary.Read(s.in, binary.LittleEndian, &listLen); err != nil {
		return err
	}
	if max := uint32(len(s.sendList)); listLen > max {
		return fmt.Errorf("remote requested %d items, only %d possible", listLen, max)
	}
	var list = make([]uint32, listLen)
	if err := binary.Read(s.in, binary.LittleEndian, &list); err != nil {
		return err
	}
	if s.opts.Verbosity >= 3 {
		log.Printf("Got list, %d items requested", len(list))
	}
	for _, index := range list {
		// index starts at 1
		if err := s.sendItem(index); err != nil {
			return err
		}
	}
	return s.out.Flush()
}
