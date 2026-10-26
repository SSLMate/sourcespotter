// Copyright (C) 2026 Opsmate, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a
// copy of this software and associated documentation files (the "Software"),
// to deal in the Software without restriction, including without limitation
// the rights to use, copy, modify, merge, publish, distribute, sublicense,
// and/or sell copies of the Software, and to permit persons to whom the
// Software is furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included
// in all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL
// THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR
// OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE,
// ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
// OTHER DEALINGS IN THE SOFTWARE.
//
// Except as contained in this notice, the name(s) of the above copyright
// holders shall not be used in advertising or otherwise to promote the
// sale, use or other dealings in this Software without prior written
// authorization.

// sourcespotter-cosign-keygen generates the Ed25519 key that sourcespotter
// uses to cosign STHs
package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"os"

	"software.sslmate.com/src/sourcespotter/cosign"
)

func main() {
	log.SetPrefix("sourcespotter-cosign-keygen: ")
	log.SetFlags(0)

	domain := flag.String("domain", "", "The `DOMAIN` configured in sourcespotter's config file (required)")
	flag.Parse()
	if *domain == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: sourcespotter-cosign-keygen -domain DOMAIN")
		flag.PrintDefaults()
		os.Exit(2)
	}

	// The key name is not configurable: a cosignature from "<domain>/toolchain"
	// means that the STH is consistent with previously cosigned STHs and that
	// every toolchain in the tree has been verified reproducible (or skipped).
	skey, vkey, err := cosign.GenerateKey(rand.Reader, *domain+"/toolchain")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Private key (set Cosign.Key to this in sourcespotter's config file):\n%s\n\n", skey)
	fmt.Printf("Public (verifier) key:\n%s\n", vkey)
}
