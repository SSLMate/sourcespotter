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
	"log"
	"net/http"
	"time"

	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/internal/dashboard"
	"software.sslmate.com/src/sourcespotter/internal/sths"
	"software.sslmate.com/src/sourcespotter/internal/sumdb"
	"software.sslmate.com/src/sourcespotter/internal/toolchain"
	"src.agwa.name/go-util/logfilter"
)

func newHTTPServer() *http.Server {
	domain := sourcespotter.Domain

	mux := http.NewServeMux()
	mux.HandleFunc("GET "+domain+"/{$}", dashboard.ServeHome)
	mux.Handle("GET "+domain+"/assets/", http.FileServerFS(dashboard.Assets))
	mux.HandleFunc("GET "+domain+"/sumdb/{$}", sumdb.ServeDashboard)
	mux.HandleFunc("GET "+domain+"/sumdb/failures.atom", sumdb.ServeFailuresAtom)
	mux.HandleFunc("GET "+domain+"/toolchain/{$}", toolchain.ServeDashboard)
	mux.HandleFunc("GET "+domain+"/toolchain/failures.atom", toolchain.ServeFailuresAtom)
	mux.HandleFunc("GET "+domain+"/toolchain/sources.csv", toolchain.ServeSourcesCSV)
	mux.HandleFunc("GET "+domain+"/toolchain/toolchains.csv", toolchain.ServeToolchainsCSV)
	mux.HandleFunc("GET gossip.api."+domain+"/{address}", sths.ServeGossip)
	mux.HandleFunc("POST gossip.api."+domain+"/{address}", sths.ReceiveGossip)

	return &http.Server{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  3 * time.Second,
		Handler:      mux,
		ErrorLog:     logfilter.New(log.Default(), logfilter.HTTPServerErrors),
	}
}
