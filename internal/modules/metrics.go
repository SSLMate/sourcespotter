// Copyright (C) 2026 Opsmate, Inc.
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

package modules

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/internal/prom"
)

const maxMetricsModules = 25
const maxMetricsRecords = 100_000

var errTooManyRecords = errors.New("too many records")

type metrics struct {
	numVersions      *prom.Family
	latest           *prom.Family
	numUnauthorized  *prom.Family
	unauthorizedInfo *prom.Family
}

func newMetrics() *metrics {
	return &metrics{
		numVersions:      prom.NewGauge("sourcespotter_module_versions", "Number of distinct non-prerelease versions of the module observed in checksum databases."),
		latest:           prom.NewGauge("sourcespotter_module_latest_timestamp_seconds", "Unix time at which the most recent non-prerelease version of the module was observed. Absent if no versions have been observed."),
		numUnauthorized:  prom.NewGauge("sourcespotter_module_unauthorized_versions", "Number of distinct non-prerelease versions of the module not authorized by the given key."),
		unauthorizedInfo: prom.NewGauge("sourcespotter_module_unauthorized_version_info", "One series per version of the module not authorized by the given key."),
	}
}

func checkModule(ctx context.Context, metrics *metrics, module string, pubkeyOrNil []byte) error {
	// A module version can appear in more than one record (e.g. a duplicate
	// record in the sumdb, or the same version in multiple sumdbs), so records
	// are grouped by version, and a version is unauthorized if any of its
	// records is.
	query := `SELECT module, version, MAX(observed_at) AS observed_at, `
	args := []any{}
	if pubkeyOrNil != nil {
		query += `bool_and(EXISTS (SELECT 1 FROM authorized_record ar WHERE ar.pubkey = $2 AND ar.module = r.module AND ar.version = r.version AND (ar.source_sha256,ar.gomod_sha256) IS NOT DISTINCT FROM (r.source_sha256,r.gomod_sha256)))`
	} else {
		query += `TRUE`
	}
	query += ` AS authorized FROM record r`
	if strings.HasSuffix(module, "/") {
		query += ` WHERE module LIKE $1`
		args = append(args, module+"%")
	} else {
		query += ` WHERE module = $1`
		args = append(args, module)
	}
	if pubkeyOrNil != nil {
		args = append(args, pubkeyOrNil)
	}
	query += ` GROUP BY module, version ORDER BY module, version`
	query += ` LIMIT ` + strconv.FormatInt(maxMetricsRecords+1, 10)

	rows, err := sourcespotter.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("error loading record: %w", err)
	}
	defer rows.Close()

	var (
		versions     int
		unauthorized int
		latestTime   time.Time
	)
	for numRows := 0; rows.Next(); numRows++ {
		if numRows == maxMetricsRecords {
			return errTooManyRecords
		}
		var (
			recordModule string
			version      string
			observedAt   time.Time
			authorized   bool
		)
		if err := rows.Scan(&recordModule, &version, &observedAt, &authorized); err != nil {
			return fmt.Errorf("error scanning record: %w", err)
		}
		if semver.Prerelease(version) != "" {
			continue
		}
		versions++
		if observedAt.After(latestTime) {
			latestTime = observedAt
		}
		if !authorized {
			unauthorized++
			metrics.unauthorizedInfo.Add(1, "module", recordModule, "version", version)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error reading record: %w", err)
	}

	metrics.numVersions.Add(float64(versions), "module", module)
	if !latestTime.IsZero() {
		metrics.latest.AddTimestamp(latestTime, "module", module)
	}
	metrics.numUnauthorized.Add(float64(unauthorized), "module", module)
	return nil
}

// ServeMetrics exports Prometheus gauges about the versions of a module,
// taking the same module, mldsa, and ed25519 parameters as the versions
// Atom feed.  Unauthorized version gauges are exported only when a key is
// supplied.
func ServeMetrics(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	modules := req.URL.Query()["module"]
	if len(modules) == 0 {
		http.Error(w, "Missing module parameter", http.StatusBadRequest)
		return
	}
	if len(modules) > maxMetricsModules {
		http.Error(w, fmt.Sprintf("module parameter cannot be specified more than %d times", maxMetricsModules), http.StatusBadRequest)
		return
	}
	pubkey, err := parsePubkeyParam(req.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	metrics := newMetrics()
	for _, module := range modules {
		if module == "" {
			http.Error(w, "module parameter cannot be empty", http.StatusBadRequest)
			return
		}
		if err := checkModule(ctx, metrics, module, pubkey); err == errTooManyRecords {
			http.Error(w, fmt.Sprintf("Sorry, there are more than %d versions matching %s and we can't export metrics for that many", maxMetricsRecords, module), http.StatusInternalServerError)
			return
		} else if err != nil {
			log.Printf("modules.ServeMetrics: %s", err)
			http.Error(w, "Internal Database Error", http.StatusInternalServerError)
			return
		}
	}
	families := []*prom.Family{metrics.numVersions, metrics.latest}
	if pubkey != nil {
		families = append(families, metrics.numUnauthorized, metrics.unauthorizedInfo)
	}
	prom.Serve(w, families)
}
