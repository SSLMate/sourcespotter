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

package sths

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"

	"software.sslmate.com/src/certspotter/merkletree"
	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/sumdb"
)

func ServeGossip(address string, w http.ResponseWriter, req *http.Request) {
	var sth sumdb.STH
	var rootHash []byte
	if err := sourcespotter.DB.QueryRowContext(req.Context(), `SELECT sth.tree_size, sth.root_hash, sth.signature FROM db JOIN sth ON sth.db_id = db.db_id AND sth.tree_size = (db.verified_position->>'size')::bigint WHERE db.address = $1`, address).Scan(&sth.TreeSize, &rootHash, &sth.Signature); err != nil {
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

func ReceiveGossip(address string, w http.ResponseWriter, req *http.Request) {
	sthBytes, err := io.ReadAll(http.MaxBytesReader(w, req.Body, 100000))
	if err != nil {
		http.Error(w, "Reading your request failed: "+err.Error(), 400)
		return
	}

	var (
		sumdbid int32
		key     []byte
	)
	if err := sourcespotter.DB.QueryRowContext(req.Context(), `SELECT db_id, key FROM db WHERE address = $1`, address).Scan(&sumdbid, &key); err != nil {
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

	if err := insert(req.Context(), sumdbid, sth, "gossip"); err != nil {
		log.Printf("ReceiveGossip: error inserting STH for sumdb %d: %s", sumdbid, err)
		http.Error(w, "500 Internal Database Error", 500)
		return
	}

	consistent, err := isConsistent(req.Context(), sumdbid, sth)
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

func isConsistent(ctx context.Context, sumdbid int32, sth *sumdb.STH) (consistent sql.NullBool, err error) {
	err = sourcespotter.DB.QueryRowContext(ctx, `SELECT consistent FROM sth WHERE (db_id, tree_size, root_hash) = ($1, $2, $3)`, sumdbid, sth.TreeSize, sth.RootHash[:]).Scan(&consistent)
	return
}
