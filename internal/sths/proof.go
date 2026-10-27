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
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb/tlog"
	"software.sslmate.com/src/certspotter/merkletree"
	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/sumdb"
	"src.agwa.name/go-dbutil"
)

const toolchainModule = "golang.org/toolchain"

// tileReader is a tlog.TileReader that downloads hash tiles from a sumdb.
// Tiles that tlog.TileHashReader has verified are remembered for the
// lifetime of the reader, which is one pass of FillToolchainProofs, so that proofs
// for records in the same part of the tree don't re-download the same tiles.
type tileReader struct {
	ctx      context.Context
	fetch    func(ctx context.Context, tile tlog.Tile) ([]byte, error)
	verified map[tlog.Tile][]byte
}

func newTileReader(ctx context.Context, address string) *tileReader {
	return &tileReader{
		ctx: ctx,
		fetch: func(ctx context.Context, tile tlog.Tile) ([]byte, error) {
			return sumdb.DownloadTile(ctx, address, tile)
		},
		verified: make(map[tlog.Tile][]byte),
	}
}

func (r *tileReader) Height() int { return sumdb.TileSize }

func (r *tileReader) ReadTiles(tiles []tlog.Tile) ([][]byte, error) {
	data := make([][]byte, len(tiles))
	for i, tile := range tiles {
		var err error
		if data[i], err = r.readTile(tile); err != nil {
			return nil, err
		}
	}
	return data, nil
}

func (r *tileReader) readTile(tile tlog.Tile) ([]byte, error) {
	if data, ok := r.verified[tile]; ok {
		return data, nil
	}
	full := tile
	full.W = 1 << uint(tile.H)
	if tile != full {
		if data, ok := r.verified[full]; ok {
			return data[:tile.W*tlog.HashSize], nil
		}
	}
	data, err := r.fetch(r.ctx, tile)
	if err == nil {
		return data, nil
	}
	if tile != full {
		// The sumdb stops serving a partial tile once the tile is
		// complete, in which case the full tile is a superset.
		if data, fullErr := r.fetch(r.ctx, full); fullErr == nil {
			return data[:tile.W*tlog.HashSize], nil
		}
	}
	return nil, err
}

func (r *tileReader) SaveTiles(tiles []tlog.Tile, data [][]byte) {
	for i, tile := range tiles {
		r.verified[tile] = data[i]
	}
}

func encodeProof(p tlog.RecordProof) []byte {
	encoded := make([]byte, 0, len(p)*tlog.HashSize)
	for _, h := range p {
		encoded = append(encoded, h[:]...)
	}
	return encoded
}

func decodeProof(encoded []byte) (tlog.RecordProof, error) {
	if len(encoded)%tlog.HashSize != 0 {
		return nil, errors.New("proof length is not a multiple of the hash size")
	}
	p := make(tlog.RecordProof, len(encoded)/tlog.HashSize)
	for i := range p {
		p[i] = tlog.Hash(encoded[i*tlog.HashSize : (i+1)*tlog.HashSize])
	}
	return p, nil
}

// FillToolchainProofs computes, for every verified toolchain whose sumdb record is
// included in the latest cosigned STH but which doesn't yet have an inclusion
// proof, a proof of the record's inclusion in that STH, and stores it in the
// toolchain_build table.  Tile download failures are logged and end the pass
// early; the affected toolchains are retried on the next pass.
func FillToolchainProofs(ctx context.Context, sumdbid int32) error {
	var (
		sthID    int64
		treeSize int64
		rootHash []byte
		address  string
	)
	if err := sourcespotter.DB.QueryRowContext(ctx, `
		SELECT cosigned_sth.sth_id, sth.tree_size, sth.root_hash, db.address
		FROM cosigned_sth
		JOIN sth ON sth.sth_id = cosigned_sth.sth_id
		JOIN db ON db.db_id = cosigned_sth.db_id
		WHERE cosigned_sth.db_id = $1
		ORDER BY cosigned_sth.tree_size DESC
		LIMIT 1
	`, sumdbid).Scan(&sthID, &treeSize, &rootHash, &address); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("error loading latest cosigned STH for sumdb %d: %w", sumdbid, err)
	}

	var pending []struct {
		Version  string `sql:"version"`
		Position int64  `sql:"position"`
	}
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &pending, `
		SELECT toolchain_build.version, MIN(record.position) AS position
		FROM toolchain_build
		JOIN record ON record.db_id = $1 AND record.module = $2 AND record.version = toolchain_build.version
		WHERE toolchain_build.inclusion_proof IS NULL
		GROUP BY toolchain_build.version
		HAVING MIN(record.position) < $3
		ORDER BY MIN(record.position)
	`, sumdbid, toolchainModule, treeSize); err != nil {
		return fmt.Errorf("error querying toolchains needing proofs for sumdb %d: %w", sumdbid, err)
	}
	if len(pending) == 0 {
		return nil
	}

	log.Printf("%s: computing inclusion proofs for %d toolchains in cosigned STH with tree size %d", address, len(pending), treeSize)
	tree := tlog.Tree{N: treeSize, Hash: tlog.Hash(rootHash)}
	hashReader := tlog.TileHashReader(tree, newTileReader(ctx, address))
	for i, p := range pending {
		recordProof, err := tlog.ProveRecord(treeSize, p.Position, hashReader)
		if err != nil {
			log.Printf("%s: error proving inclusion of record %d (%s) in tree of size %d (will retry later): %s", address, p.Position, p.Version, treeSize, err)
			return ctx.Err()
		}
		if _, err := sourcespotter.DB.ExecContext(ctx, `UPDATE toolchain_build SET inclusion_proof = $1, cosigned_sth_id = $2 WHERE version = $3 AND inclusion_proof IS NULL`, encodeProof(recordProof), sthID, p.Version); err != nil {
			return fmt.Errorf("error storing inclusion proof for %s: %w", p.Version, err)
		}
		if (i+1)%500 == 0 {
			log.Printf("%s: computed %d/%d inclusion proofs", address, i+1, len(pending))
		}
	}
	log.Printf("%s: finished computing %d inclusion proofs", address, len(pending))
	return nil
}

// ServeAttestation serves, for the toolchain module version named by the "path"
// path value (as MODULE@VERSION, escaped as in the sumdb's lookup endpoint),
// an attestation: the sumdb record followed by a tlog-proof of its inclusion
// in a cosigned STH.
func ServeAttestation(w http.ResponseWriter, req *http.Request) {
	escapedModule, escapedVersion, ok := strings.Cut(req.PathValue("path"), "@")
	if !ok {
		http.Error(w, "Expected MODULE@VERSION", 400)
		return
	}
	modulePath, err := module.UnescapePath(escapedModule)
	if err != nil {
		http.Error(w, "Invalid module path: "+err.Error(), 400)
		return
	}
	version, err := module.UnescapeVersion(escapedVersion)
	if err != nil {
		http.Error(w, "Invalid module version: "+err.Error(), 400)
		return
	}
	if modulePath != toolchainModule {
		http.Error(w, "Attestations are only available for "+toolchainModule, 400)
		return
	}

	var (
		address      string
		record       = sumdb.Record{Module: modulePath, Version: version}
		position     int64
		encodedProof []byte
		sth          sumdb.STH
		rootHash     []byte
		cosignature  []byte
	)
	if err := sourcespotter.DB.QueryRowContext(req.Context(), `
		SELECT db.address, record.position, record.source_sha256, record.gomod_sha256, toolchain_build.inclusion_proof, sth.tree_size, sth.root_hash, sth.signature, cosigned_sth.cosignature
		FROM toolchain_build
		JOIN cosigned_sth ON cosigned_sth.sth_id = toolchain_build.cosigned_sth_id
		JOIN sth ON sth.sth_id = cosigned_sth.sth_id
		JOIN db ON db.db_id = cosigned_sth.db_id
		JOIN record ON record.db_id = db.db_id AND record.module = $1 AND record.version = toolchain_build.version
		WHERE toolchain_build.version = $2 AND toolchain_build.inclusion_proof IS NOT NULL
		ORDER BY record.position
		LIMIT 1
	`, modulePath, version).Scan(&address, &position, &record.SourceSHA256, &record.GomodSHA256, &encodedProof, &sth.TreeSize, &rootHash, &sth.Signature, &cosignature); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			var exists bool
			if err := sourcespotter.DB.QueryRowContext(req.Context(), `SELECT EXISTS(SELECT 1 FROM record WHERE module = $1 AND version = $2)`, modulePath, version).Scan(&exists); err != nil {
				log.Printf("ServeAttestation: error querying record for %s@%s: %s", modulePath, version, err)
				http.Error(w, "Internal Database Error", 500)
			} else if exists {
				http.Error(w, "This toolchain has not yet been verified as reproducible. Please check back later.", 503)
			} else {
				http.Error(w, "Toolchain Version Not Found", 404)
			}
		} else {
			log.Printf("ServeAttestation: error loading proof for %s@%s: %s", modulePath, version, err)
			http.Error(w, "Internal Database Error", 500)
		}
		return
	}
	sth.RootHash = (merkletree.Hash)(rootHash)
	recordProof, err := decodeProof(encodedProof)
	if err != nil {
		log.Printf("ServeAttestation: malformed stored proof for %s@%s: %s", modulePath, version, err)
		http.Error(w, "Internal Database Error", 500)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=86400, must-revalidate")
	w.WriteHeader(200)
	writeAttestation(w, record.Format(), position, recordProof, FormatCosignedSTH(&sth, address, cosignature))
}

// writeAttestation writes an attestation: a record followed by an empty line
// and a c2sp.org/tlog-proof proving the record's inclusion at the given index
// in the given checkpoint.
func writeAttestation(w io.Writer, record []byte, index int64, recordProof tlog.RecordProof, checkpoint string) {
	w.Write(record)
	fmt.Fprintf(w, "\nc2sp.org/tlog-proof@v1\nindex %d\n", index)
	for _, h := range recordProof {
		io.WriteString(w, base64.StdEncoding.EncodeToString(h[:])+"\n")
	}
	io.WriteString(w, "\n"+checkpoint)
}
