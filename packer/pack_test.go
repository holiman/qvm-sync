package packer

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"reflect"

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

	name := "./testdata/afile.txt"
	buf := bytes.NewBuffer(nil)
	if err := OsWalk(name, true, buf); err != nil {
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
	if err := OsWalk(name, false, buf); err != nil {
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
000000c0: 6461 7461 2f61 6669 6c65 2e74 7874 0068  data/afile.txt.h
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
	// The data above fails, because in file.go, ReadDir overrides lstat with stat for tests,
	// so in this test the symlink becomes read as a regular file
	exps := "09000000fd4100000000000000000000f448c15d80f5e4095a91c05d8092450774657374646174610013000000b48100000c00000000000000f781c05d80b86723cd81c05dc03d751e74657374646174612f6166696c652e7478740068656c6c6f20776f726c640a13000000ffa1000040000000000000005b91c05d007c31255a91c05d8092450774657374646174612f616c696e6b2e666f6f002f686f6d652f757365722f676f2f7372632f6769746875622e636f6d2f686f6c696d616e2f71766d2d73796e632f74657374646174612f6166696c652e74787409000000fd4100000000000000000000f448c15d80f5e4095a91c05d809245077465737464617461000000000000000000000000000000000000000000000000000000000000000000"
	exp, _ := hex.DecodeString(exps)
	name := "./testdata"
	buf := bytes.NewBuffer(nil)
	OsWalk(name, false, buf)

	got := buf.Bytes()
	if !bytes.Equal(got, exp) {
		t.Errorf("Directory pack went wrong, expected\n%x\ngot:\n%x\n", exp, got)
	}
	fmt.Printf("%x\n", got)
}

func TestMarshalUnMarshal(t *testing.T){

	var hdr fileHeader
	{
		in := make([]byte, 32)
		rand.Read(in)
		// set name length explicitly to zero
		copy(in[0:], []byte{0,0,0,0})
		r := bytes.NewReader(in)
		hdr.unMarshallBinary(r)
		out := hdr.marshallBinary("")
		if !bytes.Equal(out, in){
			t.Fatalf("input: \n%x\n != output:\n%x\n", in, out)
		}
	}

	{
		exp := "abcde"
		hdr.nameLen = uint32(len(exp))
		out2 := hdr.marshallBinary(exp)
		var hdr2 fileHeader

		err := hdr2.unMarshallBinary(bytes.NewReader(out2))
		if err != nil{
			t.Fatal(err)
		}
		//if got != exp{
		//	t.Fatalf("name wrong, got %v exp %v", got, exp)
		//}
		if !reflect.DeepEqual(hdr, hdr2){
			t.Fatalf("err: %v != %v", hdr, hdr2)
		}
	}


}