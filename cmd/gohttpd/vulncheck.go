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

package main

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func getVulncheck(w http.ResponseWriter, req *http.Request) error {
	ctx := req.Context()
	q := req.URL.Query()
	var (
		packages = q["package"]
		goos     = q.Get("goos")
		goarch   = q.Get("goarch")
		tags     = q["tag"]
		show     = q["show"]
		test     = q.Get("test") == "1"
		format   = cmp.Or(q.Get("format"), "text")
	)

	tempDir, err := tempModule(ctx, packages...)
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	args := []string{"-format", format}
	if test {
		args = append(args, "-test")
	}
	if len(tags) != 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	if len(show) != 0 {
		args = append(args, "-show", strings.Join(show, ","))
	}
	args = append(args, "--")
	args = packagePaths(args, packages)

	var stdout, stderr bytes.Buffer
	cmd := goCommand(ctx, tempDir, "govulncheck", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = append(cmd.Env,
		"GOOS="+goos,
		"GOARCH="+goarch,
	)
	if format == "text" {
		if err := cmd.Run(); err == nil {
			// no vulnerabilities found
			w.WriteHeader(http.StatusNoContent)
		} else if stdout.Len() > 0 {
			// vulnerabilities found, details written to stdout
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			io.Copy(w, &stdout)
		} else if stderr.Len() > 0 {
			return fmt.Errorf("error from govulncheck: %s", strings.TrimSpace(stderr.String()))
		} else {
			return fmt.Errorf("error executing govulncheck: %w", err)
		}
	} else {
		if err := cmd.Run(); err == nil {
			// always exists successfully even if vulnerabilities were found
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			io.Copy(w, &stdout)
		} else if stderr.Len() > 0 {
			return fmt.Errorf("error from govulncheck: %s", strings.TrimSpace(stderr.String()))
		} else {
			return fmt.Errorf("error executing govulncheck: %w", err)
		}
	}
	return nil
}
