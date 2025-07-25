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

// Package toolchain contains functions for building a Go toolchain
package toolchain

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	goversionpkg "go/version"
)

type BuildInput struct {
	// WorkDir is the path to an empty directory in which to do work
	WorkDir string

	// Version is the version of the toolchain to build
	Version Version

	// GorootBootstrap is the path to a GOROOT to use for bootstrapping
	GorootBootstrap string

	// GetSource returns the source tar.gz file for the given Go version, e.g. "go1.24.4"
	GetSource func(context.Context, string) (io.ReadCloser, error)

	// Log, if non-nil, receives the output the build scripts
	Log io.Writer
}

// Build builds a Go toolchain and returns the path to the module zip file, which will be under input.WorkDir. Build will retrieve or build other toolchains (using input.GetToolchain and input.GetSource) as necessary for bootstrapping.
func Build(ctx context.Context, input *BuildInput) (string, error) {
	return input.build(ctx)
}

func (b *BuildInput) build(ctx context.Context) (string, error) {
	gorootBootstrap := b.GorootBootstrap

	b.logf("Getting source for %s...", b.Version.GoVersion)
	goroot := filepath.Join(b.WorkDir, "goroot")
	if err := b.getSource(ctx, b.Version.GoVersion, goroot); err != nil {
		return "", fmt.Errorf("error getting source for %s: %w", b.Version.GoVersion, err)
	}

	b.logf("Building %s using %q...", b.Version.GoVersion, gorootBootstrap)
	env := []string{
		"GOROOT_BOOTSTRAP=" + gorootBootstrap,
		"GOOS=" + b.Version.GOOS,
		"GOARCH=" + b.Version.GOARCH,
	}
	if b.Version.GOOS == "linux" && b.Version.GOARCH == "arm" && goversionpkg.Compare(b.Version.GoVersion, "go1.21.1") >= 0 {
		env = append(env, "GOARM=6")
	}
	if err := b.buildSource(ctx, goroot, []string{"-distpack"}, env); err != nil {
		return "", fmt.Errorf("error building source for %s: %w", b.Version.GoVersion, err)
	}
	zippath := filepath.Join(goroot, "pkg", "distpack", b.Version.ZipFilename())
	return zippath, nil
}

func (b *BuildInput) getSource(ctx context.Context, goVersion string, destDir string) error {
	f, err := b.GetSource(ctx, goVersion)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
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
		target := filepath.Join(destDir, filepath.FromSlash(filename))
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

func (b *BuildInput) buildSource(ctx context.Context, goroot string, args []string, env []string) error {
	dir := filepath.Join(goroot, "src")
	cmd := exec.CommandContext(ctx, "./make.bash", args...)
	cmd.Dir = dir
	cmd.Env = []string{
		// standard environment variables
		"LANG=C",
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"SHELL=/bin/sh",
		"PWD=" + dir,

		// environment variables that affect the toolchain build
		"GOTOOLCHAIN=local",
	}
	for _, name := range []string{"USER", "LOGNAME", "HOME", "TMPDIR"} {
		if value, ok := os.LookupEnv(name); ok {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdout = b.Log
	cmd.Stderr = b.Log
	return cmd.Run()
}

func (b *BuildInput) logf(format string, a ...any) {
	if b.Log != nil {
		fmt.Fprintf(b.Log, format+"\n", a...)
	}
}
