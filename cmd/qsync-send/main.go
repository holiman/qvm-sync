package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/holiman/qvm-sync/packer"
)

func init() {
	packer.SetupLogging()
}

func main() {

	disableCompression := flag.Bool("n", false, "`nocompress` disables compression")
	verbosity := flag.Uint("v", 3, "`verbosity`: 0=None, 1=Error, 2=Warn, 3=Info, 4=Debug, 5=Trace")
	ignoreSymlinks := flag.Bool("i", false, "`ignore-symlinks` - if set, symlinks are ignored")

	opts := packer.DefaultOptions
	if *disableCompression{
		opts.Compression = packer.CompressionOff
	}
	if *ignoreSymlinks{
		opts.IgnoreSymlinks = true
	}
	opts.Verbosity = int(*verbosity)

	flag.Parse()
	flag.Usage = func(){
		fmt.Fprintf(flag.CommandLine.Output(), "Usage:\n %s [options] /directory/to/sync\nOptions:\n", os.Args[0])
		flag.PrintDefaults()
	}

	if flag.NArg() < 1 {
		fmt.Fprintf(flag.CommandLine.Output(), "Error: path not supplied\n")
		flag.Usage()
		os.Exit(1)
	}
	sender, err := packer.NewSender(os.Stdout, os.Stdin, opts)
	if err != nil {
		log.Fatal(err)
	}
	if err := sender.Sync(os.Args[1]); err != nil {
		log.Fatal(err)
	}
	log.Print("All done")
	os.Exit(0)
}
