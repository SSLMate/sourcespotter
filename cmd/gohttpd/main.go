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

// gohttpd is a daemon that serves certain go commands over HTTP
package main

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"src.agwa.name/go-listener"
	_ "src.agwa.name/go-listener/tls"
	"src.agwa.name/go-util/logfilter"
)

var gopath string

func main() {
	var listenFlag []string
	flag.StringVar(&gopath, "gopath", "", "$GOPATH when executing go")
	flag.Func("listen", "Run HTTP server on `LISTENER`, in go-listener syntax (repeatable)", func(arg string) error {
		listenFlag = append(listenFlag, arg)
		return nil
	})
	flag.Parse()

	if len(gopath) == 0 {
		log.Fatal("-gopath flag not provided")
	}
	if len(listenFlag) == 0 {
		log.Fatal("-listen flag not provided")
	}

	listeners, err := listener.OpenAll(listenFlag)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.CloseAll(listeners)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /deps", handler(getDeps))
	//mux.HandleFunc("GET /modfetch", handler(getModfetch))
	mux.HandleFunc("GET /vulncheck", handler(getVulncheck))

	server := &http.Server{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 90 * time.Second,
		IdleTimeout:  3 * time.Second,
		Handler:      mux,
		ErrorLog:     logfilter.New(log.Default(), logfilter.HTTPServerErrors),
	}

	for _, listener := range listeners {
		go func() {
			log.Fatal(server.Serve(listener))
		}()
	}
	select {}
}

type handlerFunc = func(http.ResponseWriter, *http.Request) error

func handler(h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		err := h(w, r)
		if err != nil {
			http.Error(w, err.Error(), 500)
		}
	}
}

func goCommand(ctx context.Context, dir string, name string, arg ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, arg...)
	cmd.Env = []string{
		"CGO_ENABLED=0",
		// -modcacherw for easier GOPATH management, -buildvcs=false to prevent arbitrary code execution via VCS
		"GOFLAGS=-modcacherw -buildvcs=false",
		"GOPATH=" + gopath,
		// set GOPROXY and GOVCS to always get direct from module proxy instead of using VCS, as it's safer
		"GOPROXY=https://proxy.golang.org",
		"GOVCS=*:off",
		"HOME=" + os.Getenv("HOME"),
		"LOGNAME=" + os.Getenv("LOGNAME"),
		"PATH=" + cmp.Or(os.Getenv("PATH"), "/usr/local/bin:/usr/bin/:/bin"),
		"USER=" + os.Getenv("USER"),
	}
	cmd.Dir = dir
	return cmd
}

func tempModule(ctx context.Context, test bool, packages ...string) (moduleDir string, err error) {
	tempDir, err := os.MkdirTemp("", "gohttpd-tmp-")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer func() {
		if err != nil {
			os.RemoveAll(tempDir)
		}
	}()
	if out, err := goCommand(ctx, tempDir, "go", "mod", "init", "tmp").CombinedOutput(); err != nil {
		if len(out) != 0 {
			return "", fmt.Errorf("error initializing temporary module: %s", bytes.TrimSpace(out))
		}
		return "", fmt.Errorf("error executing 'go mod init': %w", err)
	}
	if len(packages) > 0 {
		args := []string{"get"}
		if test {
			args = append(args, "-t")
		}
		args = append(args, "--")
		args = append(args, packages...)
		if out, err := goCommand(ctx, tempDir, "go", args...).CombinedOutput(); err != nil {
			if len(out) != 0 {
				return "", errors.New(string(bytes.TrimSpace(out)))
			}
			return "", fmt.Errorf("error executing 'go get': %w", err)
		}
	}
	return tempDir, nil
}

func packagePaths(paths []string, packages []string) []string {
	for _, pkg := range packages {
		path, _, _ := strings.Cut(pkg, "@")
		paths = append(paths, path)
	}
	return paths
}
