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

package toolchainvuln

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	"software.sslmate.com/src/sourcespotter"
	basedashboard "software.sslmate.com/src/sourcespotter/internal/dashboard"
	"src.agwa.name/go-dbutil"
)

type vulnRow struct {
	GoVersion   string       `sql:"goversion"`
	CVEID       string       `sql:"cveid"`
	ReleasedAt  time.Time    `sql:"released_at"`
	GoID        sql.NullString `sql:"goid"`
	PublishedAt sql.NullTime `sql:"published_at"`
}

func (v *vulnRow) IsPublished() bool {
	return v.GoID.Valid
}

func (v *vulnRow) GoIDString() string {
	if v.GoID.Valid {
		return v.GoID.String
	}
	return ""
}

func (v *vulnRow) ReleasedAtString() string {
	return v.ReleasedAt.UTC().Format("2006-01-02 15:04:05")
}

func (v *vulnRow) PublishedAtString() string {
	if v.PublishedAt.Valid {
		return v.PublishedAt.Time.UTC().Format("2006-01-02 15:04:05")
	}
	return ""
}

func (v *vulnRow) TimeSinceRelease() string {
	duration := time.Since(v.ReleasedAt)
	return formatDuration(duration)
}

func (v *vulnRow) LagTime() string {
	if !v.PublishedAt.Valid {
		return ""
	}
	duration := v.PublishedAt.Time.Sub(v.ReleasedAt)
	if duration < 0 {
		// Published before release (already in vuln db)
		return "pre-release"
	}
	return formatDuration(duration)
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}

	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func (v *vulnRow) CVELink() string {
	return fmt.Sprintf("https://www.cve.org/CVERecord?id=%s", v.CVEID)
}

func (v *vulnRow) GoVulnLink() string {
	if v.GoID.Valid {
		return fmt.Sprintf("https://pkg.go.dev/vuln/%s", v.GoID.String)
	}
	return ""
}

type dashboard struct {
	Domain string
	Vulns  []vulnRow
}

func (d *dashboard) UnpublishedCount() int {
	count := 0
	for _, v := range d.Vulns {
		if !v.IsPublished() {
			count++
		}
	}
	return count
}

func (d *dashboard) PublishedCount() int {
	count := 0
	for _, v := range d.Vulns {
		if v.IsPublished() {
			count++
		}
	}
	return count
}

func loadDashboard(ctx context.Context) (*dashboard, error) {
	dash := &dashboard{Domain: sourcespotter.Domain}

	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &dash.Vulns,
		`SELECT goversion, cveid, released_at, goid, published_at
		 FROM toolchain_vuln
		 ORDER BY released_at DESC, goversion DESC, cveid`); err != nil {
		return nil, err
	}

	return dash, nil
}

func ServeDashboard(w http.ResponseWriter, req *http.Request) {
	dash, err := loadDashboard(req.Context())
	if err != nil {
		log.Printf("error loading toolchainvuln dashboard: %s", err)
		http.Error(w, "Internal Database Error", 500)
		return
	}
	basedashboard.ServePage(w, req,
		"Go Toolchain Vulnerabilities - Source Spotter",
		"Track when Go toolchain vulnerabilities are published to vuln.go.dev.",
		"toolchainvuln.html", dash)
}
