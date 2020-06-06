package gossip

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"software.sslmate.com/src/sourcespotter/gosum"
)

func ServeGossip(address string, w http.ResponseWriter, req *http.Request, db *sql.DB) {
	var sth gosum.STH

	if err := db.QueryRow(`SELECT sth.tree_size, sth.root_hash, sth.signature FROM gosum_db db JOIN gosum_sth sth ON sth.db_id = db.id AND sth.tree_size = (db.verified_position->>'size')::bigint WHERE db.address = $1`, address).Scan(&sth.TreeSize, &sth.RootHash, &sth.Signature); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "404 Sum Database Not Found", 404)
		} else {
			log.Print("serveGossip: ", err)
			http.Error(w, "500 Internal Database Error", 500)
		}
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	fmt.Fprint(w, sth.Format(address))
}

func ReceiveGossip(address string, w http.ResponseWriter, req *http.Request, db *sql.DB) {
	sthBytes, err := ioutil.ReadAll(http.MaxBytesReader(w, req.Body, 100000))
	if err != nil {
		http.Error(w, "400 Reading your request failed: "+err.Error(), 400)
		return
	}

	var sumdbid int
	var key []byte
	if err := db.QueryRow(`SELECT id, key FROM gosum_db WHERE address = $1`, address).Scan(&sumdbid, &key); err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "404 Sum Database Not Found", 404)
		} else {
			log.Print("receiveGossip: ", err)
			http.Error(w, "500 Internal Database Error", 500)
		}
		return
	}

	sth, err := gosum.ParseSTH(sthBytes, address)
	if err != nil {
		http.Error(w, "400 Malformed STH: "+err.Error(), 400)
		return
	}
	if err := sth.Verify(key); err != nil {
		http.Error(w, "400 Invalid STH: "+err.Error(), 400)
		return
	}

	_, err = db.Exec(`INSERT INTO gosum_sth (db_id, tree_size, root_hash, signature) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, sumdbid, sth.TreeSize, sth.RootHash, sth.Signature)
	if err != nil {
		log.Print("receiveGossip: ", err)
		http.Error(w, "500 Internal Database Error", 500)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	fmt.Fprint(w, "Thanks for contributing to the Go Sum Database Gossip Server!\n")
}
