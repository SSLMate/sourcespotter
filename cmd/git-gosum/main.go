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

// git-gosum outputs a go.sum file for one or more Git tags
package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/sync/errgroup"
	"software.sslmate.com/src/sourcespotter/gosum"
)

func main() {
	log.SetPrefix("git-gosum: ")
	log.SetFlags(0)

	if len(os.Args) < 2 {
		log.Fatal("at least one tag must be specified on the command line")
	}
	tags := os.Args[1:]
	repoRoot, err := gitRoot()
	if err != nil {
		log.Fatal(err)
	}
	goSumLines := make([]string, len(tags))
	group := errgroup.Group{}
	group.SetLimit(runtime.GOMAXPROCS(0))
	for i, tag := range tags {
		group.Go(func() error {
			var err error
			goSumLines[i], err = gosum.CreateFromGitTag(repoRoot, tag)
			return err
		})
	}
	if err := group.Wait(); err != nil {
		log.Fatal(err)
	}
	for _, line := range goSumLines {
		if _, err := os.Stdout.WriteString(line); err != nil {
			log.Fatal(err)
		}
	}
}

func gitRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", errors.New("unable to locate .git directory: you must run this from within a Git repository")
}
