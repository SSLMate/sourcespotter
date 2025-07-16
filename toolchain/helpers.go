// Copyright (C) 2025 Opsmate, Inc.
//
// This Source Code Form is subject to the terms of the Mozilla
// Public License, v. 2.0. If a copy of the MPL was not distributed
// with this file, You can obtain one at http://mozilla.org/MPL/2.0/.
//
// This software is distributed WITHOUT A WARRANTY OF ANY KIND.
// See the Mozilla Public License for details.

package toolchain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

func download(ctx context.Context, getURL string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", getURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, &url.Error{Op: "Get", URL: getURL, Err: errors.New(resp.Status)}
	}
	return resp.Body, nil
}

func downloadBytes(ctx context.Context, getURL string) ([]byte, error) {
	r, err := download(ctx, getURL)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, &url.Error{Op: "Get", URL: getURL, Err: err}
	}
	return data, nil
}

func copyToTempFile(r io.ReadCloser) (filename string, retErr error) {
	defer r.Close()
	f, err := os.CreateTemp("", "tmp")
	if err != nil {
		return "", err
	}
	defer func() {
		f.Close()
		if retErr != nil {
			os.Remove(f.Name())
		}
	}()
	if _, err := io.Copy(f, r); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func sqlString(s string) sql.NullString {
	return sql.NullString{Valid: true, String: s}
}

func sqlInterval(d time.Duration) sql.NullString {
	return sqlString(fmt.Sprintf("%d milliseconds", d.Milliseconds()))
}
