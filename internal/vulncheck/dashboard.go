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

package vulncheck

import (
	"net/http"

	"software.sslmate.com/src/sourcespotter"
	basedashboard "software.sslmate.com/src/sourcespotter/internal/dashboard"
)

var targets = []string{
	"aix/ppc64",
	"android/386",
	"android/amd64",
	"android/arm",
	"android/arm64",
	"darwin/amd64",
	"darwin/arm64",
	"dragonfly/amd64",
	"freebsd/386",
	"freebsd/amd64",
	"freebsd/arm",
	"freebsd/arm64",
	"freebsd/riscv64",
	"illumos/amd64",
	"ios/amd64",
	"ios/arm64",
	"js/wasm",
	"linux/386",
	"linux/amd64",
	"linux/arm",
	"linux/arm64",
	"linux/loong64",
	"linux/mips",
	"linux/mips64",
	"linux/mips64le",
	"linux/mipsle",
	"linux/ppc64",
	"linux/ppc64le",
	"linux/riscv64",
	"linux/s390x",
	"netbsd/386",
	"netbsd/amd64",
	"netbsd/arm",
	"netbsd/arm64",
	"openbsd/386",
	"openbsd/amd64",
	"openbsd/arm",
	"openbsd/arm64",
	"openbsd/ppc64",
	"openbsd/riscv64",
	"plan9/386",
	"plan9/amd64",
	"plan9/arm",
	"solaris/amd64",
	"wasip1/wasm",
	"windows/386",
	"windows/amd64",
	"windows/arm64",
}

type dashboardData struct {
	Domain  string
	GoAPI   string
	Targets []string
}

func ServeDashboard(w http.ResponseWriter, req *http.Request) {
	data := &dashboardData{
		Domain:  sourcespotter.Domain,
		GoAPI:   sourcespotter.GoAPI,
		Targets: targets,
	}

	basedashboard.ServePage(w, req,
		"Go Vulnerability Check - Source Spotter",
		"Check a Go package for known vulnerabilities using govulncheck.",
		"vulns.html", data)
}
