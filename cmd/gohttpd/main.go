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
	"cmp"
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/exec"
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
	mux.HandleFunc("GET /modfetch", handler(getModfetch))

	server := &http.Server{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 30 * time.Second,
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
		err := h(w, r)
		if err != nil {
			http.Error(w, err.Error(), 500)
		}
	}
}

func goCommand(ctx context.Context, dir string, arg ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "go", arg...)
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
