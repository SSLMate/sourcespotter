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
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"

	"golang.org/x/mod/semver"
	"golang.org/x/sync/errgroup"
	"src.agwa.name/go-dbutil"
	"software.sslmate.com/src/sourcespotter/toolchain/toolchain"
)

func BuildAll(ctx context.Context, db *sql.DB) error {
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(10)
	g.Go(func() error {
		type toolchainModule struct {
			Version string `sql:"version"` // e.g. "v0.0.1-go1.21.0.linux-amd64"
			SHA256  []byte `sql:"source_sha256"`
		}
		var toolchains []toolchainModule
		if err := dbutil.QueryAll(ctx, db, &toolchains, `SELECT version, source_sha256 FROM record WHERE module = 'golang.org/toolchain' AND NOT(EXISTS(SELECT 1 FROM toolchain_build WHERE toolchain_build.version = record.version))`); err != nil {
			return fmt.Errorf("error querying unbuilt toolchains: %w", err)
		}
		slices.SortFunc(toolchains, func(a, b toolchainModule) int { return semver.Compare(a.Version, b.Version) })

		for i := 0; i < len(toolchains); {
			var (
				version = toolchains[i].Version
				sha256  = toolchains[i].SHA256
			)
			i++
			inconsistent := false
			for i < len(toolchains) && toolchains[i].Version == version {
				if !bytes.Equal(toolchains[i].SHA256, sha256) {
					inconsistent = true
				}
				i++
			}
			if inconsistent {
				return storeBuildResult(ctx, db, version, &buildResult{
					Status:  buildFailed,
					Message: sqlString("sumdb contains more than one checksum for this toolchain"),
				})
			}
			g.Go(func() error {
				if err := Build(ctx, db, version, sha256); err != nil {
					return fmt.Errorf("error building %s: %w", version, err)
				}
				return nil
			})
		}
		return nil
	})
	return g.Wait()
}

func Build(ctx context.Context, db *sql.DB, modversion string, expectedSHA256 []byte) error {
	type lambdaInputType struct {
		Version       toolchain.Version
		SourceURLs    map[string]string
		ToolchainURLs map[string]string
		ZipUploadURL  string
		LogUploadURL  string
	}
	version, ok := toolchain.ParseModVersion(modversion)
	if !ok {
		return storeBuildResult(ctx, db, modversion, &buildResult{
			Status:  buildFailed,
			Message: sqlString("unable to parse module version"),
		})
	}
	if !toolchain.IsReproducible(version.GoVersion) {
		return storeBuildResult(ctx, db, modversion, &buildResult{
			Status:  buildSkipped,
			Message: sqlString("this version of Go is not reproducible"),
		})
	}
	lambdaInput := &lambdaInputType{
		Version:       version,
		SourceURLs:    make(map[string]string),
		ToolchainURLs: make(map[string]string),
	}

	if url, err := SaveSource(ctx, db, version.GoVersion); err != nil {
		return fmt.Errorf("error saving source code: %w", err)
	} else {
		lambdaInput.SourceURLs[version.GoVersion] = url
	}

	bootstrapVersion := toolchain.BootstrapToolchain(version.GoVersion)
	for bootstrapVersion != "" {
		if toolchain.IsReproducible(bootstrapVersion) {
			modVersion := toolchain.Version{GoVersion: bootstrapVersion, GOOS: "linux", GOARCH: LambdaArch}.ModVersion()
			zipURL := "" // TODO: create a pre-signed URL to GET toolchain/{modVersion}.zip from S3 bucket, assign to zipURL
			lambdaInput.ToolchainURLs[modVersion] = zipURL
			break
		}
		if url, err := SaveSource(ctx, db, bootstrapVersion); err != nil {
			return fmt.Errorf("error saving source code for %s (needed for bootstrap): %w", bootstrapVersion, err)
		} else {
			lambdaInput.SourceURLs[bootstrapVersion] = url
		}
		bootstrapVersion = toolchain.BootstrapToolchain(bootstrapVersion)
	}

	// TODO: create pre-signed S3 URL to PUT toolchain/{version.ModVersion()}.zip to S3, assign to lambdaInput.ZipUploadURL
	// TODO: create pre-signed S3 URL to PUT log/{version.ModVersion()}@{time.Now().UTC().Format(time.RFC3339)} to S3, assign to lambdaInput.LogUploadURL
	// TODO: launch lambda to build the toolchain, passing it lambdaInput as the event
	// TODO: if lambda was successful, download toolchain/{version.ModVersion()}.zip to a temporary file and hash it using dirhash.HashZip(zipfile, dirhash.Hash1) from golang.org/x/mod/sumdb/dirhash package and compare the hash against expectedSHA256
	// TODO: call storeBuildResult

	return nil
}

type buildStatus string

const (
	buildSkipped buildStatus = "skipped"
	buildEqual   buildStatus = "equal"
	buildUnequal buildStatus = "unequal"
	buildFailed  buildStatus = "failed"
)

type buildResult struct {
	Status        buildStatus
	Message       sql.NullString
	LogFile       sql.NullString
	BuildDuration sql.NullString
}

func storeBuildResult(ctx context.Context, db *sql.DB, modversion string, result *buildResult) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO toolchain_build (version, status, message, log_file, build_duration)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (version)
		DO UPDATE SET
			inserted_at = EXCLUDED.inserted_at,
			status = EXCLUDED.status,
			message = EXCLUDED.message,
			log_file = EXCLUDED.log_file,
			build_duration = EXCLUDED.build_duration
	`, modversion, result.Status, result.Message, result.LogFile, result.BuildDuration)
	if err != nil {
		return fmt.Errorf("error storing %s build result for %q: %w", result.Status, modversion, err)
	}
	return nil
}

func sqlString(s string) sql.NullString {
	return sql.NullString{Valid: true, String: s}
}

func sqlInterval(d time.Duration) sql.NullString {
	return sqlString(fmt.Sprintf("%d milliseconds", d.Milliseconds()))
}
