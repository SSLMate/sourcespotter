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

// Package toolchain verifies that the toolchains in the sumdb are reproducible
package toolchain

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"golang.org/x/mod/semver"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/sync/errgroup"
	"software.sslmate.com/src/sourcespotter/toolchain"
	"src.agwa.name/go-dbutil"
)

// AuditAll tries building all toolchains in the sumdb that haven't already been built
func AuditAll(ctx context.Context, db *sql.DB) error {
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
				if err := Audit(ctx, db, version, sha256); err != nil {
					return fmt.Errorf("error building %s: %w", version, err)
				}
				return nil
			})
		}
		return nil
	})
	return g.Wait()
}

// Audit checks that the building the given toolchain results in the given checksum
func Audit(ctx context.Context, db *sql.DB, modversion string, expectedSHA256 []byte) error {
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
			obj := toolchainObjectName(modVersion)
			if url, err := presignGetObject(ctx, obj); err != nil {
				return fmt.Errorf("error presigning bootstrap toolchain: %w", err)
			} else {
				lambdaInput.ToolchainURLs[modVersion] = url
			}
			break
		}
		if url, err := SaveSource(ctx, db, bootstrapVersion); err != nil {
			return fmt.Errorf("error saving source code for %s (needed for bootstrap): %w", bootstrapVersion, err)
		} else {
			lambdaInput.SourceURLs[bootstrapVersion] = url
		}
		bootstrapVersion = toolchain.BootstrapToolchain(bootstrapVersion)
	}

	var (
		zipObj = toolchainObjectName(version.ModVersion())
		logObj = logObjectName(version.ModVersion())
	)

	if url, err := presignPutObject(ctx, zipObj); err != nil {
		return fmt.Errorf("error presigning zip upload: %w", err)
	} else {
		lambdaInput.ZipUploadURL = url
	}

	if url, err := presignPutObject(ctx, logObj); err != nil {
		return fmt.Errorf("error presigning log upload: %w", err)
	} else {
		lambdaInput.LogUploadURL = url
	}

	payload, err := json.Marshal(lambdaInput)
	if err != nil {
		return fmt.Errorf("error encoding lambda payload: %w", err)
	}

	log.Printf("invoking lambda %s for %s.%s-%s", LambdaFunc, version.GoVersion, version.GOOS, version.GOARCH)
	start := time.Now()
	lambdaResult, err := newLambdaClient().Invoke(ctx, &lambda.InvokeInput{
		FunctionName: aws.String(LambdaFunc),
		Payload:      payload,
	})
	duration := time.Since(start)
	result := &buildResult{
		BuildDuration: sqlInterval(duration),
		LogFile:       sqlString(logObj),
	}
	if err != nil {
		result.Status = buildFailed
		result.Message = sqlString(err.Error())
	} else if lambdaResult.FunctionError != nil {
		result.Status = buildFailed
		result.Message = sqlString(string(lambdaResult.Payload))
	} else if isEqual, err := compare(ctx, version, expectedSHA256); err != nil {
		result.Status = buildFailed
		result.Message = sqlString(err.Error())
	} else if isEqual {
		result.Status = buildEqual
		if err := deleteObject(ctx, zipObj); err != nil {
			return fmt.Errorf("error deleting zip: %w", err)
		}
	} else {
		result.Status = buildUnequal
	}
	return storeBuildResult(ctx, db, modversion, result)
}

func compare(ctx context.Context, version toolchain.Version, expectedSHA256 []byte) (bool, error) {
	toolchainReader, err := getObject(ctx, toolchainObjectName(version.ModVersion()))
	if err != nil {
		return false, fmt.Errorf("error downloading built toolchain: %w", err)
	}
	toolchainFilename, err := copyToTempFile(toolchainReader)
	if err != nil {
		return false, fmt.Errorf("error saving built toolchain: %w", err)
	}
	defer os.Remove(toolchainFilename)

	gotHash, err := dirhash.HashZip(toolchainFilename, dirhash.Hash1)
	if err != nil {
		return false, fmt.Errorf("error hashing built toolchain: %w", err)
	}
	return gotHash == "h1:"+base64.StdEncoding.EncodeToString(expectedSHA256), nil
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
