package sths

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"software.sslmate.com/src/sourcespotter/sumdb"
)

func Download(ctx context.Context, sumdbid int32, db *sql.DB) error {
	var address string
	var key []byte
	if err := db.QueryRowContext(ctx, `SELECT address, key FROM gosum.db WHERE db_id = $1`, sumdbid).Scan(&address, &key); err != nil {
		return fmt.Errorf("error loading info for sumdb %d: %w", sumdbid, err)
	}

	sth, err := sumdb.DownloadAndAuthenticateSTH(ctx, address, key)
	if err != nil {
		log.Printf("%s: %s", address, err)
		return nil
	}

	if err := insert(ctx, db, sumdbid, sth, "https://"+address+"/latest"); err != nil {
		return fmt.Errorf("error inserting downloaded STH for sumdb %d: %w", sumdbid, err)
	}

	return nil
}
