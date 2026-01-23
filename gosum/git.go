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

// Package gosum contains functionality for creating go.sum files
package gosum

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb/dirhash"
	modzip "golang.org/x/mod/zip"
)

// CreateFromGitTag creates a go.sum file with entries for a tag in a Git repository.
func CreateFromGitTag(repoRoot string, tag string) (string, error) {
	subdir, version, err := parseTag(tag)
	if err != nil {
		return "", err
	}
	moduleDir := filepath.Join(repoRoot, subdir)
	modulePath, err := modulePathFromFile(filepath.Join(moduleDir, "go.mod"))
	if err != nil {
		return "", err
	}

	zipHash, gomodHash, err := hashModuleZip(repoRoot, tag, subdir, modulePath, version)
	if err != nil {
		return "", err
	}

	gosum := fmt.Sprintf("%s %s %s\n", modulePath, version, zipHash)
	gosum += fmt.Sprintf("%s %s/go.mod %s\n", modulePath, version, gomodHash)
	return gosum, nil
}

func parseTag(tag string) (string, string, error) {
	if tag == "" {
		return "", "", errors.New("tag cannot be empty")
	}
	idx := strings.LastIndex(tag, "/")
	if idx == -1 {
		return "", tag, nil
	}
	subdir := tag[:idx]
	version := tag[idx+1:]
	if subdir == "" || version == "" {
		return "", "", fmt.Errorf("invalid tag %q", tag)
	}
	return subdir, version, nil
}

func hashModuleZip(repoRoot, revision, subdir, modulePath, version string) (string, string, error) {
	tempFile, err := os.CreateTemp("", "sourcespotter-authorize-*.zip")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	modVersion := module.Version{Path: modulePath, Version: version}
	if err := modzip.CreateFromVCS(tempFile, modVersion, repoRoot, revision, subdir); err != nil {
		return "", "", err
	}
	if err := tempFile.Close(); err != nil {
		return "", "", err
	}
	zipHash, err := dirhash.HashZip(tempFile.Name(), dirhash.Hash1)
	if err != nil {
		return "", "", err
	}
	gomodHash, err := hashGoMod(tempFile.Name(), modVersion, dirhash.Hash1)
	if err != nil {
		return "", "", err
	}
	return zipHash, gomodHash, nil
}

func hashGoMod(zipfile string, mod module.Version, hash dirhash.Hash) (string, error) {
	z, err := zip.OpenReader(zipfile)
	if err != nil {
		return "", err
	}
	defer z.Close()
	open := func(string) (io.ReadCloser, error) { return z.Open(mod.Path + "@" + mod.Version + "/go.mod") }
	return hash([]string{"go.mod"}, open)
}

func modulePathFromFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	mod, err := modfile.Parse(path, content, nil)
	if err != nil {
		return "", err
	}
	if mod.Module == nil {
		return "", errors.New("go.mod missing module directive")
	}
	return mod.Module.Mod.Path, nil
}
