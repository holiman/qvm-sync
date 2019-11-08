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
	r := packer.NewReceiver(os.Stdin, os.Stdout, true)
	// Receive directories + metadata
	if err := r.ReceiveMetadata(); err != nil {
		log.Fatalf("Error during unpack [1]: %v", err)
	}
	// Request files
	if err := r.RequestFiles(); err != nil {
		log.Fatalf("Error during file request: %v", err)
	}
	// Receive data content
	if err := r.ReceiveFullData(); err != nil {
		log.Fatalf("Error during unpack [2]: %v", err)
	}
}
