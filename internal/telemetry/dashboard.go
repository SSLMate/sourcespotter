package telemetry

import (
	"context"
	"embed"
	"log"
	"net/http"

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

type dashboard struct {
	Domain   string
	Counters []counterRow
}

func loadDashboard(ctx context.Context) (*dashboard, error) {
	dash := &dashboard{Domain: sourcespotter.Domain}
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &dash.Counters, `select distinct program,type,name from telemetry_counter order by program,type,name`); err != nil {
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
