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

package sumdb

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"

	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/internal/prom"
	"src.agwa.name/go-dbutil"
)

// Metrics returns Prometheus gauges describing the state of every checksum
// database.  Audit progress gauges are emitted only for enabled databases,
// matching the dashboard; failure gauges are emitted for every database,
// matching the failures feed.
func Metrics(ctx context.Context) ([]*prom.Family, error) {
	var dbs []struct {
		Address               string              `sql:"address"`
		Enabled               bool                `sql:"enabled"`
		LargestSTHSize        sql.Null[int64]     `sql:"largest_sth_size"`
		LargestSTHTime        sql.Null[time.Time] `sql:"largest_sth_time"`
		DownloadSize          sql.Null[int64]     `sql:"download_size"`
		VerifiedSize          sql.Null[int64]     `sql:"verified_size"`
		VerifiedTime          sql.Null[time.Time] `sql:"verified_time"`
		UnverifiedSTHs        int64               `sql:"unverified_sths"`
		InconsistentSTHs      int64               `sql:"inconsistent_sths"`
		LatestInconsistentSTH sql.Null[time.Time] `sql:"latest_inconsistent_sth"`
		DuplicateRecords      int64               `sql:"duplicate_records"`
		LatestDuplicateRecord sql.Null[time.Time] `sql:"latest_duplicate_record"`
	}
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &dbs, `
		SELECT
			db.address,
			db.enabled,
			(SELECT MAX(tree_size) FROM sth WHERE db_id = db.db_id) AS largest_sth_size,
			(SELECT MAX(observed_at) FROM sth WHERE db_id = db.db_id AND tree_size = (SELECT MAX(tree_size) FROM sth WHERE db_id = db.db_id)) AS largest_sth_time,
			(db.download_position->>'size')::bigint AS download_size,
			(db.verified_position->>'size')::bigint AS verified_size,
			(SELECT MIN(observed_at) FROM sth WHERE db_id = db.db_id AND tree_size = (db.verified_position->>'size')::bigint AND consistent IS NOT FALSE) AS verified_time,
			(SELECT COUNT(*) FROM sth WHERE db_id = db.db_id AND consistent IS NULL) AS unverified_sths,
			(SELECT COUNT(*) FROM sth WHERE db_id = db.db_id AND consistent = FALSE) AS inconsistent_sths,
			(SELECT MAX(observed_at) FROM sth WHERE db_id = db.db_id AND consistent = FALSE) AS latest_inconsistent_sth,
			(SELECT COUNT(*) FROM record WHERE db_id = db.db_id AND previous_position IS NOT NULL) AS duplicate_records,
			(SELECT MAX(observed_at) FROM record WHERE db_id = db.db_id AND previous_position IS NOT NULL) AS latest_duplicate_record
		FROM db
		ORDER BY db.address
	`); err != nil {
		return nil, fmt.Errorf("error querying sumdb state: %w", err)
	}

	var inconsistent []struct {
		Address  string `sql:"address"`
		TreeSize int64  `sql:"tree_size"`
		RootHash []byte `sql:"root_hash"`
	}
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &inconsistent, `
		SELECT db.address, sth.tree_size, sth.root_hash
		FROM sth JOIN db USING (db_id)
		WHERE sth.consistent = FALSE
		ORDER BY db.address, sth.tree_size, sth.root_hash
	`); err != nil {
		return nil, fmt.Errorf("error querying inconsistent STHs: %w", err)
	}

	var duplicateRecs []struct {
		Address          string `sql:"address"`
		Position         int64  `sql:"position"`
		PreviousPosition int64  `sql:"previous_position"`
		Module           string `sql:"module"`
		Version          string `sql:"version"`
	}
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &duplicateRecs, `
		SELECT db.address, record.position, record.previous_position, record.module, record.version
		FROM record JOIN db USING (db_id)
		WHERE record.previous_position IS NOT NULL
		ORDER BY db.address, record.position
	`); err != nil {
		return nil, fmt.Errorf("error querying duplicate records: %w", err)
	}

	largestSize := prom.NewGauge("sourcespotter_sumdb_largest_tree_size", "Tree size of the largest STH observed from the checksum database. Absent if no STHs have been observed.")
	largestTime := prom.NewGauge("sourcespotter_sumdb_largest_tree_observed_timestamp_seconds", "Unix time at which the largest STH was first observed. Absent if no STHs have been observed.")
	downloadSize := prom.NewGauge("sourcespotter_sumdb_downloaded_records", "Number of records downloaded from the checksum database. Absent if downloading has not started.")
	verifiedSize := prom.NewGauge("sourcespotter_sumdb_verified_tree_size", "Number of records whose hashes have been verified against an STH. Absent if verification has not started.")
	verifiedTime := prom.NewGauge("sourcespotter_sumdb_verified_tree_observed_timestamp_seconds", "Unix time at which the STH corresponding to the verified tree size was first observed. Absent if nothing has been verified.")
	unverified := prom.NewGauge("sourcespotter_sumdb_unverified_sths", "Number of observed STHs not yet checked for consistency.")
	inconsistentCount := prom.NewGauge("sourcespotter_sumdb_inconsistent_sths", "Number of observed STHs whose root hash does not match the downloaded records.")
	duplicates := prom.NewGauge("sourcespotter_sumdb_duplicate_records", "Number of records that duplicate an earlier record for the same module version.")
	failureLatest := prom.NewGauge("sourcespotter_sumdb_failure_latest_timestamp_seconds", "Unix time of the most recently observed failure of the given kind. Absent if no failure of that kind has been observed.")
	inconsistentInfo := prom.NewGauge("sourcespotter_sumdb_inconsistent_sth_info", "One series per inconsistent STH.")
	duplicateInfo := prom.NewGauge("sourcespotter_sumdb_duplicate_record_info", "One series per record that duplicates an earlier record for the same module version.")

	for _, db := range dbs {
		if db.Enabled {
			if db.LargestSTHSize.Valid {
				largestSize.Add(float64(db.LargestSTHSize.V), "sumdb", db.Address)
			}
			if db.LargestSTHTime.Valid {
				largestTime.AddTimestamp(db.LargestSTHTime.V, "sumdb", db.Address)
			}
			if db.DownloadSize.Valid {
				downloadSize.Add(float64(db.DownloadSize.V), "sumdb", db.Address)
			}
			if db.VerifiedSize.Valid {
				verifiedSize.Add(float64(db.VerifiedSize.V), "sumdb", db.Address)
			}
			if db.VerifiedTime.Valid {
				verifiedTime.AddTimestamp(db.VerifiedTime.V, "sumdb", db.Address)
			}
			unverified.Add(float64(db.UnverifiedSTHs), "sumdb", db.Address)
		}
		inconsistentCount.Add(float64(db.InconsistentSTHs), "sumdb", db.Address)
		duplicates.Add(float64(db.DuplicateRecords), "sumdb", db.Address)
		if db.LatestInconsistentSTH.Valid {
			failureLatest.AddTimestamp(db.LatestInconsistentSTH.V, "sumdb", db.Address, "kind", "inconsistent_sth")
		}
		if db.LatestDuplicateRecord.Valid {
			failureLatest.AddTimestamp(db.LatestDuplicateRecord.V, "sumdb", db.Address, "kind", "duplicate_record")
		}
	}
	for _, sth := range inconsistent {
		inconsistentInfo.Add(1,
			"sumdb", sth.Address,
			"tree_size", strconv.FormatInt(sth.TreeSize, 10),
			"root_hash", base64.StdEncoding.EncodeToString(sth.RootHash),
		)
	}

	for _, rec := range duplicateRecs {
		duplicateInfo.Add(1,
			"sumdb", rec.Address,
			"module", rec.Module,
			"version", rec.Version,
			"position", strconv.FormatInt(rec.Position, 10),
			"previous_position", strconv.FormatInt(rec.PreviousPosition, 10),
		)
	}

	return []*prom.Family{
		largestSize, largestTime, downloadSize, verifiedSize, verifiedTime, unverified,
		inconsistentCount, duplicates, failureLatest, inconsistentInfo, duplicateInfo,
	}, nil
}
