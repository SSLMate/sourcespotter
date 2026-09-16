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

package toolchain

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/internal/prom"
	"src.agwa.name/go-dbutil"
)

// buildStatuses lists every value of the toolchain_build_status enum.
var buildStatuses = []string{"skipped", "equal", "unequal", "failed"}

// Metrics returns Prometheus gauges describing toolchain reproduction results.
func Metrics(ctx context.Context) ([]*prom.Family, error) {
	var counts []struct {
		Status string `sql:"status"`
		Count  int64  `sql:"count"`
	}
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &counts, `SELECT status, COUNT(*) AS count FROM toolchain_build GROUP BY status`); err != nil {
		return nil, fmt.Errorf("error counting toolchain builds: %w", err)
	}

	var failures []struct {
		Version    string    `sql:"version"`
		Status     string    `sql:"status"`
		InsertedAt time.Time `sql:"inserted_at"`
	}
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &failures, `SELECT version, status, inserted_at FROM toolchain_build WHERE status NOT IN ('equal','skipped') ORDER BY inserted_at, version`); err != nil {
		return nil, fmt.Errorf("error querying toolchain failures: %w", err)
	}

	var latest sql.Null[time.Time]
	if err := sourcespotter.DB.QueryRowContext(ctx, `SELECT MAX(inserted_at) FROM toolchain_build`).Scan(&latest); err != nil {
		return nil, fmt.Errorf("error querying latest toolchain build: %w", err)
	}

	builds := prom.NewGauge("sourcespotter_toolchain_builds", "Number of toolchain builds with the given status.")
	countByStatus := make(map[string]int64)
	for _, c := range counts {
		countByStatus[c.Status] = c.Count
	}
	for _, status := range buildStatuses {
		builds.Add(float64(countByStatus[status]), "status", status)
	}

	failureTime := prom.NewGauge("sourcespotter_toolchain_build_failure_timestamp_seconds", "Unix time at which the given toolchain build was found to be unequal or failed to build.")
	for _, f := range failures {
		failureTime.AddTimestamp(f.InsertedAt, "version", f.Version, "status", f.Status)
	}

	latestTime := prom.NewGauge("sourcespotter_toolchain_build_latest_timestamp_seconds", "Unix time of the most recent toolchain build. Absent if no builds have been attempted.")
	if latest.Valid {
		latestTime.AddTimestamp(latest.V)
	}

	return []*prom.Family{builds, failureTime, latestTime}, nil
}
