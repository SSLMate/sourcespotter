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
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	goversion "go/version"
	"log"
	"os"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"golang.org/x/mod/semver"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/sync/errgroup"
	"software.sslmate.com/src/sourcespotter/internal/httpclient"
	toolchainlambda "software.sslmate.com/src/sourcespotter/internal/toolchain/lambda"
	"software.sslmate.com/src/sourcespotter/toolchain"
	"src.agwa.name/go-dbutil"
)

var (
	Go120Object string
	Go120Hash   string
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
					Message: sqlValid("sumdb contains more than one checksum for this toolchain"),
				})
			}
			g.Go(func() error {
				if err := Audit(ctx, db, version, formatHash1(sha256)); err != nil {
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
func Audit(ctx context.Context, db *sql.DB, modversion string, expectedHash string) error {
	version, ok := toolchain.ParseModVersion(modversion)
	if !ok {
		return storeBuildResult(ctx, db, modversion, &buildResult{
			Status:  buildFailed,
			Message: sqlValid("unable to parse module version"),
		})
	}
	if version.GOOS == "darwin" {
		fixedHash, err := getFixedHash(ctx, version, expectedHash, toolchain.StripDarwinSig)
		if err != nil {
			return storeBuildResult(ctx, db, version.ModVersion(), &buildResult{
				Status:  buildFailed,
				Message: sqlValid(err.Error()),
			})
		}
		expectedHash = fixedHash
	}
	if goversion.Compare(version.GoVersion, "go1.21.0") < 0 {
		return process0(ctx, db, version, expectedHash)
	} else if goversion.Compare(version.GoVersion, "go1.24.0") < 0 {
		return process1(ctx, db, version, expectedHash)
	} else {
		return process2(ctx, db, version, expectedHash)
	}
}

// process a non-reproducible toolchain (prior to Go 1.21)
func process0(ctx context.Context, db *sql.DB, version toolchain.Version, expectedHash string) error {
	return storeBuildResult(ctx, db, version.ModVersion(), &buildResult{
		Status:  buildSkipped,
		Message: sqlValid("this version of Go is not reproducible"),
	})
}

// process a toolchain (Go 1.21 - 1.23) that can be reproduced with a non-reproducible Go 1.20 bootstrap toolchain
func process1(ctx context.Context, db *sql.DB, version toolchain.Version, expectedHash string) error {
	if Go120Object == "" || Go120Hash == "" {
		return storeBuildResult(ctx, db, version.ModVersion(), &buildResult{
			Status:  buildSkipped,
			Message: sqlValid("Go 1.20 bootstrap toolchain not configured"),
		})
	}
	bootstrapURL, err := presignGetObject(ctx, Go120Object)
	if err != nil {
		return fmt.Errorf("error presigning bootstrap download: %w", err)
	}
	return build(ctx, db, version, expectedHash, bootstrapURL, Go120Hash)
}

// process a toolchain (Go 1.24 or higher) that can be reproduced with a reproducible bootstrap toolchain
func process2(ctx context.Context, db *sql.DB, version toolchain.Version, expectedHash string) error {
	bootstrapVersion := toolchain.Version{
		GoVersion: toolchain.BootstrapToolchain(version.GoVersion),
		GOOS:      "linux",
		GOARCH:    LambdaArch,
	}
	var (
		bootstrapSHA256 []byte
		bootstrapStatus sql.Null[buildStatus]
	)
	if err := db.QueryRowContext(ctx, `
		SELECT record.source_sha256, toolchain_build.status
		FROM record
		LEFT JOIN toolchain_build USING (version)
		WHERE record.module = 'golang.org/toolchain' AND record.version = $1
	`, bootstrapVersion.ModVersion()).Scan(&bootstrapSHA256, &bootstrapStatus); err == sql.ErrNoRows {
		return storeBuildResult(ctx, db, version.ModVersion(), &buildResult{
			Status:  buildFailed,
			Message: sqlValid(fmt.Sprintf("bootstrap toolchain %q not found in module database", bootstrapVersion.ModVersion())),
		})
	} else if err != nil {
		return fmt.Errorf("error looking up bootstrap toolchain %q: %w", bootstrapVersion.ModVersion(), err)
	} else if bootstrapStatus.V != buildEqual {
		return storeBuildResult(ctx, db, version.ModVersion(), &buildResult{
			Status:  buildFailed,
			Message: sqlValid(fmt.Sprintf("bootstrap toolchain %q not verified to be reproducible", bootstrapVersion.ModVersion())),
		})
	}

	return build(ctx, db, version, expectedHash, toolchainURL(bootstrapVersion), formatHash1(bootstrapSHA256))
}

func build(ctx context.Context, db *sql.DB, version toolchain.Version, expectedHash string, bootstrapURL string, bootstrapHash string) error {
	sourceURL, err := SaveSource(ctx, db, version.GoVersion)
	if err != nil {
		return fmt.Errorf("error saving source code: %w", err)
	}
	lambdaEvent := &toolchainlambda.Event{
		Version:       version,
		SourceURL:     sourceURL,
		BootstrapURL:  bootstrapURL,
		BootstrapHash: bootstrapHash,
	}

	var buildID [16]byte
	rand.Read(buildID[:])

	var (
		zipObj = fmt.Sprintf("out/%s.%x.zip", version.ModVersion(), buildID)
		logObj = fmt.Sprintf("out/%s.%x.log", version.ModVersion(), buildID)
	)
	if url, err := presignPutObject(ctx, zipObj); err != nil {
		return fmt.Errorf("error presigning zip upload: %w", err)
	} else {
		lambdaEvent.ZipUploadURL = url
	}
	if url, err := presignPutObject(ctx, logObj); err != nil {
		return fmt.Errorf("error presigning log upload: %w", err)
	} else {
		lambdaEvent.LogUploadURL = url
	}

	payload, err := json.Marshal(lambdaEvent)
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
		BuildDuration: sqlValid(sqlInterval(duration)),
		BuildID:       sqlValid(buildID[:]),
	}
	if err != nil {
		result.Status = buildFailed
		result.Message = sqlValid(err.Error())
	} else if lambdaResult.FunctionError != nil {
		result.Status = buildFailed
		result.Message = sqlValid(string(lambdaResult.Payload))
	} else if isEqual, err := compare(ctx, zipObj, expectedHash); err != nil {
		result.Status = buildFailed
		result.Message = sqlValid(err.Error())
	} else if isEqual {
		result.Status = buildEqual
		if err := deleteObject(ctx, zipObj); err != nil {
			return fmt.Errorf("error deleting zip: %w", err)
		}
	} else {
		result.Status = buildUnequal
	}
	return storeBuildResult(ctx, db, version.ModVersion(), result)
}

func compare(ctx context.Context, zipObj string, expectedHash string) (bool, error) {
	toolchainReader, err := getObject(ctx, zipObj)
	if err != nil {
		return false, fmt.Errorf("error downloading built toolchain: %w", err)
	}
	defer toolchainReader.Close()

	toolchainFilename, err := httpclient.CopyToTempFile(toolchainReader)
	if err != nil {
		return false, fmt.Errorf("error saving built toolchain: %w", err)
	}
	defer os.Remove(toolchainFilename)

	gotHash, err := dirhash.HashZip(toolchainFilename, dirhash.Hash1)
	if err != nil {
		return false, fmt.Errorf("error hashing built toolchain: %w", err)
	}
	return gotHash == expectedHash, nil
}

func getFixedHash(ctx context.Context, version toolchain.Version, expectedHash string, fix toolchain.HashFixer) (string, error) {
	toolchainURL := toolchainURL(version)
	toolchainFilename, err := httpclient.DownloadToTempFile(ctx, toolchainURL)
	if err != nil {
		return "", err
	}
	defer os.Remove(toolchainFilename)

	gotHash, err := dirhash.HashZip(toolchainFilename, dirhash.Hash1)
	if err != nil {
		return "", fmt.Errorf("error hashing toolchain downloaded from %q: %w", toolchainURL, err)
	}
	if gotHash != expectedHash {
		return "", fmt.Errorf("toolchain downloaded from %q has unexpected hash %s (expected %s)", toolchainURL, gotHash, expectedHash)
	}

	fixedHash, err := toolchain.HashZip(toolchainFilename, dirhash.Hash1, fix)
	if err != nil {
		return "", fmt.Errorf("error fixing hash of toolchain downloaded from %q: %w", toolchainURL, err)
	}
	return fixedHash, nil
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
	Message       sql.Null[string]
	BuildID       sql.Null[[]byte]
	BuildDuration sql.Null[string]
}

func storeBuildResult(ctx context.Context, db *sql.DB, modversion string, result *buildResult) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO toolchain_build (version, status, message, build_id, build_duration)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (version)
		DO UPDATE SET
			inserted_at = EXCLUDED.inserted_at,
			status = EXCLUDED.status,
			message = EXCLUDED.message,
			build_id = EXCLUDED.build_id,
			build_duration = EXCLUDED.build_duration
	`, modversion, result.Status, result.Message, result.BuildID, result.BuildDuration)
	if err != nil {
		return fmt.Errorf("error storing %s build result for %q: %w", result.Status, modversion, err)
	}
	return nil
}

func formatHash1(sha256 []byte) string {
	return "h1:" + base64.StdEncoding.EncodeToString(sha256)
}

func sqlValid[T any](v T) sql.Null[T] {
	return sql.Null[T]{Valid: true, V: v}
}

func sqlInterval(d time.Duration) string {
	return fmt.Sprintf("%d milliseconds", d.Milliseconds())
}

func toolchainURL(version toolchain.Version) string {
	return fmt.Sprintf("https://proxy.golang.org/golang.org/toolchain/@v/%s.zip", version.ModVersion())
}
