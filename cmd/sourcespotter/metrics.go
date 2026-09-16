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

package main

import (
	"context"
	"log"
	"net/http"

	"software.sslmate.com/src/sourcespotter/internal/prom"
	"software.sslmate.com/src/sourcespotter/internal/sumdb"
	"software.sslmate.com/src/sourcespotter/internal/telemetry"
	"software.sslmate.com/src/sourcespotter/internal/toolchain"
	"software.sslmate.com/src/sourcespotter/internal/toolchainvuln"
)

var metricsCollectors = []func(context.Context) ([]*prom.Family, error){
	sumdb.Metrics,
	toolchain.Metrics,
	telemetry.Metrics,
	toolchainvuln.Metrics,
}

// serveMetrics exports every Prometheus metric that does not require
// request parameters.  Per-module metrics are served by modules.ServeMetrics.
func serveMetrics(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	var families []*prom.Family
	for _, collect := range metricsCollectors {
		f, err := collect(ctx)
		if err != nil {
			log.Printf("error collecting metrics: %s", err)
			http.Error(w, "Internal Database Error", http.StatusInternalServerError)
			return
		}
		families = append(families, f...)
	}
	prom.Serve(w, families)
}
