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

package toolchainvuln

import (
	"context"
	"fmt"

	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/internal/prom"
	"src.agwa.name/go-dbutil"
)

// Metrics returns Prometheus gauges describing toolchain vulnerabilities
// that have been fixed in a release but not yet published to the Go
// vulnerability database.
func Metrics(ctx context.Context) ([]*prom.Family, error) {
	var rows []unpublishedVulnRow
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &rows, `SELECT goversion, cveid, released_at FROM toolchain_vuln WHERE goid IS NULL ORDER BY released_at ASC, goversion ASC, cveid`); err != nil {
		return nil, fmt.Errorf("error querying unpublished toolchain vulns: %w", err)
	}

	count := prom.NewGauge("sourcespotter_toolchainvuln_unpublished", "Number of toolchain vulnerabilities fixed in a release but not yet published to the Go vulnerability database.")
	count.Add(float64(len(rows)))

	released := prom.NewGauge("sourcespotter_toolchainvuln_unpublished_released_timestamp_seconds", "Unix time at which the release fixing the given unpublished vulnerability was announced.")
	for _, row := range rows {
		released.AddTimestamp(row.ReleasedAt, "goversion", row.GoVersion, "cveid", row.CVEID)
	}

	oldest := prom.NewGauge("sourcespotter_toolchainvuln_unpublished_oldest_released_timestamp_seconds", "Unix time of the oldest release with an unpublished vulnerability. Absent if there are none.")
	if len(rows) > 0 {
		oldest.AddTimestamp(rows[0].ReleasedAt)
	}

	return []*prom.Family{count, released, oldest}, nil
}
