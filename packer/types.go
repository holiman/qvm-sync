package packer

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"
)

const (
	MaxPathLength = 16384
)

const (
	Version = 0

	CompressionOff    = 0
	CompressionSnappy = 1

	FileCrcOff               = 0
	FileCrcAtimeNsec         = 1
	FileCrcAtimeNsecMetadata = 2
)

type Options struct {
	Verbosity      int
	CrcUsage       int
	IgnoreSymlinks bool
	Compression    int
}

var DefaultOptions = &Options{
	Verbosity:      3, // info
	CrcUsage:       FileCrcAtimeNsecMetadata,
	Compression:    CompressionSnappy,
	IgnoreSymlinks: false,
}

// versionHeader is sent as the first thing when a sync is initiated.
// OBS: This deviates from the qvm-copy protocol, which does not have any
// such thing.
type versionHeader struct {
	// This field is filled with ones, and can be totally ignored. The idea is
	// that if a receiver doesn't know about versioning, it will be interpreted
	// as 'NameLen' and rejected.
	Ones        uint32
	Version     uint16
	Compression uint16 // Type of compression used for the data after this header
	// Whether crc will be used in metadata, and how.
	// 0 == no crc
	// 1 == crc in place of atimensec (always)
	// 2 == crc in place of atimensec for initial metadata, but not provided
	// in the second actual transfer
	FileCrcUsage uint16
	// Desired verbosity. 0 = None, 1 = Error, 2 = Warn, 3 = Info, 4 = Debug, 5 = Trace
	Verbosity uint8
	Reserved  uint64
}

func newVersionHeader(compression, crcUsage, verbosity int) *versionHeader {
	return &versionHeader{
		Ones:         0xFFFFFFFF,
		Version:      uint16(Version),
		Compression:  uint16(compression),
		FileCrcUsage: uint16(crcUsage),
		Verbosity:    uint8(verbosity),
	}
}

func (v *versionHeader) marshallBinary(out io.Writer) error {
	if err := binary.Write(out, binary.LittleEndian, v); err != nil {
		return err
	}
	return nil
}

type fileHeader struct {
	Data fileHeaderData
	path string
}

// fileHeaderData is 256 bits always
type fileHeaderData struct {
	NameLen uint32
	Mode    uint32
	FileLen uint64
	Atime   uint32
	// When crc is used, the AtimeNsec field is replaced with a crc32 checksum
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

func (hdr *fileHeader) Diff(other *fileHeader) []string {
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
		if a, b := hdr.Data.Mtime, other.Data.Mtime; a != b {
			errs = append(errs, fmt.Sprintf("Mtime %d != %d", a, b))
		}
		if a, b := hdr.Data.MtimeNsec, other.Data.MtimeNsec; a != b {
			errs = append(errs, fmt.Sprintf("MtimeNsec %d != %d", a, b))
		}
		// Also, ignore Atime differences
		//if a, b := hdr.Data.Atime, other.Data.Atime; a != b {
		//	errs = append(errs, fmt.Sprintf("Atime %d != %d", a, b))
		//}
		//if a, b := hdr.Data.AtimeNsec, other.Data.AtimeNsec; a != b {
		//	errs = append(errs, fmt.Sprintf("AtimeNsec %d != %d", a, b))
		//}

	}
	return errs
}

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
func (hdr *fileHeader) fixTimesAndPerms() error {
	if err := os.Chmod(hdr.path, os.FileMode(hdr.Data.Mode&07777)); err != nil {
		return err
	}
	atime := time.Unix(int64(hdr.Data.Atime), int64(hdr.Data.AtimeNsec))
	mtime := time.Unix(int64(hdr.Data.Mtime), int64(hdr.Data.MtimeNsec))
	return os.Chtimes(hdr.path, atime, mtime)
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
