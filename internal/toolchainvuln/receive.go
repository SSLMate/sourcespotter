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
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"software.sslmate.com/src/sourcespotter"
)

var (
	// Match Go version patterns like "Go 1.25.6" or "Go 1.24.12"
	goVersionRegex = regexp.MustCompile(`Go (1\.\d+\.\d+)`)
	// Match CVE IDs like "CVE-2024-12345" (case-insensitive)
	cveIDRegex = regexp.MustCompile(`(?i)CVE-\d{4}-\d+`)
)

// ParseReleaseAnnouncement parses a Go release announcement email and extracts
// the Go versions and CVE IDs.
func ParseReleaseAnnouncement(subject, body string) (versions []string, cves []string) {
	// Extract Go versions from subject
	matches := goVersionRegex.FindAllStringSubmatch(subject, -1)
	for _, m := range matches {
		versions = append(versions, m[1])
	}

	// Extract CVE IDs from body
	cveMatches := cveIDRegex.FindAllString(body, -1)
	seen := make(map[string]bool)
	for _, cve := range cveMatches {
		cve = strings.ToUpper(cve)
		if !seen[cve] {
			seen[cve] = true
			cves = append(cves, cve)
		}
	}

	return versions, cves
}

// InsertToolchainVuln inserts a toolchain vulnerability record into the database.
func InsertToolchainVuln(ctx context.Context, goVersion, cveID string, releasedAt time.Time) error {
	_, err := sourcespotter.DB.ExecContext(ctx,
		`INSERT INTO toolchain_vuln (goversion, cveid, released_at)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (goversion, cveid) DO NOTHING`,
		goVersion, cveID, releasedAt)
	return err
}

// ReceiveAnnouncement handles POST requests with Go release announcement emails.
func ReceiveAnnouncement(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		log.Printf("error reading request body: %s", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// Extract subject from the email body
	// The subject line should be at the beginning, in format "Subject: ..."
	emailContent := string(body)
	subject, emailBody := parseEmail(emailContent)

	if subject == "" {
		http.Error(w, "No subject found in email", http.StatusBadRequest)
		return
	}

	versions, cves := ParseReleaseAnnouncement(subject, emailBody)

	if len(versions) == 0 {
		http.Error(w, "No Go versions found in subject", http.StatusBadRequest)
		return
	}

	if len(cves) == 0 {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "No CVEs found in announcement for versions: %v\n", versions)
		return
	}

	releasedAt := time.Now()
	inserted := 0

	for _, version := range versions {
		for _, cve := range cves {
			if err := InsertToolchainVuln(req.Context(), version, cve, releasedAt); err != nil {
				log.Printf("error inserting toolchain_vuln: %s", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			inserted++
		}
	}

	log.Printf("inserted %d toolchain_vuln records for versions %v and CVEs %v", inserted, versions, cves)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Inserted %d records for versions %v and CVEs %v\n", inserted, versions, cves)
}

// parseEmail extracts the subject and body from an email.
func parseEmail(content string) (subject, body string) {
	lines := strings.Split(content, "\n")
	inHeader := true
	var bodyLines []string

	for _, line := range lines {
		if inHeader {
			if strings.HasPrefix(strings.ToLower(line), "subject:") {
				subject = strings.TrimSpace(strings.TrimPrefix(line, "Subject:"))
				subject = strings.TrimSpace(strings.TrimPrefix(subject, "subject:"))
			} else if line == "" || line == "\r" {
				inHeader = false
			}
		} else {
			bodyLines = append(bodyLines, line)
		}
	}

	return subject, strings.Join(bodyLines, "\n")
}
