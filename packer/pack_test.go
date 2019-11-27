package packer

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

/*
/usr/lib/qubes/qfile-agent testdata/afile.txt | xxd
qvm-sync]$ /usr/lib/qubes/qfile-agent testdata/afile.txt | xxd
00000000: 0a00 0000 b481 0000 0c00 0000 0000 0000  ................
00000010: f781 c05d 80b8 6723 cd81 c05d c03d 751e  ...]..g#...].=u.
00000020: 6166 696c 652e 7478 7400 6865 6c6c 6f20  afile.txt.hello
00000030: 776f 726c 640a 0000 0000 0000 0000 0000  world...........
00000040: 0000 0000 0000 0000 0000 0000 0000 0000  ................

*/
func TestPackFile(t *testing.T) {
	exp, _ := hex.DecodeString("0a000000b48100000c00000000000000f781c05d80b86723cd81c05dc03d751e6166696c652e7478740068656c6c6f20776f726c640a0000000000000000000000000000000000000000000000000000000000000000")
	buf := bytes.NewBuffer(nil)
	r, _ := NewSender(buf, nil, nil)
	name := "./testdata/afile.txt"

	if err := r.transmitDirectory(name); err != nil {
		t.Fatal(err)
	}
	got := buf.Bytes()

	if !bytes.Equal(got, exp) {
		t.Errorf("Regular file pack went wrong, expected\n%x\ngot:\n%x\n", exp, got)
	}

	fmt.Printf("%x\n", got)
}

/**
/usr/lib/qubes/qfile-agent testdata/alink.foo | xxd
00000000: 0a00 0000 ffa1 0000 4000 0000 0000 0000  ........@.......
00000010: 5b91 c05d 007c 3125 5a91 c05d 8092 4507  [..].|1%Z..]..E.
00000020: 616c 696e 6b2e 666f 6f00 2f68 6f6d 652f  alink.foo./home/
00000030: 7573 6572 2f67 6f2f 7372 632f 6769 7468  user/go/src/gith
00000040: 7562 2e63 6f6d 2f68 6f6c 696d 616e 2f71  ub.com/holiman/q
00000050: 766d 2d73 796e 632f 7465 7374 6461 7461  vm-sync/testdata
00000060: 2f61 6669 6c65 2e74 7874 0000 0000 0000  /afile.txt......
00000070: 0000 0000 0000 0000 0000 0000 0000 0000  ................

                                   0000 0000 0000
          0000 0000 0000 0000 0000 0000 0000 0000
*/
func TestPackSymlink(t *testing.T) {

	exp, _ := hex.DecodeString("0a000000ffa1000040000000000000005b91c05d007c31255a91c05d80924507616c696e6b2e666f6f002f686f6d652f757365722f676f2f7372632f6769746875622e636f6d2f686f6c696d616e2f71766d2d73796e632f74657374646174612f6166696c652e7478740000000000000000000000000000000000000000000000000000000000000000")

	name := "./testdata/alink.foo"
	buf := bytes.NewBuffer(nil)
	r, _ := NewSender(buf, nil, nil)

	if err := r.transmitDirectory(name); err != nil {
		t.Fatal(err)
	}
	got := buf.Bytes()
	if !bytes.Equal(got, exp) {
		t.Errorf("Symlink pack went wrong, expected\n%x\ngot:\n%x\n", exp, got)
	}
	fmt.Printf("%x\n", got)
}

/**
00000000: 0900 0000 fd41 0000 0000 0000 0000 0000  .....A..........
00000010: 5b91 c05d 007c 3125 5a91 c05d 8092 4507  [..].|1%Z..]..E.
00000020: 7465 7374 6461 7461 0013 0000 00ff a100  testdata........
00000030: 0040 0000 0000 0000 005b 91c0 5d00 7c31  .@.......[..].|1
00000040: 255a 91c0 5d80 9245 0774 6573 7464 6174  %Z..]..E.testdat
00000050: 612f 616c 696e 6b2e 666f 6f00 2f68 6f6d  a/alink.foo./hom
00000060: 652f 7573 6572 2f67 6f2f 7372 632f 6769  e/user/go/src/gi
00000070: 7468 7562 2e63 6f6d 2f68 6f6c 696d 616e  thub.com/holiman
00000080: 2f71 766d 2d73 796e 632f 7465 7374 6461  /qvm-sync/testda
00000090: 7461 2f61 6669 6c65 2e74 7874 1300 0000  ta/afile.txt....
000000a0: b481 0000 0c00 0000 0000 0000 f781 c05d  ...............]
000000b0: 80b8 6723 cd81 c05d c03d 751e 7465 7374  ..g#...].=u.test
000000c0: 6461 7461 2f61 6669 6c65 2e74 7874 0068  Data/afile.txt.h
000000d0: 656c 6c6f 2077 6f72 6c64 0a09 0000 00fd  ello world......
000000e0: 4100 0000 0000 0000 0000 005b 91c0 5d00  A..........[..].
000000f0: 7c31 255a 91c0 5d80 9245 0774 6573 7464  |1%Z..]..E.testd
00000100: 6174 6100 0000 0000 0000 0000 0000 0000  ata.............
00000110: 0000 0000 0000 0000 0000 0000 0000 0000  ................
*/
func TestWalk(t *testing.T) {

	//exps := "0900 0000 fd41 0000 0000 0000 0000 0000"+
	//"5b91 c05d 007c 3125 5a91 c05d 8092 4507"+
	//"7465 7374 6461 7461 0013 0000 00ff a100"+
	//"0040 0000 0000 0000 005b 91c0 5d00 7c31"+
	//"255a 91c0 5d80 9245 0774 6573 7464 6174"+
	//"612f 616c 696e 6b2e 666f 6f00 2f68 6f6d"+
	//"652f 7573 6572 2f67 6f2f 7372 632f 6769"+
	//"7468 7562 2e63 6f6d 2f68 6f6c 696d 616e"+
	//"2f71 766d 2d73 796e 632f 7465 7374 6461"+
	//"7461 2f61 6669 6c65 2e74 7874 1300 0000"+
	//"b481 0000 0c00 0000 0000 0000 f781 c05d"+
	//"80b8 6723 cd81 c05d c03d 751e 7465 7374"+
	//"6461 7461 2f61 6669 6c65 2e74 7874 0068"+
	//"656c 6c6f 2077 6f72 6c64 0a09 0000 00fd"+
	//"4100 0000 0000 0000 0000 005b 91c0 5d00"+
	//"7c31 255a 91c0 5d80 9245 0774 6573 7464"+
	//"6174 6100 0000 0000 0000 0000 0000 0000"+
	//"0000 0000 0000 0000 0000 0000 0000 0000"+
	//"0000 0000"

	//exps = strings.Replace(exps," ", "", -1)
	// The Data above fails, because in file.go, ReadDir overrides lstat with stat for tests,
	// so in this test the symlink becomes read as a regular file
	exps := "09000000fd4100000000000000000000f448c15d80f5e4095a91c05d8092450774657374646174610013000000b48100000c00000000000000f781c05d80b86723cd81c05dc03d751e74657374646174612f6166696c652e7478740068656c6c6f20776f726c640a13000000ffa1000040000000000000005b91c05d007c31255a91c05d8092450774657374646174612f616c696e6b2e666f6f002f686f6d652f757365722f676f2f7372632f6769746875622e636f6d2f686f6c696d616e2f71766d2d73796e632f74657374646174612f6166696c652e74787409000000fd4100000000000000000000f448c15d80f5e4095a91c05d809245077465737464617461000000000000000000000000000000000000000000000000000000000000000000"
	exp, _ := hex.DecodeString(exps)
	name := "./testdata"
	buf := bytes.NewBuffer(nil)
	r, _ := NewSender(buf, nil, nil)
	r.transmitDirectory(name)

	got := buf.Bytes()
	if !bytes.Equal(got, exp) {
		t.Errorf("Directory pack went wrong, expected\n%x\ngot:\n%x\n", exp, got)
	}
	fmt.Printf("%x\n", got)
}

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

func TestEntireDirectory(t *testing.T) {
	// Shoot over the /foobar dir
	testEntireDirectory(t, "./testdata/foobar")
	// And then the /foobar2 dir, where some files
	// are now dirs, and vice versa

	// but need to swap them first
	os.Rename("./testdata/foobar","./testdata/foobarOld")
	os.Rename("./testdata/foobar2","./testdata/foobar")
	defer func(){
		os.Rename("./testdata/foobar","./testdata/foobar2")
		os.Rename("./testdata/foobarOld","./testdata/foobar")
	}()

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
	if err != nil{
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
