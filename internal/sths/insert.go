// Copyright (C) 2023 Opsmate, Inc.
//
// This Source Code Form is subject to the terms of the Mozilla
// Public License, v. 2.0. If a copy of the MPL was not distributed
// with this file, You can obtain one at http://mozilla.org/MPL/2.0/.
//
// This software is distributed WITHOUT A WARRANTY OF ANY KIND.
// See the Mozilla Public License for details.

package sths

import (
	"context"
	"database/sql"

	"software.sslmate.com/src/sourcespotter/sumdb"
)

func insert(ctx context.Context, db *sql.DB, sumdbid int32, sth *sumdb.STH, source string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO sth (db_id, tree_size, root_hash, signature, source) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (db_id, tree_size, root_hash) DO NOTHING`, sumdbid, sth.TreeSize, sth.RootHash[:], sth.Signature, source)
	return err
}
