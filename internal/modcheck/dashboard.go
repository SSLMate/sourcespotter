package modcheck

import (
	"embed"
	"net/http"

	"software.sslmate.com/src/sourcespotter"
	basedashboard "software.sslmate.com/src/sourcespotter/internal/dashboard"
)

//go:embed templates/*
var templates embed.FS

var dashboardTemplate = basedashboard.ParseTemplate(templates, "templates/dashboard.html")

type dashboardData struct {
	Domain string
}

func ServeDashboard(w http.ResponseWriter, req *http.Request) {
	data := &dashboardData{Domain: sourcespotter.Domain}
	basedashboard.ServePage(w, req,
		"Module Checksum Verifier - Source Spotter",
		"Verify a module's checksums in the Checksum Database.",
		dashboardTemplate, data)
}
