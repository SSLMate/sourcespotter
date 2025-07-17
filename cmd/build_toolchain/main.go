// Copyright (C) 2025 Opsmate, Inc.
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

// Reproduce the sumdb checksum of a Go toolchain
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/mod/sumdb/dirhash"
	"software.sslmate.com/src/sourcespotter/toolchain"
)

func main() {
	workDir, err := os.MkdirTemp("", "build_toolchain-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(workDir)

	b := &toolchain.BuildInput{
		Version: toolchain.Version{
			GoVersion: os.Args[2],
			GOOS:      os.Args[3],
			GOARCH:    os.Args[4],
		},

		WorkDir: workDir,
		Log:     os.Stderr,

		GetSource: func(ctx context.Context, goVersion string) (io.ReadCloser, error) {
			return os.Open(filepath.Join(os.Args[1], "src", goVersion+".src.tar.gz"))
		},

		GetToolchain: func(ctx context.Context, version toolchain.Version) (io.ReadCloser, error) {
			f, err := os.Open(filepath.Join(os.Args[1], "toolchain", version.ZipFilename()))
			if errors.Is(err, fs.ErrNotExist) {
				return nil, nil
			}
			return f, err
		},
	}
	zipfile, err := toolchain.Build(context.Background(), b)
	if err != nil {
		log.Fatal(err)
	}
	hash, err := dirhash.HashZip(zipfile, dirhash.Hash1)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(hash)
}
