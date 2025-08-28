package telemetry

import (
	"context"
	"embed"
	"log"
	"net/http"
	"time"

	"software.sslmate.com/src/sourcespotter"
	basedashboard "software.sslmate.com/src/sourcespotter/internal/dashboard"
	"src.agwa.name/go-dbutil"
)

//go:embed templates/*
var templates embed.FS

var dashboardTemplate = basedashboard.ParseTemplate(templates, "templates/dashboard.html")

type counterRow struct {
	Program string `sql:"program"`
	Type    string `sql:"type"`
	Name    string `sql:"name"`
}

type errorRow struct {
	Version string    `sql:"version"`
	Time    time.Time `sql:"inserted_at"`
	Error   string    `sql:"error"`
}

type dashboard struct {
	Domain   string
	Counters []counterRow
	Errors   []errorRow
}

func loadDashboard(ctx context.Context) (*dashboard, error) {
	dash := &dashboard{Domain: sourcespotter.Domain}
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &dash.Counters, `select distinct program,type,name from telemetry_counter order by program,type,name`); err != nil {
		return nil, err
	}
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &dash.Errors, `select version, inserted_at, error from telemetry_config where error is not null order by inserted_at desc`); err != nil {
		return nil, err
	}
	return dash, nil
}

func ServeDashboard(w http.ResponseWriter, req *http.Request) {
	dash, err := loadDashboard(req.Context())
	if err != nil {
		log.Printf("error loading telemetry dashboard: %s", err)
		http.Error(w, "Internal Database Error", 500)
		return
	}
	basedashboard.ServePage(w, req, "Telemetry Counters - Source Spotter", dashboardTemplate, dash)
}
