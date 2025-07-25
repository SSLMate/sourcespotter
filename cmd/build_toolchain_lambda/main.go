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
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/aws/aws-lambda-go/lambda"
	"golang.org/x/mod/sumdb/dirhash"
	"software.sslmate.com/src/sourcespotter/internal/httpclient"
	toolchainlambda "software.sslmate.com/src/sourcespotter/internal/toolchain/lambda"
	"software.sslmate.com/src/sourcespotter/toolchain"
)

func init() {
	os.Setenv("HOME", "/tmp")
}

func main() {
	lambda.Start(handler)
}

func handler(ctx context.Context, event toolchainlambda.Event) error {
	gorootBootstrap, err := downloadToolchain(ctx, event.BootstrapURL, event.BootstrapHash)
	if err != nil {
		return fmt.Errorf("error downloading bootstrap toolchain: %w", err)
	}
	defer os.RemoveAll(gorootBootstrap)

	workDir, err := os.MkdirTemp("", "build-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	var logBuf bytes.Buffer

	input := &toolchain.BuildInput{
		WorkDir:         workDir,
		Version:         event.Version,
		GorootBootstrap: gorootBootstrap,
		GetSource: func(ctx context.Context, goversion string) (io.ReadCloser, error) {
			if goversion != event.Version.GoVersion {
				return nil, fmt.Errorf("no source available for %q", goversion)
			}
			return httpclient.Download(ctx, event.SourceURL)
		},
		Log: &logBuf,
	}

	var errs []error
	if zipPath, err := toolchain.Build(ctx, input); err != nil {
		errs = append(errs, fmt.Errorf("build failed: %w", err))
	} else if err := uploadFile(ctx, event.ZipUploadURL, toolchainlambda.ZipContentType, zipPath); err != nil {
		errs = append(errs, fmt.Errorf("uploading zip failed: %w", err))
	}

	if err := httpclient.Upload(ctx, event.LogUploadURL, toolchainlambda.LogContentType, &logBuf); err != nil {
		errs = append(errs, fmt.Errorf("uploading log failed: %w", err))
	}

	return errors.Join(errs...)
}

func downloadToolchain(ctx context.Context, zipURL string, expectedHash string) (path string, err error) {
	zipFilename, err := httpclient.DownloadToTempFile(ctx, zipURL)
	if err != nil {
		return "", err
	}
	defer os.Remove(zipFilename)
	if hash, err := dirhash.HashZip(zipFilename, dirhash.Hash1); err != nil {
		return "", err
	} else if hash != expectedHash {
		return "", fmt.Errorf("%q has unexpected hash %s (expected %s)", zipURL, hash, expectedHash)
	}
	zipReader, err := zip.OpenReader(zipFilename)
	if err != nil {
		return "", err
	}
	defer zipReader.Close()

	gorootPaths, _ := fs.Glob(zipReader, "golang.org/toolchain@*")
	if len(gorootPaths) != 1 {
		return "", fmt.Errorf("%q does not appear to be a toolchain zip", zipURL)
	}
	fsys, err := fs.Sub(zipReader, gorootPaths[0])
	if err != nil {
		return "", fmt.Errorf("error making toolchain filesystem: %w", err)
	}
	tempdir, err := os.MkdirTemp("", "toolchain-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() {
		if err != nil {
			os.RemoveAll(tempdir)
		}
	}()
	if err := os.CopyFS(tempdir, fsys); err != nil {
		return "", fmt.Errorf("error unzipping toolchain: %w", err)
	}
	if err := renameGoModFiles(tempdir); err != nil {
		return "", err
	}
	return tempdir, nil
}

func uploadFile(ctx context.Context, url, contentType, path string) error {
	f, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return httpclient.Upload(ctx, url, contentType, bytes.NewReader(f))
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
