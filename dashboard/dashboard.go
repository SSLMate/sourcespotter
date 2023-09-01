package dashboard

import (
	"context"
	"database/sql"
	"embed"
	"encoding/base64"
	"html/template"
	"log"
	"net/http"
	"time"

	"software.sslmate.com/src/certspotter/merkletree"
	"software.sslmate.com/src/sourcespotter/sumdb"
	"src.agwa.name/go-dbutil"
)

//go:embed templates/*
var content embed.FS

var defaultDashboardTemplate = template.Must(template.ParseFS(content, "templates/dashboard.html"))

type SumDB struct {
	Address        string
	LargestSTHSize uint64
	LargestSTHTime time.Time
	DownloadSize   uint64
	VerifiedSize   uint64
}

func (db *SumDB) DownloadBacklog() uint64 {
	return db.LargestSTHSize - db.DownloadSize
}

func (db *SumDB) VerifyBacklog() uint64 {
	return db.LargestSTHSize - db.VerifiedSize
}

type InconsistentSTH struct {
	SumDB              string
	TreeSize           uint64
	RootHash           []byte
	CalculatedRootHash []byte
	Signature          []byte
}

func (sth *InconsistentSTH) RootHashString() string {
	return base64.StdEncoding.EncodeToString(sth.RootHash)
}

func (sth *InconsistentSTH) CalculatedRootHashString() string {
	return base64.StdEncoding.EncodeToString(sth.CalculatedRootHash)
}

func (sth *InconsistentSTH) STH() *sumdb.STH {
	return &sumdb.STH{
		TreeSize:  sth.TreeSize,
		RootHash:  (merkletree.Hash)(sth.RootHash),
		Signature: sth.Signature,
	}
}

func (sth *InconsistentSTH) DownloadURL() template.URL {
	sthString := sth.STH().Format(sth.SumDB)
	return template.URL("data:text/plain;charset=UTF-8;base64," + base64.StdEncoding.EncodeToString([]byte(sthString)))
}

type DuplicateRecord struct {
	SumDB            string
	Position         uint64
	PreviousPosition uint64
	Module           string
	Version          string
}

type Dashboard struct {
	SumDBs           []SumDB
	InconsistentSTHs []InconsistentSTH
	DuplicateRecords []DuplicateRecord
}

func LoadDashboard(ctx context.Context, db *sql.DB) (*Dashboard, error) {
	dashboard := new(Dashboard)

	if err := dbutil.QueryAll(ctx, db, &dashboard.SumDBs, `
		SELECT
			db.address AS "Address",
			largest_sth.tree_size AS "LargestSTHSize",
			largest_sth.observed_at AS "LargestSTHTime",
			db.download_position->>'size' AS "DownloadSize",
			db.verified_position->>'size' AS "VerifiedSize"
		FROM gosum.db
		LEFT JOIN LATERAL (
			SELECT DISTINCT ON (db_id) * FROM gosum.sth ORDER BY db_id, tree_size DESC
		) largest_sth USING (db_id)
		WHERE db.enabled
		ORDER BY db.address
	`); err != nil {
		return nil, err
	}

	if err := dbutil.QueryAll(ctx, db, &dashboard.InconsistentSTHs, `
		SELECT
			db.address AS "SumDB",
			sth.tree_size AS "TreeSize",
			sth.root_hash AS "RootHash",
			record.root_hash AS "CalculatedRootHash",
			sth.signature AS "Signature"
		FROM gosum.sth
		JOIN gosum.db USING (db_id)
		JOIN gosum.record ON (record.db_id, record.position) = (sth.db_id, sth.tree_size-1)
		WHERE sth.consistent = FALSE
		ORDER BY sth.db_id, sth.tree_size, sth.root_hash
	`); err != nil {
		return nil, err
	}

	if err := dbutil.QueryAll(ctx, db, &dashboard.DuplicateRecords, `
		SELECT
			db.address AS "SumDB",
			record.position AS "Position",
			record.previous_position AS "PreviousPosition",
			record.module AS "Module",
			record.version AS "Version"
		FROM gosum.record
		JOIN gosum.db USING (db_id)
		WHERE record.previous_position IS NOT NULL
		ORDER BY (record.db_id, record.position)
	`); err != nil {
		return nil, err
	}

	return dashboard, nil
}

func ServeHTTP(w http.ResponseWriter, req *http.Request, db *sql.DB, template *template.Template) {
	if template == nil {
		template = defaultDashboardTemplate
	}
	dashboard, err := LoadDashboard(req.Context(), db)
	if err != nil {
		log.Printf("error loading dashboard: %s", err)
		http.Error(w, "Internal Database Error", 500)
	}
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Xss-Protection", "0")
	w.WriteHeader(http.StatusOK)
	template.Execute(w, dashboard)
}
