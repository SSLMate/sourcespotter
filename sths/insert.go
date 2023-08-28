package sths

import (
	"context"
	"database/sql"

	"software.sslmate.com/src/sourcespotter/sumdb"
)

func insert(ctx context.Context, db *sql.DB, sumdbid int32, sth *sumdb.STH, source string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO gosum.sth (db_id, tree_size, root_hash, signature, source) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (db_id, tree_size, root_hash) DO NOTHING`, sumdbid, sth.TreeSize, sth.RootHash[:], sth.Signature, source)
	return err
}
