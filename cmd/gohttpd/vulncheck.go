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
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

func getVulncheck(w http.ResponseWriter, req *http.Request) error {
	rc := http.NewResponseController(w)
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

	cmd := goCommand(ctx, tempDir, "govulncheck", args...)
	cmd.Env = append(cmd.Env,
		"GOOS="+goos,
		"GOARCH="+goarch,
	)
	if format == "text" {
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
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
	} else if format == "sarif" || format == "openvex" {
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
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
	} else if format == "json" {
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("error executing govulncheck: %w", err)
		}
		buf := new(bytes.Buffer)
		dec := json.NewDecoder(stdout)
		dec.UseNumber()
		wroteHeader := false
		for dec.More() {
			var item json.RawMessage
			if err := dec.Decode(&item); err != nil {
				log.Printf("received invalid JSON from govulncheck: %s", err)
				break
			}
			if err := json.Compact(buf, item); err != nil {
				log.Printf("error compacting JSON from govulncheck: %s", err)
				break
			}
			if !wroteHeader {
				w.Header().Set("Content-Type", "application/jsonl")
				w.Header().Set("X-Content-Type-Options", "nosniff")
				w.WriteHeader(http.StatusOK)
				wroteHeader = true
			}
			io.Copy(w, buf)
			w.Write([]byte{'\n'})
			rc.Flush()
			buf.Reset()
		}
		io.Copy(io.Discard, stdout)
		if err := cmd.Wait(); err != nil {
			if stderr.Len() > 0 {
				err = fmt.Errorf("error from govulncheck: %s", strings.TrimSpace(stderr.String()))
			}
			if !wroteHeader {
				return err
			}
			if ctx.Err() == nil {
				log.Printf("govulncheck failed after it started streaming JSON: %s", err)
			}
		}
	} else {
		return fmt.Errorf("unknown format %q", format)
	}
	return nil
}
