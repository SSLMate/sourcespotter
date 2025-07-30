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

package dashboard

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"runtime/debug"

	"software.sslmate.com/src/sourcespotter"
)

//go:embed assets/*
var Assets embed.FS

//go:embed templates/*
var templates embed.FS

var templateFuncs = template.FuncMap{}

var baseTemplate = template.Must(template.New("base.html").Funcs(templateFuncs).ParseFS(templates, "templates/base.html"))

var homeTemplate = ParseTemplate(templates, "templates/home.html")

func ParseTemplate(fs fs.FS, patterns ...string) *template.Template {
	return template.Must(template.Must(baseTemplate.Clone()).ParseFS(fs, patterns...))
}

type templateData struct {
	Domain    string
	BuildInfo *debug.BuildInfo
	Request   *http.Request
	Title     string
	Body      any
}

func ServePage(w http.ResponseWriter, req *http.Request, title string, tmpl *template.Template, body any) {
	data := &templateData{
		Domain:  sourcespotter.Domain,
		Request: req,
		Title:   title,
		Body:    body,
	}
	data.BuildInfo, _ = debug.ReadBuildInfo()
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Xss-Protection", "0")
	w.WriteHeader(http.StatusOK)
	tmpl.Execute(w, data)
}
