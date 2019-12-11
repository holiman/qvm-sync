package packer

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	rand2 "math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestMarshalUnMarshal(t *testing.T) {

	var fromBin = func(data []byte) (*fileHeader, error) {
		r := bytes.NewReader(data)
		return unMarshallBinary(r)
	}
	var toBin = func(hdr *fileHeader) ([]byte, error) {
		outb := bytes.NewBuffer(nil)
		err := hdr.marshallBinary(outb)
		return outb.Bytes(), err
	}

	var hdr fileHeader
	{
		in := make([]byte, 32)
		rand.Read(in)
		// set name length explicitly to zero
		copy(in[0:], []byte{0, 0, 0, 0})
		hdr, err := fromBin(in)
		if err != nil {
			t.Fatal(err)
		}
		out, err := toBin(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out, in) {
			t.Fatalf("input: \n%x\n != output:\n%x\n", in, out)
		}
	}

	{
		hdr.path = "abcde"
		hdr.Data.NameLen = uint32(len(hdr.path) + 1)
		out, err := toBin(&hdr)
		if err != nil {
			t.Fatal(err)
		}

		hdr2, err := fromBin(out)
		if err != nil {
			t.Fatal(err)
		}

		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(&hdr, hdr2) {
			t.Fatalf("err: %v != %v", hdr, hdr2)
		}
	}
}

func swapDirs(a, b string) error {
	c := fmt.Sprintf("%v.tmp", a)
	if err := os.Rename(a, c); err != nil {
		return err
	}
	if err := os.Rename(b, a); err != nil {
		return err
	}
	if err := os.Rename(c, b); err != nil {
		return err
	}
	return nil
}

func TestEntireDirectory(t *testing.T) {
	// Shoot over the /foobar dirs
	testEntireDirectory(t, "./testdata/foobar")
	testEntireDirectory(t, "./testdata/foobar2")

	// These now become
	// - /tmp/packtest/foobar and
	// - /tmp/packtest/foobar2
	// respectively

	// Now we swap them, and sync again. This should cause some headache, since
	// some files are now dirs and vice versa.
	if err := swapDirs("./testdata/foobar", "./testdata/foobar2"); err != nil {
		t.Fatal(err)
	}
	defer swapDirs("./testdata/foobar", "./testdata/foobar2")

	testEntireDirectory(t, "./testdata/foobar")
	testEntireDirectory(t, "./testdata/foobar2")
}

func testEntireDirectory(t *testing.T, path string) {

	pipeOneIn, pipeOneOut := io.Pipe()
	pipeTwoIn, pipeTwoOut := io.Pipe()

	// Resolve the syncsource before we chdir
	syncSource, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll("/tmp/packtest", 0755)
	if err := os.Chdir("/tmp/packtest/"); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	opts := &Options{
		Compression: CompressionSnappy,
		//Compression:    CompressionOff,
		CrcUsage:       FileCrcAtimeNsecMetadata,
		Verbosity:      4,
		IgnoreSymlinks: false,
	}
	var wg sync.WaitGroup
	wg.Add(1)
	var send = func() {
		defer wg.Done()
		defer pipeOneOut.Close()
		sender, err := NewSender(pipeOneOut, pipeTwoIn, opts)
		if err != nil {
			t.Fatal(err)
		}
		if err := sender.Sync(syncSource); err != nil {
			t.Fatal(err)
		}
		// wait for response
		log.Print("Sender all done")
	}

	var recv = func() {
		defer pipeTwoOut.Close()

		r, err := NewReceiver(pipeOneIn, pipeTwoOut)
		if err != nil {
			t.Fatal(err)
		}
		// Receive directories + metadata
		if err := r.Sync(); err != nil {
			t.Fatalf("Error during sync: %v", err)
		}
		log.Printf("Receiver all done")
	}

	go send()
	recv()
	wg.Wait()
}

func testOsWalk(dirname string) error {

	absPath, _ := filepath.Abs(filepath.Clean(dirname))
	root, path := filepath.Split(absPath)
	stat, err := os.Lstat(absPath)
	if err != nil {
		return err
	}
	// Check that it actually is a directory
	if !stat.IsDir() {
		return fmt.Errorf("%v is not a directory", dirname)
	}
	if err := testOsWalkInternal(root, path, stat); err != nil {
		return err
	}
	return err
}

// With crc:
//BenchmarkCrcFilesBuf/test-32-6         	       1	1033402134 ns/op
//BenchmarkCrcFilesBuf/test-64-6         	       2	 884616798 ns/op // 884 ms-- sane choice
//BenchmarkCrcFilesBuf/test-128-6        	       2	 869347812 ns/op
//BenchmarkCrcFilesBuf/test-1M-6         	       2	 873511816 ns/op

// Without crc:
//BenchmarkCrcFilesBuf/test-32-6         	      10	 161151702 ns/op // 161 ms
//BenchmarkCrcFilesBuf/test-64-6         	      10	 161965409 ns/op
//BenchmarkCrcFilesBuf/test-128-6        	      10	 170212225 ns/op
//BenchmarkCrcFilesBuf/test-1M-6         	      10	 166505231 ns/op

func testOsWalkInternal(root, path string, stat os.FileInfo) error {
	var (
		cur = filepath.Join(root, path)
	)
	_, err := CrcFile(cur, stat)
	if err != nil {
		return err
	}
	if stat.IsDir() {
		files, err := ioutil.ReadDir(cur)
		if err != nil {
			return fmt.Errorf("read dir err on %v: %v", cur, err)
		}
		for _, finfo := range files {
			fName := filepath.Join(path, finfo.Name())
			if err := testOsWalkInternal(root, fName, finfo); err != nil {
				return err
			}
		}
	}
	return nil
}

func TestCrcFiles(t *testing.T) {

	err := testOsWalk("/home/user/go/src/github.com/ethereum/go-ethereum")
	if err != nil {
		t.Fatal(err)
	}
}

func BenchmarkCrcFiles(b *testing.B) {
	for i := 0; i < b.N; i++ {
		testOsWalk("/home/user/go/src/github.com/ethereum/go-ethereum")
	}
}

func BenchmarkCrcFilesBuf(b *testing.B) {
	b.Run("test-32", func(b *testing.B) {

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			testOsWalk("/home/user/go/src/github.com/ethereum/go-ethereum")
		}
	})

	b.Run("test-64", func(b *testing.B) {

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			testOsWalk("/home/user/go/src/github.com/ethereum/go-ethereum")
		}
	})
	b.Run("test-128", func(b *testing.B) {

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			testOsWalk("/home/user/go/src/github.com/ethereum/go-ethereum")
		}
	})
	b.Run("test-1M", func(b *testing.B) {

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			testOsWalk("/home/user/go/src/github.com/ethereum/go-ethereum")
		}
	})
}

// TestSymlinkOutsideOfJailRemoval tests that if the root-jailing is not active,
// that we still do not remove files outside of the sync directory.
//
// Scenario: the receiver has a symlink on the local filesystem:
// /tmp/linktest/thelink.baz -> /tmp/linktest.target
// If we sync over a directory where 'thelink.baz' is not present, the receiver
// should remove the symlink, _not_ the symlink target
func TestSymlinkOutsideOfJailRemoval(t *testing.T) {
	// Set up the canaries

	// One for a direct symlink
	if f, err := os.Create("/tmp/linktest.target"); err != nil {
		t.Fatal(err)
	} else {
		f.Write([]byte("bazonk"))
		f.Close()
	}
	// One for a symlink within a dir that gets nuked
	if f, err := os.Create("/tmp/linktest.target2"); err != nil {
		t.Fatal(err)
	} else {
		f.Write([]byte("bazonk"))
		f.Close()
	}
	// Now sync 'linktest' directory
	testEntireDirectory(t, "./testdata/linktest")
	/*
		At this point, the symlinks are 'live', and resolve to actual existing files:

		[user@work linktest]$ ls -laR
		.:
		total 0
		drwxrwxr-x 3 user user 80 Nov 28 09:35 .
		drwxr-xr-x 3 user user 60 Nov 28 09:35 ..
		drwxrwxr-x 2 user user 60 Nov 28 09:35 directory
		lrwxrwxrwx 1 user user 20 Nov 28 09:35 link1 -> /tmp/linktest.target

		./directory:
		total 0
		drwxrwxr-x 2 user user 60 Nov 28 09:35 .
		drwxrwxr-x 3 user user 80 Nov 28 09:35 ..
		lrwxrwxrwx 1 user user 21 Nov 28 09:35 link2 -> /tmp/linktest.target2

	*/
	// So now we sync over 'emptydir', which will trigger the removal of these
	// But we must first swap the names
	if err := swapDirs("./testdata/linktest", "./testdata/emptydir"); err != nil {
		t.Fatal(err)
	}
	defer swapDirs("./testdata/linktest", "./testdata/emptydir")
	// Now sync 'linktest' directory (which is now empty)
	testEntireDirectory(t, "./testdata/linktest")

	// verify that none of the targets have been removed
	if _, err := os.Stat("/tmp/linktest.target"); err != nil {
		t.Fatalf("File missing: %v", err)
	}
	if _, err := os.Stat("/tmp/linktest.target2"); err != nil {
		t.Fatalf("File missing: %v", err)
	}
}

func TestOverwriteROnlyFiles(t *testing.T) {
	rand2.Seed(time.Now().Unix())
	dir := fmt.Sprintf("/tmp/rdonlytest-%d/readonlydir", rand2.Uint32())
	p := filepath.Join(dir, "readonlyfile")
	// create dir with permissive perms first, so we can create the file
	if err := os.MkdirAll(dir, 0777); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("This file is generated to check if we can delete files which " +
		"have perms 'r--r--r--'"))
	f.Close()

	if err := os.Chmod(p, 0444); err != nil {
		t.Fatal(err)
	}
	// If the dir doesn't have x, we can't open in
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	// Now, we have a rdonly directory, with an rdonly file in it. Shoot it
	// over to a receiver

	testEntireDirectory(t, dir)
	RemoveIfExist(dir)

}
