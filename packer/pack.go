package packer

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

func singleFileProcess(filename string, info os.FileInfo, out io.Writer) error {
	hdr := newFileHeader(filename, info)
	out.Write(hdr.marshallBinary(filename))
	if info.Mode().IsRegular() {
		// file data
		file, err := os.Open(filename)
		if err != nil {
			return err
		}
		defer file.Close()
		if _, err := io.Copy(out, file); err != nil {
			return err
		}

	} else if info.Mode()&os.ModeSymlink != 0 {
		data, err := os.Readlink(filename)
		if err != nil {
			return err
		}
		out.Write([]byte(data))
	}
	return nil
}

func OsWalk(filename string, ignoreSymlinks bool, out io.Writer) error {
	stat, err := os.Lstat(filename)
	if err != nil {
		return err
	}
	filename = filepath.Clean(filename)
	root, file := filepath.Split(filename)
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	os.Chdir(root)
	defer os.Chdir(cwd)
	if err := osWalk(file, stat, ignoreSymlinks, out); err != nil {
		return err
	}
	// send ending
	var hdr fileHeader
	_, err = out.Write(hdr.marshallBinary(""))
	return err
}

func osWalk(filename string, stat os.FileInfo, ignoreSymlinks bool, out io.Writer) error {

	if ignoreSymlinks && (stat.Mode()&os.ModeSymlink != 0) {
		return nil
	}
	if err := singleFileProcess(filename, stat, out); err != nil {
		return err
	}
	if !stat.IsDir() {
		return nil
	}
	files, err := ioutil.ReadDir(filename)
	if err != nil {
		return err
	}
	for _, finfo := range files {
		fName := filepath.Join(filename, finfo.Name())
		if err := osWalk(fName, finfo, ignoreSymlinks, out); err != nil {
			return err
		}
	}
	// resend directory info
	stat, _ = os.Lstat(filename)
	if err = singleFileProcess(filename, stat, out); err != nil {
		return err
	}
	return nil
}

func WaitForResult(input io.Reader) error {
	hdr := new(resultHeader)
	if err := hdr.unMarshallBinary(input); err != nil {
		return err
	}
	hdrExt := new(resultHeaderExt)
	lastFilenamePrefix := "; Last file: "
	if err := hdrExt.unMarshallBinary(input); err != nil {
		return err
	}
	if hdrExt.lastNameLen == 0 {
		lastFilenamePrefix = ""
	}
	switch hdr.errorCode {
	case 17: // EEXIST:
		return fmt.Errorf("A file named %s already exists in QubesIncoming dir", hdrExt.lastName)
	case 22: // EINVAL
		return fmt.Errorf("File copy: Corrupted data from packer: %v %v", lastFilenamePrefix, hdrExt.lastName)
	case 0:
		break
	default:
		return fmt.Errorf("File copy: error code %v , %v %v", hdr.errorCode, lastFilenamePrefix, hdrExt.lastName)

	}
	log.Print("Crc checking ignored (todo!)")
	return nil
}
