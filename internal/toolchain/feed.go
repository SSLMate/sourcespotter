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
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"go/version"
	"io"
	"log"
	"net/http"
	"slices"
	"time"

	"golang.org/x/mod/semver"
	"software.sslmate.com/src/sourcespotter"
	"src.agwa.name/go-dbutil"
)

// Atom feed structures

type atomFeed struct {
	XMLName xml.Name   `xml:"feed"`
	Xmlns   string     `xml:"xmlns,attr"`
	ID      string     `xml:"id"`
	Title   string     `xml:"title"`
	Updated string     `xml:"updated"`
	Author  atomPerson `xml:"author"`
	Link    atomLink   `xml:"link"`
	Entries []atomItem `xml:"entry"`
}

type atomItem struct {
	Title   string      `xml:"title"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Content atomContent `xml:"content"`
}

type atomContent struct {
	Type string `xml:"type,attr"`
	Body string `xml:",chardata"`
}

type atomPerson struct {
	Name string `xml:"name"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr,omitempty"`
	Href string `xml:"href,attr"`
}

func ServeFailuresAtom(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	var rows []struct {
		Version    string    `sql:"version"`
		Status     string    `sql:"status"`
		Message    string    `sql:"message"`
		BuildID    []byte    `sql:"build_id"`
		InsertedAt time.Time `sql:"inserted_at"`
	}
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &rows, `SELECT version,status,coalesce(message,'') AS message,build_id,inserted_at FROM toolchain_build WHERE status NOT IN ('equal','skipped') ORDER BY inserted_at DESC`); err != nil {
		log.Printf("error querying toolchain failures: %s", err)
		http.Error(w, "Internal Database Error", 500)
		return
	}

	feedURL := "https://" + sourcespotter.Domain + "/toolchain/failures.atom"
	feed := atomFeed{
		Xmlns:  "http://www.w3.org/2005/Atom",
		ID:     feedURL,
		Title:  "Toolchain Build Failures",
		Author: atomPerson{Name: "Source Spotter on " + sourcespotter.Domain},
		Link:   atomLink{Rel: "self", Href: feedURL},
	}
	if len(rows) > 0 {
		feed.Updated = rows[0].InsertedAt.UTC().Format(time.RFC3339Nano)
	} else {
		feed.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	}

	for _, row := range rows {
		entry := atomItem{
			Title:   fmt.Sprintf("%s %s", row.Version, row.Status),
			ID:      fmt.Sprintf("%s#%d-%s", feedURL, row.InsertedAt.UnixNano(), row.Version),
			Updated: row.InsertedAt.UTC().Format(time.RFC3339Nano),
		}
		body := row.Message
		if len(row.BuildID) > 0 {
			hexid := hex.EncodeToString(row.BuildID)
			logKey := fmt.Sprintf("out/%s.%s.log", row.Version, hexid)
			if r, err := getObject(ctx, logKey); err == nil {
				b, err2 := io.ReadAll(r)
				r.Close()
				if err2 == nil {
					if body != "" {
						body += "\n\n"
					}
					body += string(b)
				}
			} else {
				log.Printf("error retrieving log %s: %s", logKey, err)
			}
			zipKey := fmt.Sprintf("out/%s.%s.zip", row.Version, hexid)
			if url, err := presignGetObject(ctx, zipKey); err == nil {
				if body != "" {
					body += "\n\n"
				}
				body += "zip: " + url
			} else {
				log.Printf("error presigning zip %s: %s", zipKey, err)
			}
		}
		entry.Content = atomContent{Type: "text", Body: body}
		feed.Entries = append(feed.Entries, entry)
	}

	data, err := xml.MarshalIndent(feed, "", "  ")
	if err != nil {
		log.Printf("error encoding Atom feed: %s", err)
		http.Error(w, "Internal Server Error", 500)
		return
	}

	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func ServeSourcesCSV(w http.ResponseWriter, req *http.Request) {
	type sourceCSVRow struct {
		Version      string    `sql:"version"`
		URL          string    `sql:"url"`
		SHA256       []byte    `sql:"sha256"`
		DownloadedAt time.Time `sql:"downloaded_at"`
	}

	ctx := req.Context()
	var rows []sourceCSVRow
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &rows, `SELECT version,url,sha256,downloaded_at FROM toolchain_source`); err != nil {
		log.Printf("error querying toolchain sources: %s", err)
		http.Error(w, "Internal Database Error", 500)
		return
	}
	slices.SortFunc(rows, func(a, b sourceCSVRow) int {
		return version.Compare(a.Version, b.Version)
	})
	w.Header().Set("Content-Type", "text/csv; charset=UTF-8; header=present")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	cw.Write([]string{"Version", "URL", "SHA256", "Download time"})
	for _, row := range rows {
		cw.Write([]string{
			row.Version,
			row.URL,
			hex.EncodeToString(row.SHA256),
			row.DownloadedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	cw.Flush()
}

func ServeToolchainsCSV(w http.ResponseWriter, req *http.Request) {
	type row struct {
		Version    string              `sql:"version"`
		Status     sql.Null[string]    `sql:"status"`
		Message    sql.Null[string]    `sql:"message"`
		InsertedAt sql.Null[time.Time] `sql:"inserted_at"`
	}
	ctx := req.Context()
	var rows []*row
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &rows, `SELECT record.version,toolchain_build.status,toolchain_build.message,toolchain_build.inserted_at FROM record LEFT JOIN toolchain_build USING (version) WHERE module='golang.org/toolchain'`); err != nil {
		log.Printf("error querying toolchains: %s", err)
		http.Error(w, "Internal Database Error", 500)
		return
	}
	slices.SortFunc(rows, func(a, b *row) int {
		return semver.Compare(a.Version, b.Version)
	})
	w.Header().Set("Content-Type", "text/csv; charset=UTF-8; header=present")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	cw.Write([]string{"Version", "Status", "Message", "Build Time"})
	for _, row := range rows {
		var t string
		if row.InsertedAt.Valid {
			t = row.InsertedAt.V.UTC().Format(time.RFC3339Nano)
		}
		cw.Write([]string{
			row.Version,
			row.Status.V,
			row.Message.V,
			t,
		})
	}
	cw.Flush()
}
