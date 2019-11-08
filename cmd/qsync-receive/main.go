package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/holiman/qvm-sync/packer"
)

var logger *log.Logger

func init() {
	host, _ := os.Hostname()
	name := filepath.Base(os.Args[0])
	prefix := fmt.Sprintf(" [%v@%s] ", name, host)
	log.SetPrefix(prefix)
	log.SetOutput(os.Stderr)
}

func main() {
	log.Printf("test %v", os.Args[0])
	if err := packer.DoUnpack(os.Stdin, os.Stdout); err != nil {
		log.Fatalf("Error during unpack: %v", err)
	}
	os.Exit(0)
}
