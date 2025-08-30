package telemetry

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/gob"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
	"time"

	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/internal/atom"
	"src.agwa.name/go-dbutil"
)

type counterCSVRow struct {
	Program string `sql:"program"`
	Type    string `sql:"type"`
	Name    string `sql:"name"`
}

type counterAtomRow struct {
	Program         string    `sql:"program"`
	Type            string    `sql:"type"`
	Name            string    `sql:"name"`
	FirstObservedAt time.Time `sql:"first_observed_at"`
}

func counterHash(program, typ, name string) string {
	h := sha256.New()
	gob.NewEncoder(h).Encode(struct {
		Program string
		Type    string
		Name    string
	}{program, typ, name})
	return hex.EncodeToString(h.Sum(nil))
}

func ServeCountersCSV(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	var rows []counterCSVRow
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &rows, `select distinct program,type,name from telemetry_counter order by program,type,name`); err != nil {
		log.Printf("error querying telemetry counters: %s", err)
		http.Error(w, "Internal Database Error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=UTF-8; header=present")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	cw.Write([]string{"Program", "Type", "Name"})
	for _, row := range rows {
		cw.Write([]string{row.Program, row.Type, row.Name})
	}
	cw.Flush()
}

func ServeCountersAtom(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	var rows []counterAtomRow
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &rows, `select tc.program,tc.type,tc.name,min(observed_at) as first_observed_at from telemetry_counter tc join record on record.module='golang.org/x/telemetry/config' and record.version=tc.version group by tc.program,tc.type,tc.name order by first_observed_at desc`); err != nil {
		log.Printf("error querying telemetry counters: %s", err)
		http.Error(w, "Internal Database Error", 500)
		return
	}

	feedURL := "https://feeds.api." + sourcespotter.Domain + "/telemetry/counters.atom"
	feed := atom.Feed{
		Xmlns:  "http://www.w3.org/2005/Atom",
		ID:     feedURL,
		Title:  "Go Telemetry Counters",
		Author: atom.Person{Name: "Source Spotter on " + sourcespotter.Domain},
		Link:   atom.Link{Rel: "self", Href: feedURL},
	}
	if len(rows) > 0 {
		feed.Updated = rows[0].FirstObservedAt.UTC().Format(time.RFC3339Nano)
	} else {
		feed.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	}

	for _, row := range rows {
		entry := atom.Entry{
			Title:   row.Name,
			ID:      fmt.Sprintf("%s#%s", feedURL, counterHash(row.Program, row.Type, row.Name)),
			Updated: row.FirstObservedAt.UTC().Format(time.RFC3339Nano),
			Content: atom.Content{Type: "text", Body: fmt.Sprintf("Program: %s\nType: %s\nName: %s", row.Program, row.Type, row.Name)},
		}
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
	w.Header().Set("Cache-Control", "public, max-age=86400, must-revalidate")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
