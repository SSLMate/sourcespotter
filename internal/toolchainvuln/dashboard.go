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
	return v.ReleasedAt.UTC().Format("2006-01-02")
}

func (v *vulnRow) PublishedAtString() string {
	if v.PublishedAt.Valid {
		return v.PublishedAt.Time.UTC().Format("2006-01-02")
	}
	return ""
}

func (v *vulnRow) ReleaseSearchLink() string {
	return fmt.Sprintf("https://groups.google.com/g/golang-announce/search?q=Go%%20%s%%20released", v.GoVersion)
}

func (v *vulnRow) TimeSinceRelease() string {
	days := int(time.Since(v.ReleasedAt).Hours() / 24)
	return formatDays(days)
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
	days := int(duration.Hours() / 24)
	return formatDays(days)
}

func formatDays(days int) string {
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

func (v *vulnRow) CVELink() string {
	return fmt.Sprintf("https://pkg.go.dev/search?q=%s&m=vuln", v.CVEID)
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
		 ORDER BY released_at ASC, goversion ASC, cveid`); err != nil {
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
