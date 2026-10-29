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

package sths

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"software.sslmate.com/src/certspotter/merkletree"
	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/cosign"
	"software.sslmate.com/src/sourcespotter/sumdb"
)

// CosignerVerifierKey is the verifier key corresponding to the key that this
// process uses to cosign STHs, for display on the dashboard.  Empty if this
// process is not configured to cosign STHs.
var CosignerVerifierKey string

// CosignerName returns the name of the key that Source Spotter uses to cosign
// STHs: the configured domain followed by "/toolchain".  A cosignature from
// this key attests that the STH is consistent with every STH that Source
// Spotter has previously cosigned, and that every golang.org/toolchain
// record in the tree has been verified to be reproducible (or skipped as not
// reproducible).
func CosignerName() string {
	return sourcespotter.Domain + "/toolchain"
}

// Cosign cosigns the largest eligible STH for the given sumdb, if it is larger
// than the largest STH that has already been cosigned.  An STH is eligible if
// its root hash matches the records downloaded from the sumdb and every
// golang.org/toolchain record in the tree has a toolchain_build row with
// status 'equal' or 'skipped'.
func Cosign(ctx context.Context, sumdbid int32, signer *cosign.Signer) error {
	if signer.Name() != CosignerName() {
		return fmt.Errorf("cosigner key name is %q but should be %q", signer.Name(), CosignerName())
	}

	tx, err := sourcespotter.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("error starting database transaction: %w", err)
	}
	defer tx.Rollback()

	var (
		address      string
		verifiedSize int64
	)
	if err := tx.QueryRowContext(ctx, `SELECT address, coalesce((verified_position->>'size')::bigint, 0) FROM db WHERE db_id = $1`, sumdbid).Scan(&address, &verifiedSize); err != nil {
		return fmt.Errorf("error loading info for sumdb %d: %w", sumdbid, err)
	}

	// The largest tree size for which every toolchain has been verified is
	// the position of the first toolchain record that hasn't been.
	var unverifiedToolchain sql.Null[int64]
	if err := tx.QueryRowContext(ctx, `
		SELECT MIN(record.position)
		FROM record
		LEFT JOIN toolchain_build USING (version)
		WHERE record.db_id = $1 AND record.module = 'golang.org/toolchain' AND (toolchain_build.status IS NULL OR toolchain_build.status NOT IN ('equal', 'skipped'))
	`, sumdbid).Scan(&unverifiedToolchain); err != nil {
		return fmt.Errorf("error querying unverified toolchains for sumdb %d: %w", sumdbid, err)
	}
	eligibleSize := verifiedSize
	if unverifiedToolchain.Valid && unverifiedToolchain.V < eligibleSize {
		eligibleSize = unverifiedToolchain.V
	}

	var cosignedSize int64
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(MAX(tree_size), 0) FROM cosigned_sth WHERE db_id = $1`, sumdbid).Scan(&cosignedSize); err != nil {
		return fmt.Errorf("error querying cosigned STHs for sumdb %d: %w", sumdbid, err)
	}
	if eligibleSize <= cosignedSize {
		return nil
	}

	var (
		sthID    int64
		sth      sumdb.STH
		rootHash []byte
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT sth_id, tree_size, root_hash, signature
		FROM sth
		WHERE db_id = $1 AND consistent AND tree_size > $2 AND tree_size <= $3
		ORDER BY tree_size DESC
		LIMIT 1
	`, sumdbid, cosignedSize, eligibleSize).Scan(&sthID, &sth.TreeSize, &rootHash, &sth.Signature); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("error querying eligible STH for sumdb %d: %w", sumdbid, err)
	}
	sth.RootHash = (merkletree.Hash)(rootHash)

	now := time.Now().Truncate(time.Second)
	cosignature := signer.Cosign(sth.Checkpoint(), now)
	if _, err := tx.ExecContext(ctx, `INSERT INTO cosigned_sth (db_id, tree_size, sth_id, cosigned_at, cosignature) VALUES ($1, $2, $3, $4, $5)`, sumdbid, sth.TreeSize, sthID, now, cosignature); err != nil {
		return fmt.Errorf("error inserting cosigned STH for sumdb %d: %w", sumdbid, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("error committing transaction: %w", err)
	}
	log.Printf("%s: cosigned STH with tree size %d", address, sth.TreeSize)
	return nil
}

// FormatCosignedSTH formats an STH as a checkpoint signed by the sumdb and
// cosigned by Source Spotter.
func FormatCosignedSTH(sth *sumdb.STH, address string, cosignature []byte) string {
	return sth.Format(address) + cosign.FormatSignatureLine(CosignerName(), cosignature)
}

// serveCosignedCheckpoint serves the latest cosigned STH for the sumdb with the
// given address, in checkpoint format with both the sumdb's signature and
// Source Spotter's cosignature.
func serveCosignedCheckpoint(w http.ResponseWriter, req *http.Request, address string) {
	var (
		sth         sumdb.STH
		rootHash    []byte
		cosignature []byte
	)
	if err := sourcespotter.DB.QueryRowContext(req.Context(), `
		SELECT sth.tree_size, sth.root_hash, sth.signature, cosigned_sth.cosignature
		FROM db
		JOIN cosigned_sth ON cosigned_sth.db_id = db.db_id
		JOIN sth ON sth.sth_id = cosigned_sth.sth_id
		WHERE db.address = $1
		ORDER BY cosigned_sth.tree_size DESC
		LIMIT 1
	`, address).Scan(&sth.TreeSize, &rootHash, &sth.Signature, &cosignature); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "We have not yet cosigned any STHs for this sumdb. Please check back later.", 404)
		} else {
			log.Printf("serveCosignedCheckpoint: error loading cosigned STH for sumdb %q: %s", address, err)
			http.Error(w, "Internal Database Error", 500)
		}
		return
	}
	sth.RootHash = (merkletree.Hash)(rootHash)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=60, must-revalidate")
	w.WriteHeader(200)
	fmt.Fprint(w, FormatCosignedSTH(&sth, address, cosignature))
}
