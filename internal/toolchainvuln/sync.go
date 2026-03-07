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
	"encoding/json"
	"log"
	"strings"
	"time"

	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/internal/httpclient"
	"src.agwa.name/go-dbutil"
)

const vulnIndexURL = "https://vuln.go.dev/index/vulns.json"

// VulnEntry represents an entry in the vuln.go.dev index.
type VulnEntry struct {
	ID       string   `json:"id"`
	Modified string   `json:"modified"`
	Aliases  []string `json:"aliases"`
}

// UnpublishedVuln represents a toolchain_vuln row with null goid.
type UnpublishedVuln struct {
	GoVersion string `sql:"goversion"`
	CVEID     string `sql:"cveid"`
}

// FetchVulnIndex downloads and parses the vuln.go.dev index.
func FetchVulnIndex(ctx context.Context) ([]VulnEntry, error) {
	data, err := httpclient.DownloadBytes(ctx, vulnIndexURL)
	if err != nil {
		return nil, err
	}

	var entries []VulnEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}

	return entries, nil
}

// BuildCVEToGoIDMap creates a map from CVE ID to (GO ID, modified time).
func BuildCVEToGoIDMap(entries []VulnEntry) map[string]struct {
	GoID     string
	Modified time.Time
} {
	result := make(map[string]struct {
		GoID     string
		Modified time.Time
	})

	for _, entry := range entries {
		modified, err := time.Parse(time.RFC3339, entry.Modified)
		if err != nil {
			// Skip entries with invalid timestamps
			continue
		}

		for _, alias := range entry.Aliases {
			if strings.HasPrefix(alias, "CVE-") {
				result[alias] = struct {
					GoID     string
					Modified time.Time
				}{GoID: entry.ID, Modified: modified}
			}
		}
	}

	return result
}

// SyncVulnDatabase checks unpublished vulnerabilities against vuln.go.dev and updates them.
func SyncVulnDatabase(ctx context.Context) error {
	// Get unpublished vulnerabilities
	var unpublished []UnpublishedVuln
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &unpublished,
		`SELECT goversion, cveid FROM toolchain_vuln WHERE goid IS NULL`); err != nil {
		return err
	}

	if len(unpublished) == 0 {
		log.Printf("no unpublished toolchain vulns to check")
		return nil
	}

	log.Printf("checking %d unpublished toolchain vulns against vuln.go.dev", len(unpublished))

	// Fetch the vuln index
	entries, err := FetchVulnIndex(ctx)
	if err != nil {
		return err
	}

	cveMap := BuildCVEToGoIDMap(entries)

	// Update any that are now published
	updated := 0
	for _, vuln := range unpublished {
		if info, ok := cveMap[vuln.CVEID]; ok {
			_, err := sourcespotter.DB.ExecContext(ctx,
				`UPDATE toolchain_vuln SET goid = $1, published_at = $2
				 WHERE goversion = $3 AND cveid = $4`,
				info.GoID, info.Modified, vuln.GoVersion, vuln.CVEID)
			if err != nil {
				return err
			}
			log.Printf("updated %s/%s with GO ID %s (published %s)",
				vuln.GoVersion, vuln.CVEID, info.GoID, info.Modified.Format(time.RFC3339))
			updated++
		}
	}

	log.Printf("updated %d toolchain vulns with GO IDs", updated)
	return nil
}
