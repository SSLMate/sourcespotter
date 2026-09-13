package modules

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/internal/atom"
)

const maxFeedEntries = 10_000

type recordRow struct {
	Module       string    `sql:"module"`
	Version      string    `sql:"version"`
	SourceSHA256 []byte    `sql:"source_sha256"`
	GomodSHA256  []byte    `sql:"gomod_sha256"`
	ObservedAt   time.Time `sql:"observed_at"`
}

func ServeVersionsAtom(w http.ResponseWriter, req *http.Request) {
	module := req.URL.Query().Get("module")
	if module == "" {
		http.Error(w, "Missing module parameter", http.StatusBadRequest)
		return
	}
	pubkey, err := parsePubkeyParam(req.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := req.Context()
	query := `SELECT module,version,source_sha256,gomod_sha256,observed_at FROM record r`
	args := []any{}
	if strings.HasSuffix(module, "/") {
		query += ` WHERE module LIKE $1`
		args = append(args, module+"%")
	} else {
		query += ` WHERE module = $1`
		args = append(args, module)
	}
	if pubkey != nil {
		query += ` AND NOT EXISTS (SELECT 1 FROM authorized_record ar WHERE (ar.pubkey,ar.module,ar.version,ar.source_sha256,ar.gomod_sha256) IS NOT DISTINCT FROM ($2,r.module,r.version,r.source_sha256,r.gomod_sha256))`
		args = append(args, pubkey)
	}
	query += ` ORDER BY module, version, db_id, "position" DESC`
	query += ` LIMIT ` + strconv.FormatInt(maxFeedEntries+1, 10)

	rows, err := sourcespotter.DB.QueryContext(ctx, query, args...)
	if err != nil {
		log.Printf("error loading records: %s", err)
		http.Error(w, "Internal Database Error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	baseURL := "https://feeds.api." + sourcespotter.Domain + "/modules/versions.atom"
	u, _ := url.Parse(baseURL)
	q := u.Query()
	q.Set("module", module)
	for _, param := range []string{"mldsa", "ed25519"} {
		if value := req.URL.Query().Get(param); value != "" {
			q.Set(param, value)
		}
	}
	u.RawQuery = q.Encode()
	feedURL := u.String()

	feed := atom.Feed{
		Xmlns:  "http://www.w3.org/2005/Atom",
		ID:     feedURL,
		Title:  fmt.Sprintf("Versions of %s", module),
		Author: atom.Person{Name: "Source Spotter on " + sourcespotter.Domain},
		Link:   atom.Link{Rel: "self", Href: feedURL},
	}
	var latest time.Time
	for numRows := 0; rows.Next(); numRows++ {
		if numRows == maxFeedEntries {
			http.Error(w, fmt.Sprintf("Sorry, there are more than %d versions matching %s and we can't create a feed that large", maxFeedEntries, module), http.StatusInternalServerError)
			return
		}

		var r recordRow
		if err := rows.Scan(&r.Module, &r.Version, &r.SourceSHA256, &r.GomodSHA256, &r.ObservedAt); err != nil {
			log.Printf("error scanning record: %s", err)
			http.Error(w, "Internal Database Error", http.StatusInternalServerError)
			return
		}
		if semver.Prerelease(r.Version) != "" {
			continue
		}
		if r.ObservedAt.After(latest) {
			latest = r.ObservedAt
		}
		body := fmt.Sprintf("h1:%s\n", base64.StdEncoding.EncodeToString(r.SourceSHA256))
		body += fmt.Sprintf("go.mod h1:%s\n", base64.StdEncoding.EncodeToString(r.GomodSHA256))
		entry := atom.Entry{
			Title:   fmt.Sprintf("%s@%s", r.Module, r.Version),
			ID:      fmt.Sprintf("%s#%s@%s", baseURL, r.Module, r.Version),
			Updated: r.ObservedAt.UTC().Format(time.RFC3339Nano),
			Content: atom.Content{Type: "text", Body: body},
		}
		feed.Entries = append(feed.Entries, entry)
	}
	if err := rows.Err(); err != nil {
		log.Printf("error reading records: %s", err)
		http.Error(w, "Internal Database Error", http.StatusInternalServerError)
		return
	}
	if latest.IsZero() {
		latest = time.Now()
	}
	feed.Updated = latest.UTC().Format(time.RFC3339Nano)

	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	enc.Encode(feed)
}
