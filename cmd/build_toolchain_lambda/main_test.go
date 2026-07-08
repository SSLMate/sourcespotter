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

package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"software.sslmate.com/src/sourcespotter/internal/httpclient"
)

func TestRedactURLError(t *testing.T) {
	const sensitive = "sensitive_query_string"

	raw := &url.Error{Op: "Put", URL: "https://doesnotexist.invalid/path?" + sensitive, Err: errors.New("timeout")}
	if !strings.Contains(raw.Error(), sensitive) {
		t.Fatalf("expected raw error to contain %q, got: %q", sensitive, raw.Error())
	}
	redacted := redactURLError(raw)
	if strings.Contains(redacted.Error(), sensitive) {
		t.Errorf("wrapped error still contains the query string: %q", redacted.Error())
	}

	var target *url.Error
	if !errors.As(redacted, &target) {
		t.Errorf("redacted error does not wrap *url.Error")
	}
}

func TestRedactURLErrorEndToEnd(t *testing.T) {
	const sensitive = "sensitive_query_string"

	_, raw := httpclient.Download(context.Background(), "https://doesnotexist.invalid/path?"+sensitive)
	if raw == nil {
		t.Fatal("expected request to a non-existent host to fail")
	}
	if !strings.Contains(raw.Error(), sensitive) {
		t.Fatalf("expected raw error to contain %q, got: %q", sensitive, raw.Error())
	}
	redacted := redactURLError(raw)
	if strings.Contains(redacted.Error(), sensitive) {
		t.Errorf("wrapped error still contains the query string: %q", redacted.Error())
	}
}

func TestRedactURLErrorHTTPStatusEndToEnd(t *testing.T) {
	const sensitive = "sensitive_query_string"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	_, raw := httpclient.Download(context.Background(), server.URL+"/path?"+sensitive)
	if raw == nil {
		t.Fatal("expected request returning a non-200 status to fail")
	}
	if !strings.Contains(raw.Error(), sensitive) {
		t.Fatalf("expected raw error to contain %q, got: %q", sensitive, raw.Error())
	}
	redacted := redactURLError(raw)
	if strings.Contains(redacted.Error(), sensitive) {
		t.Errorf("wrapped error still contains the query string: %q", redacted.Error())
	}
}
