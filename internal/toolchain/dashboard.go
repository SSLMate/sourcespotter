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

package toolchain

import (
	"context"
	"embed"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"slices"
	"time"

	"go/version"
	"golang.org/x/mod/semver"
	"software.sslmate.com/src/sourcespotter"
	basedashboard "software.sslmate.com/src/sourcespotter/internal/dashboard"
	"src.agwa.name/go-dbutil"
)

//go:embed templates/*
var templates embed.FS

var dashboardTemplate = basedashboard.ParseTemplate(templates, "templates/dashboard.html")

type failureRow struct {
	Version    string    `sql:"version"`
	Status     string    `sql:"status"`
	Message    string    `sql:"message"`
	BuildID    []byte    `sql:"build_id"`
	InsertedAt time.Time `sql:"inserted_at"`
	LogURL     string
	ZipURL     string
}

func (f *failureRow) StatusString() string {
	s := f.Status
	if f.Message != "" {
		s += ": " + f.Message
	}
	return s
}

func (f *failureRow) InsertedAtString() string {
	return f.InsertedAt.UTC().Format("2006-01-02 15:04:05")
}

type sourceRow struct {
	Version      string    `sql:"version"`
	URL          string    `sql:"url"`
	SHA256       []byte    `sql:"sha256"`
	DownloadedAt time.Time `sql:"downloaded_at"`
}

func (s *sourceRow) SHA256String() string {
	return hex.EncodeToString(s.SHA256)
}

func (s *sourceRow) DownloadedAtString() string {
	return s.DownloadedAt.UTC().Format("2006-01-02 15:04:05")
}

type dashboard struct {
	Domain   string
	Verified []string
	Failures []failureRow
	Sources  []sourceRow
}

func loadDashboard(ctx context.Context) (*dashboard, error) {
	dash := &dashboard{Domain: sourcespotter.Domain}

	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &dash.Verified, `SELECT version FROM toolchain_build WHERE status='equal'`); err != nil {
		return nil, err
	}
	semver.Sort(dash.Verified)

	var failures []failureRow
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &failures, `SELECT version,status,coalesce(message,'') AS message,build_id,inserted_at FROM toolchain_build WHERE status NOT IN ('equal', 'skipped') ORDER BY inserted_at DESC`); err != nil {
		return nil, err
	}
	for i := range failures {
		if len(failures[i].BuildID) > 0 {
			hexid := hex.EncodeToString(failures[i].BuildID)
			logKey := fmt.Sprintf("out/%s.%s.log", failures[i].Version, hexid)
			zipKey := fmt.Sprintf("out/%s.%s.zip", failures[i].Version, hexid)
			if url, err := presignGetObject(ctx, logKey); err == nil {
				failures[i].LogURL = url
			} else {
				return nil, err
			}
			if url, err := presignGetObject(ctx, zipKey); err == nil {
				failures[i].ZipURL = url
			} else {
				return nil, err
			}
		}
	}
	dash.Failures = failures

	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &dash.Sources, `SELECT version,url,sha256,downloaded_at FROM toolchain_source`); err != nil {
		return nil, err
	}
	slices.SortFunc(dash.Sources, func(a, b sourceRow) int {
		return version.Compare(a.Version, b.Version)
	})

	return dash, nil
}

func ServeDashboard(w http.ResponseWriter, req *http.Request) {
	dash, err := loadDashboard(req.Context())
	if err != nil {
		log.Printf("error loading toolchain dashboard: %s", err)
		http.Error(w, "Internal Database Error", 500)
		return
	}
	basedashboard.ServePage(w, req, "Go Toolchain Reproducibility - Source Spotter", dashboardTemplate, dash)
}
