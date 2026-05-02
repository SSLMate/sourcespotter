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

package cooldown

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/internal/cooldown/goproxy"
	"src.agwa.name/go-dbutil"
)

func loadVersions(ctx context.Context, module goproxy.ModulePath, minAge time.Duration) ([]string, error) {
	maxTime := time.Now().Add(-minAge)

	var versions []string
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &versions, `SELECT version FROM record WHERE module = $1 AND observed_at <= $2`, module, maxTime); err != nil {
		return nil, fmt.Errorf("error querying record table: %w", err)
	}
	return versions, nil
}

func redirectUpstream(w http.ResponseWriter, module goproxy.ModulePath, req goproxy.Request) {
	url := "https://proxy.golang.org/" + module.Escaped() + "/" + req.Path()
	w.Header().Set("Location", url)
	w.WriteHeader(http.StatusSeeOther)
}

func serveLatest(ctx context.Context, w http.ResponseWriter, module goproxy.ModulePath, minAge time.Duration) {
	versions, err := loadVersions(ctx, module, minAge)
	if err != nil {
		log.Print(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(versions) == 0 {
		http.Error(w, fmt.Sprintf("no versions of %q found that are at least %s old", module, minAge), http.StatusNotFound)
		return
	}
	semver.Sort(versions)
	redirectUpstream(w, module, goproxy.InfoRequest{Version: goproxy.ModuleVersion(versions[len(versions)-1])})
}

func serveList(ctx context.Context, w http.ResponseWriter, modulePath goproxy.ModulePath, minAge time.Duration) {
	versions, err := loadVersions(ctx, modulePath, minAge)
	if err != nil {
		log.Print(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	for _, version := range versions {
		if !module.IsPseudoVersion(version) {
			fmt.Fprintln(w, version)
		}
	}
}

func Serve(w http.ResponseWriter, httpReq *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	path := strings.TrimPrefix(httpReq.URL.Path, "/")
	duration, path, ok := strings.Cut(path, "/")
	if !ok {
		http.Error(w, `Path does not start with /DURATION/ (e.g. "/24h/")`, http.StatusBadRequest)
		return
	}
	minAge, err := time.ParseDuration(duration)
	if err != nil {
		http.Error(w, `Path does not start with a valid duration (e.g. "/24h/"): `+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.HasPrefix(path, "sumdb/") {
		http.Error(w, "sumdb is not proxied", http.StatusNotFound)
		return
	}
	module, request, err := goproxy.ParseRequestPath(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	switch request := request.(type) {
	case goproxy.LatestRequest:
		serveLatest(httpReq.Context(), w, module, minAge)
	case goproxy.ListRequest:
		serveList(httpReq.Context(), w, module, minAge)
	case goproxy.InfoRequest:
		redirectUpstream(w, module, request)
	case goproxy.ModRequest:
		redirectUpstream(w, module, request)
	case goproxy.ZipRequest:
		redirectUpstream(w, module, request)
	default:
		http.Error(w, "Unsupported request", http.StatusBadRequest)
	}
}
