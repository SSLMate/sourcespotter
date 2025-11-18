package modules

import (
	"net/http"

	"software.sslmate.com/src/sourcespotter"
	basedashboard "software.sslmate.com/src/sourcespotter/internal/dashboard"
)

type dashboardData struct {
	Domain string
}

func ServeDashboard(w http.ResponseWriter, req *http.Request) {
	data := &dashboardData{Domain: sourcespotter.Domain}
	basedashboard.ServePage(w, req,
		"Go Module Monitoring - Source Spotter",
		"Monitor the versions of your modules observed by Source Spotter.",
		"modules.html", data)
}
