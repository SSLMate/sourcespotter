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
