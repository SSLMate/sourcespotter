// Copyright (C) 2023 Opsmate, Inc.
//
// This Source Code Form is subject to the terms of the Mozilla
// Public License, v. 2.0. If a copy of the MPL was not distributed
// with this file, You can obtain one at http://mozilla.org/MPL/2.0/.
//
// This software is distributed WITHOUT A WARRANTY OF ANY KIND.
// See the Mozilla Public License for details.

// Package sths contains code to download, audit, and gossip STHs (checkpoints)
package sths

import (
	"context"
	"database/sql"
	"fmt"
)

const auditStmt = `
	UPDATE sth
	SET consistent = (sth.root_hash = record.root_hash)
	FROM record
	WHERE
		record.db_id = sth.db_id AND
		record.position = sth.tree_size - 1 AND
		sth.consistent IS NULL AND
		sth.db_id = $1 AND
		sth.tree_size > 0 AND
		sth.tree_size <= $2
`

func Audit(ctx context.Context, sumdbid int32, db *sql.DB) error {
	var verifiedSize uint64
	if err := db.QueryRowContext(ctx, `SELECT coalesce((verified_position->>'size')::bigint, 0) FROM db WHERE db_id = $1`, sumdbid).Scan(&verifiedSize); err != nil {
		return fmt.Errorf("error loading verified position of sumdb %d: %w", sumdbid, err)
	}
	if _, err := db.ExecContext(ctx, auditStmt, sumdbid, verifiedSize); err != nil {
		return fmt.Errorf("error auditing STHs for sumdb %d: %w", sumdbid, err)
	}
	return nil
}
