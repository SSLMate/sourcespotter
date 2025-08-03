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

// Lambda function to build a Go toolchain and upload it to S3
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"go/version"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/aws/aws-lambda-go/lambda"
	"golang.org/x/mod/sumdb/dirhash"
	"software.sslmate.com/src/sourcespotter/internal/httpclient"
	toolchainlambda "software.sslmate.com/src/sourcespotter/internal/toolchain/lambda"
)

func main() {
	lambda.Start(handler)
}

func handler(ctx context.Context, event toolchainlambda.Event) error {
	workDir, err := os.MkdirTemp("", "build-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	var (
		bootstrap = filepath.Join(workDir, "bootstrap")
		goroot    = filepath.Join(workDir, "go")
		zipfile   = filepath.Join(goroot, "pkg", "distpack", event.Version.ZipFilename())
	)

	if err := downloadToolchain(ctx, bootstrap, event.BootstrapURL, event.BootstrapHash); err != nil {
		return fmt.Errorf("error downloading bootstrap toolchain: %w", err)
	}
	if err := downloadSource(ctx, goroot, event.SourceURL); err != nil {
		return fmt.Errorf("error downloading source code: %w", err)
	}

	var logBuf bytes.Buffer
	cmdDir := filepath.Join(goroot, "src")
	cmd := exec.CommandContext(ctx, "./make.bash", "-distpack")
	cmd.Dir = cmdDir
	cmd.Stdout = &logBuf
	cmd.Stderr = &logBuf
	cmd.Env = []string{
		// standard environment variables
		"HOME=/tmp",
		"LANG=C",
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"PWD=" + cmdDir,
		"SHELL=/bin/sh",
		"TMPDIR=/tmp",

		// environment variables that affect the toolchain build
		"GOTOOLCHAIN=local",
		"GOROOT_BOOTSTRAP=" + bootstrap,
		"GOOS=" + event.Version.GOOS,
		"GOARCH=" + event.Version.GOARCH,
	}
	if event.Version.GOOS == "linux" && event.Version.GOARCH == "arm" && version.Compare(event.Version.GoVersion, "go1.21.1") >= 0 {
		cmd.Env = append(cmd.Env, "GOARM=6")
	}

	var errs []error
	if err := cmd.Run(); err != nil {
		errs = append(errs, fmt.Errorf("build failed: %w", err))
	} else if err := httpclient.UploadFile(ctx, event.ZipUploadURL, toolchainlambda.ZipContentType, zipfile); err != nil {
		errs = append(errs, fmt.Errorf("uploading zip failed: %w", err))
	}

	if err := httpclient.Upload(ctx, event.LogUploadURL, toolchainlambda.LogContentType, &logBuf); err != nil {
		errs = append(errs, fmt.Errorf("uploading log failed: %w", err))
	}

	return errors.Join(errs...)
}

func downloadSource(ctx context.Context, destdir string, tgzURL string) error {
	reader, err := httpclient.Download(ctx, tgzURL)
	if err != nil {
		return err
	}
	defer reader.Close()

	gz, err := gzip.NewReader(reader)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		filename := path.Clean(hdr.Name)
		filename, ok := strings.CutPrefix(filename, "go/")
		if !ok {
			continue
		}
		target := filepath.Join(destdir, filepath.FromSlash(filename))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0777); err != nil {
				return err
			}
			w, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(w, tr); err != nil {
				w.Close()
				return err
			}
			w.Close()
		}
	}
	return nil
}

func downloadToolchain(ctx context.Context, destdir string, zipURL string, expectedHash string) error {
	zipFilename, err := httpclient.DownloadToTempFile(ctx, zipURL)
	if err != nil {
		return err
	}
	defer os.Remove(zipFilename)
	if hash, err := dirhash.HashZip(zipFilename, dirhash.Hash1); err != nil {
		return err
	} else if hash != expectedHash {
		return fmt.Errorf("%q has unexpected hash %s (expected %s)", zipURL, hash, expectedHash)
	}
	zipReader, err := zip.OpenReader(zipFilename)
	if err != nil {
		return err
	}
	defer zipReader.Close()

	gorootPaths, _ := fs.Glob(zipReader, "golang.org/toolchain@*")
	if len(gorootPaths) != 1 {
		return fmt.Errorf("%q does not appear to be a toolchain zip", zipURL)
	}
	fsys, err := fs.Sub(zipReader, gorootPaths[0])
	if err != nil {
		return fmt.Errorf("error making toolchain filesystem: %w", err)
	}
	if err := os.CopyFS(destdir, fsys); err != nil {
		return fmt.Errorf("error unzipping toolchain: %w", err)
	}
	if err := renameGoModFiles(destdir); err != nil {
		return err
	}
	return nil
}

func renameGoModFiles(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == "_go.mod" {
			newPath := filepath.Join(filepath.Dir(path), "go.mod")
			return os.Rename(path, newPath)
		}
		return nil
	})
}
