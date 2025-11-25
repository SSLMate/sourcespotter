package deps

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"software.sslmate.com/src/sourcespotter"
)

type depCounts struct {
	Golang     int
	FirstParty int
	ThirdParty int
}

func ServeBadge(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	query := req.URL.RawQuery

	firstPartyPrefix := req.URL.Query().Get("firstparty")
	if firstPartyPrefix != "" && !strings.HasSuffix(firstPartyPrefix, "/") {
		firstPartyPrefix += "/"
	}

	noCache := strings.Contains(req.Header.Get("Cache-Control"), "no-cache")
	counts, fresh, err := getCachedCounts(ctx, noCache, query)
	if err != nil {
		http.Error(w, "Internal Database Error", http.StatusInternalServerError)
		return
	}
	if !fresh {
		counts, err = fetchCounts(ctx, query, firstPartyPrefix)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if err := storeCounts(ctx, query, counts); err != nil {
			http.Error(w, "Internal Database Error", http.StatusInternalServerError)
			return
		}
	}

	var text string
	//var width int

	//if firstPartyPrefix == "" {
	//	text = fmt.Sprintf("go/x: %d, 3rd party: %d", counts.Golang, counts.ThirdParty)
	//	width = 205
	//} else {
	//	text = fmt.Sprintf("go/x: %d, 1st party: %d, 3rd party: %d", counts.Golang, counts.FirstParty, counts.ThirdParty)
	//	width = 275
	//}

	text = fmt.Sprintf("%d third-party", counts.ThirdParty)

	w.Header().Set("Cache-Control", "public, max-age=3600, must-revalidate")
	http.Redirect(w, req, fmt.Sprintf("https://img.shields.io/badge/dependencies-%s-blue", strings.ReplaceAll(text, "-", "--")), http.StatusSeeOther)

	//w.Header().Set("Content-Type", "image/svg+xml;charset=utf-8")
	//w.Header().Set("X-Content-Type-Options", "nosniff")
	//fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="dependencies: %s"><title>dependencies: %s</title><linearGradient id="s" x2="0" y2="100%%"><stop offset="0" stop-color="#bbb" stop-opacity=".1"/><stop offset="1" stop-opacity=".1"/></linearGradient><clipPath id="r"><rect width="%d" height="20" rx="3" fill="#fff"/></clipPath><g clip-path="url(#r)"><rect width="85" height="20" fill="#555"/><rect x="85" width="%d" height="20" fill="#007ec6"/><rect width="%d" height="20" fill="url(#s)"/></g><g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" text-rendering="geometricPrecision" font-size="110"><text aria-hidden="true" x="435" y="150" fill="#010101" fill-opacity=".3" transform="scale(.1)">dependencies</text><text x="435" y="140" transform="scale(.1)" fill="#fff">dependencies</text><text aria-hidden="true" x="%d" y="150" fill="#010101" fill-opacity=".3" transform="scale(.1)" white-space="pre">%s</text><text x="%d" y="140" transform="scale(.1)" fill="#fff" white-space="pre">%s</text></g></svg>`, width, text, text, width, width-85, width, 850+(width*10-850)/2, text, 850+(width*10-850)/2, text)
}

func getCachedCounts(ctx context.Context, noCache bool, query string) (depCounts, bool, error) {
	if noCache {
		return depCounts{}, false, nil
	}
	var (
		counts     depCounts
		insertedAt time.Time
	)
	err := sourcespotter.DB.QueryRowContext(ctx, `SELECT golang, firstparty, thirdparty, inserted_at FROM dep_count_cache WHERE query=$1`, query).Scan(&counts.Golang, &counts.FirstParty, &counts.ThirdParty, &insertedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return depCounts{}, false, nil
	}
	if err != nil {
		return depCounts{}, false, err
	}
	if time.Since(insertedAt) > time.Hour {
		return counts, false, nil
	}
	return counts, true, nil
}

func fetchCounts(ctx context.Context, query, firstPartyPrefix string) (depCounts, error) {
	goAPI := fmt.Sprintf("https://go.api.%s/deps", sourcespotter.Domain)
	if query != "" {
		goAPI += "?" + query
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, goAPI, nil)
	if err != nil {
		return depCounts{}, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return depCounts{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return depCounts{}, fmt.Errorf("Request to %s failed with status %d: %s", goAPI, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return parseCounts(resp.Body, firstPartyPrefix)
}

func storeCounts(ctx context.Context, query string, counts depCounts) error {
	_, err := sourcespotter.DB.ExecContext(ctx, `INSERT INTO dep_count_cache (query, golang, firstparty, thirdparty) VALUES ($1, $2, $3, $4) ON CONFLICT (query) DO UPDATE SET inserted_at=excluded.inserted_at, golang=excluded.golang, firstparty=excluded.firstparty, thirdparty=excluded.thirdparty`, query, counts.Golang, counts.FirstParty, counts.ThirdParty)
	return err
}

func parseCounts(r io.Reader, firstPartyPrefix string) (depCounts, error) {
	scanner := bufio.NewScanner(r)
	moduleMap := make(map[string]struct{})
	mainModule := ""

	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())
		if trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 3 {
			continue
		}
		depOnly, module := fields[0], fields[1]
		moduleMap[module] = struct{}{}
		if depOnly == "false" {
			mainModule = module
		}
	}
	if err := scanner.Err(); err != nil {
		return depCounts{}, err
	}

	delete(moduleMap, mainModule)

	var counts depCounts
	for module := range moduleMap {
		switch {
		case strings.HasPrefix(module, "golang.org/"):
			counts.Golang++
		case firstPartyPrefix != "" && strings.HasPrefix(module, firstPartyPrefix):
			counts.FirstParty++
		default:
			counts.ThirdParty++
		}
	}

	return counts, nil
}
