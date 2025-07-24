package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"golang.org/x/mod/sumdb/dirhash"
	"software.sslmate.com/src/sourcespotter/toolchain"
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: hashzip [-strip-darwin-sig] zipfilename\n")
	os.Exit(2)
}

func main() {
	log.SetPrefix("hashzip: ")
	log.SetFlags(0)

	stripDarwinSig := flag.Bool("strip-darwin-sig", false, "Strip Darwin signatures from binaries before hashing")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) != 1 {
		usage()
	}
	zipfilename := args[0]
	var fix toolchain.HashFixer
	if *stripDarwinSig {
		fix = toolchain.StripDarwinSig
	}

	h, err := toolchain.HashZip(zipfilename, dirhash.Hash1, fix)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(h)
}
