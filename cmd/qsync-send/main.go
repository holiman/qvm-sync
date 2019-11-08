package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/holiman/qvm-sync/packer"
)

func init() {
	host, _ := os.Hostname()
	name := filepath.Base(os.Args[0])
	prefix := fmt.Sprintf(" [%v@%s] ", name, host)
	log.SetPrefix(prefix)
	log.SetOutput(os.Stderr)
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Error: path not supplied")
	}
	fn := os.Args[1]
	if err := packer.OsWalk(fn, false, os.Stdout); err != nil {
		log.Fatalf("Error sending: %v", err)
	}

	if err := packer.WaitForResult(os.Stdin); err != nil {
		log.Fatalf("Error waiting for response: %v", err)
	}
	// wait for response
	log.Print("All done")
	os.Exit(0)
}
