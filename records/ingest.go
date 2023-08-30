package records

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log"

	"github.com/lib/pq"
	"software.sslmate.com/src/certspotter/merkletree"
	"software.sslmate.com/src/sourcespotter/sumdb"
	"src.agwa.name/go-dbutil"
)

const (
	checkpointInterval = 10000
)

type nextSTH struct {
	TreeSize uint64 `sql:"tree_size"`
	RootHash []byte `sql:"root_hash"`
}

type ingestState struct {
	id             int32
	db             *sql.DB
	address        string
	tree           merkletree.CollapsedTree
	sths           []nextSTH
	tx             *sql.Tx
	copyStmt       *sql.Stmt
	pendingRecords int
}

func loadIngestState(ctx context.Context, id int32, db *sql.DB) (*ingestState, error) {
	state := &ingestState{id: id, db: db}
	if err := db.QueryRowContext(ctx, `SELECT address, download_position FROM gosum.db WHERE db_id = $1`, id).Scan(&state.address, dbutil.JSON(&state.tree)); err != nil {
		return nil, fmt.Errorf("error loading sumdb %d: %w", id, err)
	}
	if err := dbutil.QueryAll(ctx, db, &state.sths, `SELECT DISTINCT ON (tree_size) tree_size, root_hash FROM gosum.sth WHERE db_id = $1 AND tree_size > $2 ORDER BY tree_size, root_hash`, state.id, state.tree.Size()); err != nil {
		return nil, fmt.Errorf("error loading next STHs for sumdb %d: %w", id, err)
	}
	if err := state.begin(ctx); err != nil {
		return nil, err
	}
	return state, nil
}

func (state *ingestState) begin(ctx context.Context) error {
	if state.tx != nil || state.copyStmt != nil {
		panic("ingestState.begin: begin already called")
	}

	tx, err := state.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("error starting database transaction: %w", err)
	}
	defer func() {
		if tx != nil {
			tx.Rollback()
		}
	}()

	var (
		address string
		tree    merkletree.CollapsedTree
	)
	if err := tx.QueryRowContext(ctx, `SELECT address, download_position FROM gosum.db WHERE db_id = $1 FOR UPDATE`, state.id).Scan(&address, dbutil.JSON(&tree)); err != nil {
		return fmt.Errorf("error reloading sumdb %d: %w", state.id, err)
	}
	if address != state.address || !tree.Equal(state.tree) {
		return fmt.Errorf("sumdb %d has been modified by a different process", state.id)
	}

	stmt, err := tx.PrepareContext(ctx, pq.CopyInSchema("gosum", "record", "db_id", "position", "module", "version", "source_sha256", "gomod_sha256", "root_hash"))
	if err != nil {
		return fmt.Errorf("error preparing COPY statement: %w", err)
	}

	state.tx = tx
	state.copyStmt = stmt
	tx = nil

	return nil
}

func (state *ingestState) commit(ctx context.Context, verified bool) error {
	if state.copyStmt == nil || state.tx == nil {
		panic("ingestState.commit: commit already called")
	}

	if _, err := state.copyStmt.ExecContext(ctx); err != nil {
		return fmt.Errorf("error finishing COPY statement: %w", err)
	}
	if err := state.copyStmt.Close(); err != nil {
		return fmt.Errorf("error closing COPY statement: %w", err)
	}
	if verified {
		if err := dbutil.MustAffectRow(state.tx.ExecContext(ctx, `UPDATE gosum.db SET download_position = $1, verified_position = $1 WHERE db_id = $2`, dbutil.JSON(state.tree), state.id)); err != nil {
			return fmt.Errorf("error updating download and verified position: %w", err)
		}
	} else {
		if err := dbutil.MustAffectRow(state.tx.ExecContext(ctx, `UPDATE gosum.db SET download_position = $1 WHERE db_id = $2`, dbutil.JSON(state.tree), state.id)); err != nil {
			return fmt.Errorf("error updating download position: %w", err)
		}
	}
	if err := state.tx.Commit(); err != nil {
		return fmt.Errorf("error committing transaction: %w", err)
	}

	state.pendingRecords = 0
	state.copyStmt = nil
	state.tx = nil
	return nil
}

func (state *ingestState) rollback() {
	if state.tx != nil {
		state.tx.Rollback()
		state.tx = nil
	}
}

func (state *ingestState) checkpoint(ctx context.Context, verified bool) error {
	if err := state.commit(ctx, verified); err != nil {
		return err
	}
	if err := state.begin(ctx); err != nil {
		return err
	}
	return nil
}

func (state *ingestState) addRecord(ctx context.Context, record *sumdb.Record) error {
	leafHash := record.Hash()
	position := state.tree.Size()
	state.tree.Add(leafHash)
	rootHash := state.tree.CalculateRoot()

	if _, err := state.copyStmt.ExecContext(ctx, state.id, position, record.Module, record.Version, record.SourceSHA256, record.GomodSHA256, rootHash[:]); err != nil {
		return fmt.Errorf("error COPYing record: %w", err)
	}
	state.pendingRecords++

	if len(state.sths) > 0 && state.tree.Size() == state.sths[0].TreeSize {
		if bytes.Equal(state.sths[0].RootHash, rootHash[:]) {
			if err := state.checkpoint(ctx, true); err != nil {
				return err
			}
		} else {
			log.Printf("%s: root hash calculated from first %d entries (%x) does not match STH root hash (%x)", state.address, state.tree.Size(), rootHash, state.sths[0].RootHash)
		}
		state.sths = state.sths[1:]
	}

	if state.pendingRecords >= checkpointInterval {
		if err := state.checkpoint(ctx, false); err != nil {
			return err
		}
	}
	return nil
}

func Ingest(ctx context.Context, id int32, db *sql.DB) (bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	state, err := loadIngestState(ctx, id, db)
	if err != nil {
		return false, err
	}
	defer state.rollback()

	if len(state.sths) == 0 {
		return false, nil
	}

	var (
		downloadBegin = state.tree.Size()
		downloadEnd   = state.sths[len(state.sths)-1].TreeSize
	)
	records := make(chan *sumdb.Record, sumdb.RecordsPerTile*2)
	var downloadErr error
	go func() {
		defer close(records)
		downloadErr = sumdb.DownloadRecords(ctx, state.address, downloadBegin, downloadEnd, records)
	}()
	for record := range records {
		if err := state.addRecord(ctx, record); err != nil {
			return false, err
		}
	}
	if downloadErr != nil {
		return false, downloadErr
	}
	if err := state.commit(ctx, false); err != nil {
		return false, err
	}
	return true, nil
}
