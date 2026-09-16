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

package telemetry

import (
	"context"
	"fmt"

	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/internal/prom"
	"src.agwa.name/go-dbutil"
)

// Metrics returns Prometheus gauges describing the Go telemetry counters
// that have been observed in the telemetry configuration.
func Metrics(ctx context.Context) ([]*prom.Family, error) {
	var rows []struct {
		Program string `sql:"program"`
		Type    string `sql:"type"`
		Count   int64  `sql:"count"`
	}
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &rows, `SELECT program, type, COUNT(DISTINCT name) AS count FROM telemetry_counter GROUP BY program, type ORDER BY program, type`); err != nil {
		return nil, fmt.Errorf("error counting telemetry counters: %w", err)
	}
	counters := prom.NewGauge("sourcespotter_telemetry_counters", "Number of distinct telemetry counters for the given program and counter type.")
	for _, row := range rows {
		counters.Add(float64(row.Count), "program", row.Program, "type", row.Type)
	}

	var numErrors int64
	if err := sourcespotter.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM telemetry_config WHERE error IS NOT NULL`).Scan(&numErrors); err != nil {
		return nil, fmt.Errorf("error counting telemetry config errors: %w", err)
	}
	errors := prom.NewGauge("sourcespotter_telemetry_config_errors", "Number of telemetry config versions that could not be processed.")
	errors.Add(float64(numErrors))

	return []*prom.Family{counters, errors}, nil
}
