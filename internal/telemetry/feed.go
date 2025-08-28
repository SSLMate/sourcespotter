package telemetry

import (
	"encoding/csv"
	"log"
	"net/http"

	"software.sslmate.com/src/sourcespotter"
	"src.agwa.name/go-dbutil"
)

type counterCSVRow struct {
	Program string `sql:"program"`
	Type    string `sql:"type"`
	Name    string `sql:"name"`
}

func ServeCountersCSV(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	var rows []counterCSVRow
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &rows, `select distinct program,type,name from telemetry_counter order by program,type,name`); err != nil {
		log.Printf("error querying telemetry counters: %s", err)
		http.Error(w, "Internal Database Error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=UTF-8; header=present")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	cw.Write([]string{"Program", "Type", "Name"})
	for _, row := range rows {
		cw.Write([]string{row.Program, row.Type, row.Name})
	}
	cw.Flush()
}
