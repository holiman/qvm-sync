package packer

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"syscall"
)

const (
	MaxPathLength = 16384
)

type fileHeader struct {
	nameLen   uint32
	mode      uint32
	fileLen   uint64
	atime     uint32
	atimeNsec uint32
	mtime     uint32
	mtimeNsec uint32
}

func newFileHeader(fileName string, info os.FileInfo) *fileHeader {
	stat := info.Sys().(*syscall.Stat_t)
	hdr := &fileHeader{
		mode:      uint32(info.Mode()),
		mtime:     uint32(stat.Mtim.Sec),
		mtimeNsec: uint32(stat.Mtim.Nsec),
		atime:     uint32(stat.Atim.Sec),
		atimeNsec: uint32(stat.Atim.Nsec),
		fileLen:   uint64(stat.Size),
		nameLen:   uint32(len(fileName) + 1),
	}
	if info.Mode().IsDir() {
		hdr.fileLen = 0
	}
	return hdr
}

func (hdr *fileHeader) marshallBinary(filename string) []byte {
	var buf []byte
	if len(filename) != 0 {
		buf = make([]byte, 32+len(filename)+1)
	} else {
		buf = make([]byte, 32)
	}
	binary.LittleEndian.PutUint32(buf[0:], hdr.nameLen)
	binary.LittleEndian.PutUint32(buf[4:], hdr.mode)
	binary.LittleEndian.PutUint64(buf[8:], hdr.fileLen)
	binary.LittleEndian.PutUint32(buf[16:], hdr.atime)
	binary.LittleEndian.PutUint32(buf[20:], hdr.atimeNsec)
	binary.LittleEndian.PutUint32(buf[24:], hdr.mtime)
	binary.LittleEndian.PutUint32(buf[28:], hdr.mtimeNsec)
	// filename + zero suffix

	//fmt.Fprintf(os.Stderr,"name: %v\n", filename)
	//fmt.Fprintf(os.Stderr,"  namelen %x\n",buf[0:4])
	//fmt.Fprintf(os.Stderr,"  mode %x\n",buf[4:8])
	//fmt.Fprintf(os.Stderr,"  fileLen %x\n",buf[8:16])
	//fmt.Fprintf(os.Stderr,"  atime %x\n",buf[16:20])
	//fmt.Fprintf(os.Stderr,"  atimeNsec %x\n",buf[20:24])
	//fmt.Fprintf(os.Stderr,"  mtime %x\n",buf[24:28])
	//fmt.Fprintf(os.Stderr,"  mtimeNsec %x\n",buf[28:32])
	copy(buf[32:], []byte(filename))
	return buf
}

func (hdr *fileHeader) unMarshallBinary(reader io.Reader) error {
	buf := make([]byte, 32)
	n, err := reader.Read(buf)
	if n == 32 {
		hdr.nameLen = binary.LittleEndian.Uint32(buf[0:])
		hdr.mode = binary.LittleEndian.Uint32(buf[4:])
		hdr.fileLen = binary.LittleEndian.Uint64(buf[8:])
		hdr.atime = binary.LittleEndian.Uint32(buf[16:])
		hdr.atimeNsec = binary.LittleEndian.Uint32(buf[20:])
		hdr.mtime = binary.LittleEndian.Uint32(buf[24:])
		hdr.mtimeNsec = binary.LittleEndian.Uint32(buf[28:])
	} else {
		return fmt.Errorf("could only read %d bytes, need 32 , err: %v", n, err)
	}
	// drop zero suffix
	if hdr.nameLen > 0 {
		if hdr.nameLen > MaxPathLength-1 {
			return fmt.Errorf("too large file name, %d characters", hdr.nameLen)
		}
	}
	return nil
}

type resultHeader struct {
	errorCode uint32
	pad       uint32
	crc32     uint64
}

func (hdr *resultHeader) unMarshallBinary(in io.Reader) error {

	if err := binary.Read(in, binary.LittleEndian, hdr.errorCode); err != nil {
		return err
	}
	if err := binary.Read(in, binary.LittleEndian, hdr.pad); err != nil {
		return err
	}
	return binary.Read(in, binary.LittleEndian, hdr.crc32)
}

func (hdr *resultHeader) marshallBinary(out io.Writer) error {
	if err := binary.Write(out, binary.LittleEndian, hdr.errorCode); err != nil {
		return err
	}
	if err := binary.Write(out, binary.LittleEndian, hdr.pad); err != nil {
		return err
	}
	return binary.Write(out, binary.LittleEndian, hdr.crc32)
}

//
///* optional info about last processed file */
//struct result_header_ext {
//    uint32_t last_namelen;
//    char last_name[0];
//} __attribute__((packed));
//

type resultHeaderExt struct {
	lastNameLen uint32
	lastName    string
}

func (hdr *resultHeaderExt) marshallBinary(out io.Writer) error {
	if err := binary.Write(out, binary.LittleEndian, hdr.lastNameLen); err != nil {
		return err
	}
	buf := make([]byte, hdr.lastNameLen)
	copy(buf, []byte(hdr.lastName))
	_, err := out.Write(buf)
	return err
}

func (hdr *resultHeaderExt) unMarshallBinary(in io.Reader) error {

	if err := binary.Read(in, binary.LittleEndian, hdr.lastNameLen); err != nil {
		return err
	}
	if hdr.lastNameLen > MaxPathLength {
		hdr.lastNameLen = MaxPathLength
	}
	buf := make([]byte, hdr.lastNameLen)
	if n, err := in.Read(buf); n != int(hdr.lastNameLen) {
		return fmt.Errorf("failed parsing result, wanted %d bytes, got %d (err: %v)", hdr.lastNameLen, n, err)
	}
	if hdr.lastNameLen > 0 {
		hdr.lastName = string(buf[:int(hdr.lastNameLen)-1])
	}
	return nil
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
		if n == 0 {
			return fmt.Errorf("eof reading data")
		}
		// write a chunk
		if _, err := output.Write(buf[:n]); err != nil {
			return err
		}
		length -= n
	}
	return nil
}
