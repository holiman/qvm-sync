package packer

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"syscall"
)

const (
	MaxPathLength = 16384
)

type fileHeader struct {
	Data fileHeaderData
	path string
}
type fileHeaderData struct {
	NameLen   uint32
	Mode      uint32
	FileLen   uint64
	Atime     uint32
	AtimeNsec uint32
	Mtime     uint32
	MtimeNsec uint32
}

func newFileHeaderFromStat(path string, info os.FileInfo) *fileHeader {
	stat := info.Sys().(*syscall.Stat_t)
	data := fileHeaderData{
		Mode:      uint32(info.Mode()),
		Mtime:     uint32(stat.Mtim.Sec),
		MtimeNsec: uint32(stat.Mtim.Nsec),
		Atime:     uint32(stat.Atim.Sec),
		AtimeNsec: uint32(stat.Atim.Nsec),
		FileLen:   uint64(stat.Size),
		NameLen:   uint32(len(path) + 1),
	}
	if info.Mode().IsDir() {
		data.FileLen = 0
	}
	return &fileHeader{
		path: path,
		Data: data,
	}
}

func (hdr *fileHeader) marshallBinary(out io.Writer) error {
	if err := binary.Write(out, binary.LittleEndian, hdr.Data); err != nil {
		return err
	}
	if err := WritePath(out, hdr.path); err != nil {
		return err
	}
	return nil
}

func unMarshallBinary(reader io.Reader) (*fileHeader, error) {
	var data fileHeaderData
	if err := binary.Read(reader, binary.LittleEndian, &data); err != nil {
		return nil, err
	}
	path, err := ReadPath(reader, data.NameLen)
	if err != nil {
		return nil, err
	}
	return &fileHeader{
		path: path,
		Data: data,
	}, nil
}

func (hdr *fileHeader) Eq(other *fileHeader) bool {
	var errs []string
	if a, b := hdr.Data.NameLen, other.Data.NameLen; a != b {
		errs = append(errs, fmt.Sprintf("NameLen %d != %d", a, b))
	}
	if a, b := hdr.Data.Mode, other.Data.Mode; a != b {
		errs = append(errs, fmt.Sprintf("Mode %x != %x", a, b))
	}
	if a, b := hdr.Data.FileLen, other.Data.FileLen; a != b {
		errs = append(errs, fmt.Sprintf("FileLen %d != %d", a, b))
	}
	if !(hdr.isSymlink() && other.isSymlink()) {
		// Ignore comparing atime/mtime for symlinks, since we
		// cannot set the times/perms on those when syncing, so they will
		// basically always yield errors
		if a, b := hdr.Data.Atime, other.Data.Atime; a != b {
			errs = append(errs, fmt.Sprintf("Atime %d != %d", a, b))
		}
		if a, b := hdr.Data.AtimeNsec, other.Data.AtimeNsec; a != b {
			errs = append(errs, fmt.Sprintf("AtimeNsec %d != %d", a, b))
		}
		if a, b := hdr.Data.Mtime, other.Data.Mtime; a != b {
			errs = append(errs, fmt.Sprintf("Mtime %d != %d", a, b))
		}
		if a, b := hdr.Data.MtimeNsec, other.Data.MtimeNsec; a != b {
			errs = append(errs, fmt.Sprintf("MtimeNsec %d != %d", a, b))
		}
	}
	if len(errs) != 0 {
		log.Printf("file diffs for %v: %v", hdr.path, errs)
		return false
	}
	return true
}

func (hdr *fileHeader) isRegular() bool {
	return os.FileMode(hdr.Data.Mode).IsRegular()
}
func (hdr *fileHeader) isSymlink() bool {
	return os.FileMode(hdr.Data.Mode)&os.ModeSymlink != 0
}
func (hdr *fileHeader) isDir() bool {
	return os.FileMode(hdr.Data.Mode).IsDir()
}

type resultHeader struct {
	ErrorCode uint32
	Pad       uint32
	Crc32     uint64
}

func (hdr *resultHeader) unMarshallBinary(in io.Reader) error {
	return binary.Read(in, binary.LittleEndian, hdr)
}

func (hdr *resultHeader) marshallBinary(out io.Writer) error {
	return binary.Write(out, binary.LittleEndian, hdr)
}

//resultHeaderExt contains info about last processed file
type resultHeaderExt struct {
	LastNameLen uint32
	LastName    string
}

func (hdr *resultHeaderExt) marshallBinary(out io.Writer) error {
	if err := binary.Write(out, binary.LittleEndian, hdr.LastNameLen); err != nil {
		return err
	}
	return WritePath(out, hdr.LastName)
}

func (hdr *resultHeaderExt) unMarshallBinary(in io.Reader) error {
	err := binary.Read(in, binary.LittleEndian, &hdr.LastNameLen)
	if err != nil {
		return err
	}
	hdr.LastName, err = ReadPath(in, hdr.LastNameLen)
	return err
}

func CopyFile(input io.Reader, output io.Writer, length int) error {
	const bufSize = 500 * 1000 // 500 KB
	buf := make([]byte, bufSize)
	for length > 0 {
		// read a chunk
		size := bufSize
		if length < size {
			size = length
		}
		n, err := input.Read(buf[:size])
		if err != nil {
			return err
		}
		// write a chunk
		if _, err := output.Write(buf[:n]); err != nil {
			return err
		}
		length -= n
	}
	return nil
}

// reads a NULL-terminated string from r
func ReadPath(in io.Reader, length uint32) (string, error) {
	if length > MaxPathLength-1 {
		return "", fmt.Errorf("path too large (%d characters)", length)
	}
	if length == 0 {
		return "", nil
	}
	nBuf := make([]byte, length)
	if n, err := io.ReadFull(in, nBuf); err != nil {
		return "", fmt.Errorf("read err, wanted %d, got only %d: %v", length, n, err)
	}
	if nBuf[length-1] != 0 {
		return "", fmt.Errorf("expected NULL-terminated string")
	}
	return string(nBuf[:length-1]), nil
}

// write strings as a null-terminated string to out
func WritePath(out io.Writer, path string) error {
	// write path with zero-suffix
	if len(path) != 0 {
		buf := make([]byte, len(path)+1)
		copy(buf, path)
		_, err := out.Write(buf)
		if err != nil {
			return err
		}
	}
	return nil
}
func RemoveIfExist(path string) error {
	_, err := os.Lstat(path)
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return os.Remove(path)

}
