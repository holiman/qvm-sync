package main

import (
	"log"
	"os"

	"github.com/holiman/qvm-sync/packer"
)

func init() {
	packer.SetupLogging()
}

const useSnappy = true

func main() {
	r, err := packer.NewReceiver(os.Stdin, os.Stdout)
	if err != nil {
		log.Fatalf("Error during init: %v", err)
	}
	if err := r.Sync(); err != nil {
		log.Fatalf("Error during sync : %v", err)
	}
}
