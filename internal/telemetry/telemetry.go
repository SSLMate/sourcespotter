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

package telemetry

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"golang.org/x/mod/sumdb/dirhash"
	"software.sslmate.com/src/sourcespotter"
	"software.sslmate.com/src/sourcespotter/internal/httpclient"
	"src.agwa.name/go-dbutil"
)

// RefreshCounters downloads and parses telemetry counter configurations
// referenced in the record table but missing from the telemetry_config table.
func RefreshCounters(ctx context.Context) error {
	var rows []struct {
		Version     string
		GoModSHA256 []byte `db:"gomod_sha256"`
	}
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &rows, `SELECT version, gomod_sha256 FROM record WHERE module = 'golang.org/x/telemetry/config' AND NOT EXISTS (SELECT 1 FROM telemetry_config WHERE telemetry_config.version = record.version)`); err != nil {
		return fmt.Errorf("error querying telemetry configs: %w", err)
	}
	for _, r := range rows {
		if err := refreshVersion(ctx, r.Version, r.GoModSHA256); err != nil {
			return err
		}
	}
	return nil
}

func refreshVersion(ctx context.Context, version string, gomodSHA256 []byte) error {
	url := fmt.Sprintf("https://proxy.golang.org/golang.org/x/telemetry/config/@v/%s.zip", version)
	filename, err := httpclient.DownloadToTempFile(ctx, url)
	if err != nil {
		return fmt.Errorf("error downloading telemetry config %s: %w", version, err)
	}
	defer os.Remove(filename)

	hash, err := dirhash.HashZip(filename, dirhash.Hash1)
	if err != nil {
		return fmt.Errorf("error hashing telemetry config %s: %w", version, err)
	}
	expected := "h1:" + base64.StdEncoding.EncodeToString(gomodSHA256)
	if hash != expected {
		return fmt.Errorf("telemetry config %s has unexpected hash %s (expected %s)", version, hash, expected)
	}

	cfg, err := readConfig(filename, version)
	if err != nil {
		return err
	}
	return insertConfig(ctx, version, cfg)
}

type configJSON struct {
	Programs []struct {
		Name     string
		Counters []struct {
			Name string
			Rate int
		}
		Stacks []struct {
			Name  string
			Rate  int
			Depth int
		}
	}
}

func readConfig(filename, version string) (*configJSON, error) {
	path := fmt.Sprintf("golang.org/x/telemetry/config@%s/config.json", version)
	z, err := zip.OpenReader(filename)
	if err != nil {
		return nil, fmt.Errorf("error opening telemetry config zip: %w", err)
	}
	defer z.Close()
	var data []byte
	for _, f := range z.File {
		if f.Name == path {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("error opening %q in zip: %w", path, err)
			}
			defer rc.Close()
			data, err = io.ReadAll(rc)
			if err != nil {
				return nil, fmt.Errorf("error reading %q in zip: %w", path, err)
			}
			break
		}
	}
	if data == nil {
		return nil, fmt.Errorf("telemetry config zip missing %q", path)
	}
	var cfg configJSON
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("error parsing telemetry config: %w", err)
	}
	return &cfg, nil
}

func insertConfig(ctx context.Context, version string, cfg *configJSON) error {
	tx, err := sourcespotter.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("error starting database transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `INSERT INTO telemetry_config (version) VALUES ($1)`, version); err != nil {
		return fmt.Errorf("error inserting telemetry_config row: %w", err)
	}
	for _, p := range cfg.Programs {
		for _, c := range p.Counters {
			if _, err := tx.ExecContext(ctx, `INSERT INTO telemetry_counter (version, program, name, type, rate) VALUES ($1,$2,$3,'counter',$4)`, version, p.Name, c.Name, c.Rate); err != nil {
				return fmt.Errorf("error inserting telemetry_counter row: %w", err)
			}
		}
		for _, s := range p.Stacks {
			if _, err := tx.ExecContext(ctx, `INSERT INTO telemetry_counter (version, program, name, type, rate, depth) VALUES ($1,$2,$3,'stack',$4,$5)`, version, p.Name, s.Name, s.Rate, s.Depth); err != nil {
				return fmt.Errorf("error inserting telemetry_counter row: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("error committing database transaction: %w", err)
	}
	return nil
}
