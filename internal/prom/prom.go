// Copyright (C) 2026 Opsmate, Inc.
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

// Package prom formats metrics in the Prometheus text exposition format
// (version 0.0.4).  It supports only gauges, which is all Source Spotter
// needs: every exported value is derived from database state at scrape time.
package prom

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ContentType is the media type of the text exposition format.
const ContentType = "text/plain; version=0.0.4; charset=utf-8"

// Family is a set of samples sharing one metric name.
type Family struct {
	name    string
	help    string
	samples []sample
}

type sample struct {
	labels []string // alternating name, value
	value  float64
}

// NewGauge creates an empty gauge family.  name must be a valid Prometheus
// metric name and help must be a single line.
func NewGauge(name, help string) *Family {
	return &Family{name: name, help: help}
}

// Add records a sample.  labels are alternating label names and values.
func (f *Family) Add(value float64, labels ...string) {
	if len(labels)%2 != 0 {
		panic("prom: Add called with odd number of label arguments")
	}
	f.samples = append(f.samples, sample{labels: labels, value: value})
}

// AddTimestamp records a sample whose value is t in seconds since the Unix
// epoch, which is the conventional representation of a point in time in
// Prometheus.  Callers with no time to report should omit the sample rather
// than pass a zero time, which would falsely claim 1970.
func (f *Family) AddTimestamp(t time.Time, labels ...string) {
	f.Add(float64(t.UnixMilli())/1000, labels...)
}

func escapeHelp(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

func escapeLabelValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

func formatLabels(labels []string) string {
	if len(labels) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i < len(labels); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(labels[i])
		b.WriteString(`="`)
		b.WriteString(escapeLabelValue(labels[i+1]))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

func (f *Family) write(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", f.name, escapeHelp(f.help), f.name); err != nil {
		return err
	}
	for _, s := range f.samples {
		if _, err := fmt.Fprintf(w, "%s%s %s\n", f.name, formatLabels(s.labels), strconv.FormatFloat(s.value, 'f', -1, 64)); err != nil {
			return err
		}
	}
	return nil
}

// Write writes families in the text exposition format.
func Write(w io.Writer, families []*Family) error {
	for _, f := range families {
		if err := f.write(w); err != nil {
			return err
		}
	}
	return nil
}

// Serve writes families as a successful HTTP response.
func Serve(w http.ResponseWriter, families []*Family) {
	w.Header().Set("Content-Type", ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	Write(w, families)
}
