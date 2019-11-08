package packer

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

type Sender struct {
	out            io.Writer
	in             io.Reader
	ignoreSymlinks bool
	sendList       []string
	root           string
}

const regularOrSymlink = os.ModeDir | os.ModeNamedPipe | os.ModeSocket |
	os.ModeDevice | os.ModeIrregular

func NewSender(out io.Writer, in io.Reader, ignoreSymlinks bool) *Sender {
	return &Sender{out: out, in: in, ignoreSymlinks: ignoreSymlinks}
}

func (s *Sender) Sync(path string) error {
	if err := s.OsWalk(path); err != nil {
		return fmt.Errorf("phase 0 send error: %v", err)
	}

	if err := s.WaitForResult(); err != nil {
		return fmt.Errorf("phase 1 wait error: %v", err)
	}
	if err := s.HandleFileList(); err != nil {
		return fmt.Errorf("phase 2 list error: %v", err)
	}
	if err := s.WaitForResult(); err != nil {
		return fmt.Errorf("phase 3 wait error: %v", err)
	}
	return nil
}

// sendItemMetadata sends the list of files and directories
// it remembers the paths of each file sent
func (s *Sender) sendItemMetadata(path string, info os.FileInfo) error {
	header := newFileHeaderFromStat(path, info)
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
		info, err = os.Lstat(filepath.Join(s.root, filename))
	)
	if err != nil {
		return fmt.Errorf("file %v no longer available: %v", filename, err)
	}
	log.Printf("Sending file %v", filename)
	header := newFileHeaderFromStat(filename, info)
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

// OsWalk resolves the given dirname to a directory, and syncs that directory
func (s *Sender) OsWalk(dirname string) error {

	absPath, _ := filepath.Abs(filepath.Clean(dirname))
	root, path := filepath.Split(absPath)
	log.Printf("Root: %v, sync dir: %v", root, path)
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
	_, err = s.out.Write(make([]byte, 32))
	return err
}

func (s *Sender) osWalk(path string, stat os.FileInfo) error {

	if s.ignoreSymlinks && (stat.Mode()&os.ModeSymlink != 0) {
		return nil
	}
	log.Printf("Sending metadata for %v", path)
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
	stat, _ = os.Lstat(filepath.Join(s.root, path))
	if err = s.sendItemMetadata(path, stat); err != nil {
		return err
	}
	return nil
}

func (s *Sender) WaitForResult() error {
	hdr := new(resultHeader)
	if err := hdr.unMarshallBinary(s.in); err != nil {
		return err
	}
	hdrExt := new(resultHeaderExt)
	if err := hdrExt.unMarshallBinary(s.in); err != nil {
		return err
	}
	switch hdr.ErrorCode {
	case 17: // EEXIST:
		return fmt.Errorf("A file named %s already exists in QubesIncoming dir", hdrExt.LastName)
	case 22: // EINVAL
		return fmt.Errorf("File copy: Corrupted Data from packer (last file %v)", hdrExt.LastName)
	case 0:
		break
	default:
		return fmt.Errorf("File copy: error code %v , %v", hdr.ErrorCode, hdrExt.LastName)
	}
	log.Printf("Crc [%x] checking ignored (todo!). Last file was %v", hdr.Crc32, hdrExt.LastName)
	return nil
}

func (s *Sender) HandleFileList() error {

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
	log.Printf("Got list, %d items requested", len(list))

	for _, index := range list {
		// index starts at 1
		if err := s.sendItem(index); err != nil {
			return err
		}
	}
	return nil
}
