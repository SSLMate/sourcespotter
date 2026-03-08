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
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
	"time"

	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/internal/atom"
	"src.agwa.name/go-dbutil"
)

type unpublishedVulnRow struct {
	GoVersion  string    `sql:"goversion"`
	CVEID      string    `sql:"cveid"`
	ReleasedAt time.Time `sql:"released_at"`
}

func (v *unpublishedVulnRow) CVELink() string {
	return fmt.Sprintf("https://pkg.go.dev/search?q=%s&m=vuln", v.CVEID)
}

func (v *unpublishedVulnRow) ReleaseSearchLink() string {
	return fmt.Sprintf("https://groups.google.com/g/golang-announce/search?q=Go%%20%s%%20released", v.GoVersion)
}

const defaultMinAge time.Duration = 0

// ServeUnpublishedAtom publishes an Atom feed of toolchain vulnerabilities
// that have been released for more than a specified duration but are not yet published to vuln.go.dev.
// The minimum age can be specified via the "min_age" query parameter (default: 24h).
func ServeUnpublishedAtom(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	minAge := defaultMinAge
	if minAgeParam := req.URL.Query().Get("min_age"); minAgeParam != "" {
		var err error
		minAge, err = time.ParseDuration(minAgeParam)
		if err != nil {
			http.Error(w, "Invalid min_age parameter: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	var rows []unpublishedVulnRow
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &rows,
		`SELECT goversion, cveid, released_at
		 FROM toolchain_vuln
		 WHERE goid IS NULL
		   AND released_at < $1
		 ORDER BY released_at ASC, goversion ASC, cveid`,
		time.Now().Add(-minAge)); err != nil {
		log.Printf("error querying unpublished toolchain vulns: %s", err)
		http.Error(w, "Internal Database Error", http.StatusInternalServerError)
		return
	}

	feedURL := "https://feeds.api." + sourcespotter.Domain + "/toolchainvuln/unpublished.atom?min_age=" + minAge.String()
	feedTitle := "Unpublished Go Toolchain Vulnerabilities"
	if minAge > 0 {
		feedTitle += fmt.Sprintf(" (>%s)", minAge)
	}
	feed := atom.Feed{
		Xmlns:  "http://www.w3.org/2005/Atom",
		ID:     feedURL,
		Title:  feedTitle,
		Author: atom.Person{Name: "Source Spotter on " + sourcespotter.Domain},
		Link:   atom.Link{Rel: "self", Href: feedURL},
	}

	var latest time.Time
	for _, row := range rows {
		if row.ReleasedAt.After(latest) {
			latest = row.ReleasedAt
		}

		days := int(time.Since(row.ReleasedAt).Hours() / 24)
		var daysStr string
		if days == 1 {
			daysStr = "1 day"
		} else {
			daysStr = fmt.Sprintf("%d days", days)
		}

		entry := atom.Entry{
			Title:   fmt.Sprintf("%s %s (unpublished for %s)", row.GoVersion, row.CVEID, daysStr),
			ID:      fmt.Sprintf("%s#%s-%s", feedURL, row.GoVersion, row.CVEID),
			Updated: row.ReleasedAt.UTC().Format(time.RFC3339Nano),
			Content: atom.Content{
				Type: "text",
				Body: fmt.Sprintf("Go Version: %s\nCVE: %s\nReleased: %s\nUnpublished for: %s\n\nCVE Link: %s\nRelease Announcement: %s\n",
					row.GoVersion,
					row.CVEID,
					row.ReleasedAt.UTC().Format("2006-01-02"),
					daysStr,
					row.CVELink(),
					row.ReleaseSearchLink()),
			},
		}
		feed.Entries = append(feed.Entries, entry)
	}

	if latest.IsZero() {
		latest = time.Now()
	}
	feed.Updated = latest.UTC().Format(time.RFC3339Nano)

	data, err := xml.MarshalIndent(feed, "", "  ")
	if err != nil {
		log.Printf("error encoding Atom feed: %s", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
