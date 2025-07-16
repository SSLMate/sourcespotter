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
