package packer

import (
	"bufio"
	"fmt"
	"github.com/golang/snappy"
	"hash/crc32"
	"io"
	"log"
	"os"
	"path/filepath"
)


func SetupLogging() {
	host, _ := os.Hostname()
	name := filepath.Base(os.Args[0])
	prefix := fmt.Sprintf(" [%v@%s] ", name, host)
	log.SetPrefix(prefix)
	log.SetOutput(os.Stderr)
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

var readBuf = make([]byte, 64*1000)

// CrcFile return the crc32 using IEEETable.
// If file is directory, symlink or empty, it return crc 0
// This method is not at all safe for concurrent usage, as it
// reuses an internal buffer
func CrcFile(path string, stat os.FileInfo) (uint32, error) {
	if !stat.Mode().IsRegular() {
		return 0, nil
	}
	var (
		size = stat.Size()
		crc  uint32
	)
	if size == 0 {
		return 0, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	for size > 0 {
		n, err := file.Read(readBuf)
		if err != nil {
			return 0, err
		}
		crc = crc32.Update(crc, crc32.IEEETable, readBuf[:n])
		size -= int64(n)
	}
	return crc, nil
}

func CopyFile(input io.Reader, output io.Writer, size int) error {
	bufSize := len(readBuf)
	for size > 0 {
		// read a chunk
		maxRead := bufSize
		if size < maxRead {
			maxRead = size
		}
		n, err := input.Read(readBuf[:maxRead])
		if err != nil {
			return err
		}
		// write a chunk
		if _, err := output.Write(readBuf[:n]); err != nil {
			return err
		}
		size -= n
	}
	return nil
}

// BufferedWriter is used to make it possible to switch os.Stdout for a
// buffered one or snappy-based on
type BufferedWriter interface {
	io.Writer
	Flush() error
}

// MeteredWriter keeps track of amount of bytes written
type MeteredWriter struct {
	c   int
	out BufferedWriter
}

func (c *MeteredWriter) Write(p []byte) (n int, err error) {
	n, e := c.out.Write(p)
	c.c += n
	return n, e
}
func (c *MeteredWriter) Flush() error {
	return c.out.Flush()
}
func NewMeteredWriter(out BufferedWriter) *MeteredWriter {
	return &MeteredWriter{0, out}
}

// SnapShim is a hack to make snappy.Writer behave like a proper writer.
//
// For some reason, the snappy.Writer.Flush method does not actually
// implement the regular Flush semantics: whatever has been written is sent
// off to the receiver. Instead, the docs says that in order to properly
// "Flush" the content to the receiver, we need to Close() the writer.
type SnapShim struct {
	out  BufferedWriter
	snap *snappy.Writer
}

func (s *SnapShim) Write(p []byte) (n int, err error) {
	return s.snap.Write(p)
}

func (s *SnapShim) Flush() error {
	if err := s.snap.Flush(); err != nil {
		return err
	}
	if err := s.snap.Close(); err != nil {
		return err
	}
	s.out.Flush()
	s.snap.Reset(s.out)
	return nil
}

// ConfigurableWriter is a convenience type to use either snappy or not,
// and also keep track of the write-stats
type ConfigurableWriter struct {
	out BufferedWriter

	compressedMeter *MeteredWriter
	rawMeter        *MeteredWriter
}

func NewConfigurableWriter(useSnappy bool, out io.Writer) BufferedWriter {
	var (
		snappyMeter *MeteredWriter
		rawMeter    *MeteredWriter
		bufOut      BufferedWriter
	)
	bufOut = bufio.NewWriter(out)
	if useSnappy {
		snappyMeter = NewMeteredWriter(bufOut)
		bufOut = &SnapShim{
			out:  snappyMeter,
			snap: snappy.NewBufferedWriter(snappyMeter),
		}
	}
	rawMeter = NewMeteredWriter(bufOut)
	return &ConfigurableWriter{
		out:             rawMeter,
		compressedMeter: snappyMeter,
		rawMeter:        rawMeter,
	}
}

func (s *ConfigurableWriter) Write(p []byte) (n int, err error) {
	return s.out.Write(p)
}

func (s *ConfigurableWriter) Flush() error {
	return s.out.Flush()
}

func (s *ConfigurableWriter) Stats() (raw int, compressed int) {
	if s.rawMeter != nil {
		raw = s.rawMeter.c
	}
	if s.compressedMeter != nil {
		compressed = s.compressedMeter.c
	}
	return raw, compressed
}
