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

package sumdb

import (
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
	"time"

	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/internal/atom"
)

// ServeFailuresAtom publishes inconsistencies seen in checksum databases as an Atom feed.
func ServeFailuresAtom(w http.ResponseWriter, req *http.Request) {
	dashboard, err := LoadDashboard(req.Context())
	if err != nil {
		log.Printf("error loading dashboard: %s", err)
		http.Error(w, "Internal Database Error", http.StatusInternalServerError)
		return
	}

	feedURL := "https://feeds.api." + sourcespotter.Domain + "/sumdb/failures.atom"
	feed := atom.Feed{
		Xmlns:  "http://www.w3.org/2005/Atom",
		ID:     feedURL,
		Title:  "Checksum Database Audit Failures",
		Author: atom.Person{Name: "Source Spotter on " + sourcespotter.Domain},
		Link:   atom.Link{Rel: "self", Href: feedURL},
	}
	var latest time.Time
	addTime := func(t time.Time) {
		if t.After(latest) {
			latest = t
		}
	}

	for _, sth := range dashboard.InconsistentSTHs {
		addTime(sth.ObservedAt)
		entry := atom.Entry{
			Title:   fmt.Sprintf("Inconsistent STH from %s", sth.SumDB),
			ID:      fmt.Sprintf("%s#sth-%s-%d-%s", feedURL, sth.SumDB, sth.TreeSize, sth.RootHashString()),
			Updated: sth.ObservedAt.UTC().Format(time.RFC3339Nano),
			Content: atom.Content{Type: "text", Body: fmt.Sprintf("SumDB: %s\nTree Size: %d\nSTH Root Hash: %s\nExpected Root Hash: %s\n", sth.SumDB, sth.TreeSize, sth.RootHashString(), sth.CalculatedRootHashString())},
		}
		feed.Entries = append(feed.Entries, entry)
	}

	for _, rec := range dashboard.DuplicateRecords {
		addTime(rec.ObservedAt)
		entry := atom.Entry{
			Title:   fmt.Sprintf("Duplicate record in %s", rec.SumDB),
			ID:      fmt.Sprintf("%s#dup-%s-%d", feedURL, rec.SumDB, rec.Position),
			Updated: rec.ObservedAt.UTC().Format(time.RFC3339Nano),
			Content: atom.Content{Type: "text", Body: fmt.Sprintf("SumDB: %s\nModule: %s\nVersion: %s\nPosition: %d\nPrevious Position: %d\n", rec.SumDB, rec.Module, rec.Version, rec.Position, rec.PreviousPosition)},
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
