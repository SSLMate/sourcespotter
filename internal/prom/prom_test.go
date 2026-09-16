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

package prom

import (
	"bytes"
	"math"
	"testing"
	"time"
)

func TestWrite(t *testing.T) {
	empty := NewGauge("test_empty", "No samples")
	g := NewGauge("test_gauge", `Has a \ backslash and a
newline`)
	g.Add(1)
	g.Add(40000000, "sumdb", "sum.golang.org")
	g.Add(1758000000.123, "a", `quote " backslash \ newline
`, "b", "")
	g.Add(math.Inf(1))
	g.Add(-0.5)

	var buf bytes.Buffer
	if err := Write(&buf, []*Family{empty, g}); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	want := `# HELP test_empty No samples
# TYPE test_empty gauge
# HELP test_gauge Has a \\ backslash and a\nnewline
# TYPE test_gauge gauge
test_gauge 1
test_gauge{sumdb="sum.golang.org"} 40000000
test_gauge{a="quote \" backslash \\ newline\n",b=""} 1758000000.123
test_gauge +Inf
test_gauge -0.5
`
	if got != want {
		t.Errorf("Write mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestAddTimestamp(t *testing.T) {
	g := NewGauge("test_timestamp_seconds", "Timestamps")
	g.AddTimestamp(time.Date(2025, 9, 16, 12, 34, 56, 789_000_000, time.UTC), "when", "then")

	var buf bytes.Buffer
	if err := Write(&buf, []*Family{g}); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	want := `# HELP test_timestamp_seconds Timestamps
# TYPE test_timestamp_seconds gauge
test_timestamp_seconds{when="then"} 1758026096.789
`
	if got != want {
		t.Errorf("Write mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}
