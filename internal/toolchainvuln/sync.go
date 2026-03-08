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
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
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

// VulnRecord represents the full vulnerability record from vuln.go.dev.
type VulnRecord struct {
	ID        string           `json:"id"`
	Published string           `json:"published"`
	Affected  []AffectedEntry  `json:"affected"`
}

// AffectedEntry represents an affected package in a vulnerability record.
type AffectedEntry struct {
	Package AffectedPackage `json:"package"`
}

// AffectedPackage represents a package reference in an affected entry.
type AffectedPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

// AffectsToolchain returns true if the vulnerability affects stdlib or toolchain.
func (r *VulnRecord) AffectsToolchain() bool {
	for _, a := range r.Affected {
		if a.Package.Name == "stdlib" || a.Package.Name == "toolchain" {
			return true
		}
	}
	return false
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

// FetchVulnRecord downloads and parses a specific vulnerability record.
func FetchVulnRecord(ctx context.Context, goID string) (*VulnRecord, error) {
	url := fmt.Sprintf("https://vuln.go.dev/ID/%s.json", goID)
	data, err := httpclient.DownloadBytes(ctx, url)
	if err != nil {
		return nil, err
	}

	var record VulnRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}

	return &record, nil
}

// BuildCVEToGoIDMap creates a map from CVE ID to GO ID.
func BuildCVEToGoIDMap(entries []VulnEntry) map[string]string {
	result := make(map[string]string)

	for _, entry := range entries {
		for _, alias := range entry.Aliases {
			if strings.HasPrefix(alias, "CVE-") {
				result[alias] = entry.ID
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

	// Find which GO IDs we need to fetch
	goIDsToFetch := make(map[string]bool)
	for _, vuln := range unpublished {
		if goID, ok := cveMap[vuln.CVEID]; ok {
			goIDsToFetch[goID] = true
		}
	}

	if len(goIDsToFetch) == 0 {
		log.Printf("no matching GO IDs found in vuln.go.dev")
		return nil
	}

	// Fetch published times for each GO ID (up to 10 in parallel)
	publishedTimes := make(map[string]time.Time)
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(10)

	for goID := range goIDsToFetch {
		goID := goID // capture for goroutine
		g.Go(func() error {
			record, err := FetchVulnRecord(gctx, goID)
			if err != nil {
				log.Printf("error fetching %s: %v", goID, err)
				return nil // don't fail the whole sync
			}

			// Only consider vulnerabilities that actually affect stdlib or toolchain
			if !record.AffectsToolchain() {
				log.Printf("%s does not affect stdlib or toolchain, skipping", goID)
				return nil
			}

			published, err := time.Parse(time.RFC3339, record.Published)
			if err != nil {
				log.Printf("error parsing published time for %s: %v", goID, err)
				return nil
			}

			mu.Lock()
			publishedTimes[goID] = published
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	// Update any that are now published
	updated := 0
	for _, vuln := range unpublished {
		goID, ok := cveMap[vuln.CVEID]
		if !ok {
			continue
		}

		published, ok := publishedTimes[goID]
		if !ok {
			continue
		}

		_, err := sourcespotter.DB.ExecContext(ctx,
			`UPDATE toolchain_vuln SET goid = $1, published_at = $2
			 WHERE goversion = $3 AND cveid = $4`,
			goID, published, vuln.GoVersion, vuln.CVEID)
		if err != nil {
			return err
		}
		log.Printf("updated %s/%s with GO ID %s (published %s)",
			vuln.GoVersion, vuln.CVEID, goID, published.Format(time.RFC3339))
		updated++
	}

	log.Printf("updated %d toolchain vulns with GO IDs", updated)
	return nil
}
