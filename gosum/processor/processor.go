package processor

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"

	"software.sslmate.com/src/sourcespotter/gosum"
	"software.sslmate.com/src/sourcespotter/merkle"
	"src.agwa.name/go-dbutil"
)

const (
	checkpointInterval = 1024
)

type sumdbInfo struct {
	ID       int
	Address  string
	Position merkle.CollapsedTree
}

func loadSumdb(ctx context.Context, address string, db *sql.DB) (info sumdbInfo, err error) {
	err = db.QueryRowContext(ctx, `SELECT id, address, download_position FROM gosum_db WHERE address = $1`, address).Scan(&info.ID, &info.Address, dbutil.JSON(&info.Position))
	return
}

func loadNextSTH(ctx context.Context, sumdb *sumdbInfo, db *sql.DB) (sth gosum.STH, err error) {
	err = db.QueryRowContext(ctx, `SELECT tree_size, root_hash, signature FROM gosum_sth WHERE db_id = $1 AND tree_size > $2 ORDER BY tree_size LIMIT 1`, sumdb.ID, sumdb.Position.Size()).Scan(&sth.TreeSize, &sth.RootHash, &sth.Signature)
	return
}

func insertRecord(ctx context.Context, sumdb *sumdbInfo, record *gosum.Record, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO gosum_record (db_id, position, module, version, source_sha256, gomod_sha256, root_hash) VALUES($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (db_id, position) DO UPDATE SET module = EXCLUDED.module, version = EXCLUDED.version, source_sha256 = EXCLUDED.source_sha256, gomod_sha256 = EXCLUDED.gomod_sha256, root_hash = EXCLUDED.root_hash, observed_at = EXCLUDED.observed_at`, sumdb.ID, sumdb.Position.Size()-1, record.Module, record.Version, record.SourceSHA256, record.GomodSHA256, sumdb.Position.CalculateRoot())
	return err
}

func checkpoint(ctx context.Context, sumdb *sumdbInfo, tx *sql.Tx, db *sql.DB) (*sql.Tx, error) {
	_, err := tx.ExecContext(ctx, `UPDATE gosum_db SET download_position = $1 WHERE id = $2`, dbutil.JSON(&sumdb.Position), sumdb.ID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return db.BeginTx(ctx, nil)
}

func finishSuccessfully(ctx context.Context, sumdb *sumdbInfo, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE gosum_db SET download_position = $1, verified_position = $1 WHERE id = $2`, dbutil.JSON(&sumdb.Position), sumdb.ID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func finishWithError(ctx context.Context, sumdb *sumdbInfo, updatePosition bool, tx *sql.Tx) error {
	var err error
	if updatePosition {
		_, err = tx.ExecContext(ctx, `UPDATE gosum_db SET download_position = $1 WHERE id = $2`, dbutil.JSON(&sumdb.Position), sumdb.ID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE gosum_db SET download_position = verified_position WHERE id = $1`, sumdb.ID)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func Process(ctx context.Context, address string, db *sql.DB) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sumdb, err := loadSumdb(ctx, address, db)
	if err != nil {
		return err
	}

	for {
		nextSTH, err := loadNextSTH(ctx, &sumdb, db)
		if err != nil {
			if err == sql.ErrNoRows {
				return nil
			}
			return err
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}

		var downloadErr error
		downloadBegin := sumdb.Position.Size()
		downloadEnd := nextSTH.TreeSize
		records := make(chan *gosum.Record)
		go func() {
			downloadErr = gosum.DownloadRecords(ctx, sumdb.Address, downloadBegin, downloadEnd, records)
			close(records)
		}()

		for record := range records {
			sumdb.Position.Add(record.Hash())
			if err := insertRecord(ctx, &sumdb, record, tx); err != nil {
				return err
			}
			if sumdb.Position.Size()%checkpointInterval == 0 {
				tx, err = checkpoint(ctx, &sumdb, tx, db)
				if err != nil {
					return err
				}
			}
		}
		if downloadErr != nil {
			if err := finishWithError(ctx, &sumdb, true, tx); err != nil {
				return err
			}
			return err
		}

		calculatedRootHash := sumdb.Position.CalculateRoot()
		if bytes.Equal(calculatedRootHash, nextSTH.RootHash) {
			if err := finishSuccessfully(ctx, &sumdb, tx); err != nil {
				return err
			}
		} else {
			if err := finishWithError(ctx, &sumdb, false, tx); err != nil {
				return err
			}
			return fmt.Errorf("Expected root hash %x at %d, but calculated %x instead", nextSTH.RootHash, sumdb.Position.Size(), calculatedRootHash)
		}
	}
}
