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

package toolchainvuln

import (
	"testing"
)

func TestParseReleaseAnnouncement(t *testing.T) {
	tests := []struct {
		name         string
		subject      string
		body         string
		wantVersions []string
		wantCVEs     []string
	}{
		{
			name:         "two versions",
			subject:      "Go 1.26.1 and Go 1.25.8 are released",
			body:         "This release fixes CVE-2024-12345 and CVE-2024-67890.",
			wantVersions: []string{"1.26.1", "1.25.8"},
			wantCVEs:     []string{"CVE-2024-12345", "CVE-2024-67890"},
		},
		{
			name:         "security prefix",
			subject:      "[security] Go 1.25.6 and Go 1.24.12 are released",
			body:         "Fixes CVE-2024-11111.",
			wantVersions: []string{"1.25.6", "1.24.12"},
			wantCVEs:     []string{"CVE-2024-11111"},
		},
		{
			name:         "single version",
			subject:      "[security] Go 1.25.2 is released",
			body:         "This fixes CVE-2024-99999.",
			wantVersions: []string{"1.25.2"},
			wantCVEs:     []string{"CVE-2024-99999"},
		},
		{
			name:         "multiple CVEs",
			subject:      "Go 1.23.4 and Go 1.22.10 are released",
			body:         "CVE-2024-45337\nCVE-2024-45338\ncve-2024-45339",
			wantVersions: []string{"1.23.4", "1.22.10"},
			wantCVEs:     []string{"CVE-2024-45337", "CVE-2024-45338", "CVE-2024-45339"},
		},
		{
			name:         "no CVEs",
			subject:      "Go 1.23.5 is released",
			body:         "Bug fixes only.",
			wantVersions: []string{"1.23.5"},
			wantCVEs:     nil,
		},
		{
			name:         "duplicate CVEs",
			subject:      "Go 1.24.0 is released",
			body:         "CVE-2024-12345 is fixed. Also CVE-2024-12345 again.",
			wantVersions: []string{"1.24.0"},
			wantCVEs:     []string{"CVE-2024-12345"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			versions, cves := ParseReleaseAnnouncement(tt.subject, tt.body)

			if len(versions) != len(tt.wantVersions) {
				t.Errorf("got %d versions, want %d", len(versions), len(tt.wantVersions))
			} else {
				for i, v := range versions {
					if v != tt.wantVersions[i] {
						t.Errorf("version[%d] = %q, want %q", i, v, tt.wantVersions[i])
					}
				}
			}

			if len(cves) != len(tt.wantCVEs) {
				t.Errorf("got %d CVEs, want %d", len(cves), len(tt.wantCVEs))
			} else {
				for i, c := range cves {
					if c != tt.wantCVEs[i] {
						t.Errorf("cve[%d] = %q, want %q", i, c, tt.wantCVEs[i])
					}
				}
			}
		})
	}
}

func TestParseEmail(t *testing.T) {
	email := `From: golang-announce@googlegroups.com
Subject: [security] Go 1.25.6 and Go 1.24.12 are released
Date: Mon, 1 Jan 2024 12:00:00 +0000

Hello gophers,

This release fixes CVE-2024-12345.

Best,
The Go Team`

	subject, body := parseEmail(email)

	wantSubject := "[security] Go 1.25.6 and Go 1.24.12 are released"
	if subject != wantSubject {
		t.Errorf("subject = %q, want %q", subject, wantSubject)
	}

	if body == "" {
		t.Error("body is empty")
	}

	if !contains(body, "CVE-2024-12345") {
		t.Errorf("body does not contain CVE: %q", body)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
