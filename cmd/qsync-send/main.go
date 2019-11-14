package main

import (
	"log"
	"os"

	"github.com/holiman/qvm-sync/packer"
)

func init() {
	packer.SetupLogging()
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Error: path not supplied")
	}
	sender, err := packer.NewSender(os.Stdout, os.Stdin, nil)
	if err != nil {
		log.Fatal(err)
	}
	if err := sender.Sync(os.Args[1]); err != nil {
		log.Fatal(err)
	}
	log.Print("All done")
	os.Exit(0)
}
