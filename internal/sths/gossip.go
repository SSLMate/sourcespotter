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
	"fmt"
	"io"
	"log"
	"net/http"

	"software.sslmate.com/src/certspotter/merkletree"
	"software.sslmate.com/src/sourcespotter/sumdb"
)

func ServeGossip(address string, w http.ResponseWriter, req *http.Request, db *sql.DB) {
	var sth sumdb.STH
	var rootHash []byte
	if err := db.QueryRowContext(req.Context(), `SELECT sth.tree_size, sth.root_hash, sth.signature FROM db JOIN sth ON sth.db_id = db.db_id AND sth.tree_size = (db.verified_position->>'size')::bigint WHERE db.address = $1`, address).Scan(&sth.TreeSize, &rootHash, &sth.Signature); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Go Checksum Database Not Found", 404)
		} else {
			log.Printf("ServeGossip: error loading gossip for sumdb %q: %s", address, err)
			http.Error(w, "Internal Database Error", 500)
		}
		return
	}
	sth.RootHash = (merkletree.Hash)(rootHash)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	fmt.Fprint(w, sth.Format(address))
}

func ReceiveGossip(address string, w http.ResponseWriter, req *http.Request, db *sql.DB) {
	sthBytes, err := io.ReadAll(http.MaxBytesReader(w, req.Body, 100000))
	if err != nil {
		http.Error(w, "Reading your request failed: "+err.Error(), 400)
		return
	}

	var (
		sumdbid int32
		key     []byte
	)
	if err := db.QueryRowContext(req.Context(), `SELECT db_id, key FROM db WHERE address = $1`, address).Scan(&sumdbid, &key); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Go Checksum Database Not Found", 404)
		} else {
			log.Printf("ReceiveGossip: error loading info for sumdb %q: %s", address, err)
			http.Error(w, "Internal Database Error", 500)
		}
		return
	}

	sth, err := sumdb.ParseAndAuthenticateSTH(sthBytes, address, key)
	if err != nil {
		http.Error(w, "Invalid STH: "+err.Error(), 400)
		return
	}

	if err := insert(req.Context(), db, sumdbid, sth, "gossip"); err != nil {
		log.Printf("ReceiveGossip: error inserting STH for sumdb %d: %s", sumdbid, err)
		http.Error(w, "500 Internal Database Error", 500)
		return
	}

	consistent, err := isConsistent(req.Context(), db, sumdbid, sth)
	if err != nil {
		log.Printf("ReceiveGossip: error querying consistency of STH for sumdb %d: %s", sumdbid, err)
		http.Error(w, "500 Internal Database Error", 500)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	if !consistent.Valid {
		fmt.Fprintf(w, "pending: we don't know yet if this STH is consistent with other STHs that we've seen from %s; we have have saved this STH and will audit it ASAP\n", address)
	} else if consistent.Bool {
		fmt.Fprintf(w, "consistent: this STH is consistent with other STHs that we've seen from %s\n", address)
	} else {
		fmt.Fprintf(w, "inconsistent: uh oh, this STH is NOT consistent with other STHs that we've seen from %s; it is possible that you have been served malicious code by the Go Module Mirror; we have saved this STH and will report it\n", address)
	}
}

func isConsistent(ctx context.Context, db *sql.DB, sumdbid int32, sth *sumdb.STH) (consistent sql.NullBool, err error) {
	err = db.QueryRowContext(ctx, `SELECT consistent FROM sth WHERE (db_id, tree_size, root_hash) = ($1, $2, $3)`, sumdbid, sth.TreeSize, sth.RootHash[:]).Scan(&consistent)
	return
}
