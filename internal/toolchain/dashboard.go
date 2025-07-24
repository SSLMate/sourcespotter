// Copyright (C) 2025 Opsmate, Inc.
//
// This Source Code Form is subject to the terms of the Mozilla
// Public License, v. 2.0. If a copy of the MPL was not distributed
// with this file, You can obtain one at http://mozilla.org/MPL/2.0/.
//
// This software is distributed WITHOUT A WARRANTY OF ANY KIND.
// See the Mozilla Public License for details.

package toolchain

import (
	"context"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"runtime/debug"
	"slices"
	"time"

	"go/version"
	"golang.org/x/mod/semver"
	"src.agwa.name/go-dbutil"
)

//go:embed templates/*
var content embed.FS

var defaultDashboardTemplate = template.Must(template.ParseFS(content, "templates/dashboard.html"))

type failureRow struct {
	Version string `sql:"version"`
	Status  string `sql:"status"`
	Message string `sql:"message"`
	BuildID []byte `sql:"build_id"`
	InsertedAt time.Time `sql:"inserted_at"`
	LogURL  string
	ZipURL  string
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

type Dashboard struct {
	Verified  []string
	Failures  []failureRow
	Sources   []sourceRow
	BuildInfo *debug.BuildInfo
}

func LoadDashboard(ctx context.Context, db *sql.DB) (*Dashboard, error) {
	dash := new(Dashboard)
	if err := dbutil.QueryAll(ctx, db, &dash.Verified, `SELECT version FROM toolchain_build WHERE status='equal'`); err != nil {
		return nil, err
	}
	semver.Sort(dash.Verified)

	var failures []failureRow
	if err := dbutil.QueryAll(ctx, db, &failures, `SELECT version,status,coalesce(message,'') AS message,build_id,inserted_at FROM toolchain_build WHERE status NOT IN ('equal', 'skipped') ORDER BY inserted_at DESC`); err != nil {
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

	if err := dbutil.QueryAll(ctx, db, &dash.Sources, `SELECT version,url,sha256,downloaded_at FROM toolchain_source`); err != nil {
		return nil, err
	}
	slices.SortFunc(dash.Sources, func(a, b sourceRow) int {
		return version.Compare(a.Version, b.Version)
	})

	dash.BuildInfo, _ = debug.ReadBuildInfo()
	return dash, nil
}

func ServeHTTP(w http.ResponseWriter, req *http.Request, db *sql.DB, template *template.Template) {
	if template == nil {
		template = defaultDashboardTemplate
	}
	dash, err := LoadDashboard(req.Context(), db)
	if err != nil {
		log.Printf("error loading toolchain dashboard: %s", err)
		http.Error(w, "Internal Database Error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Xss-Protection", "0")
	w.WriteHeader(http.StatusOK)
	template.Execute(w, dash)
}
