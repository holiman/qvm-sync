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
	path := os.Args[1]
	sender := packer.NewSender(os.Stdout, os.Stdin, false)
	if err := sender.Sync(path); err != nil{
		log.Fatal(err)
	}
	// wait for response
	log.Print("All done")
	os.Exit(0)
}
