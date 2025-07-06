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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/mod/semver"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/sync/errgroup"
	"software.sslmate.com/src/sourcespotter/toolchain/toolchain"
	"src.agwa.name/go-dbutil"
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
	presigner := s3.NewPresignClient(newS3Client())
	for bootstrapVersion != "" {
		if toolchain.IsReproducible(bootstrapVersion) {
			modVersion := toolchain.Version{GoVersion: bootstrapVersion, GOOS: "linux", GOARCH: LambdaArch}.ModVersion()
			obj := toolchainObjectName(modVersion)
			presigned, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
				Bucket: aws.String(S3Bucket),
				Key:    aws.String(obj),
			}, s3.WithPresignExpires(24*time.Hour))
			if err != nil {
				return fmt.Errorf("error presigning bootstrap toolchain: %w", err)
			}
			lambdaInput.ToolchainURLs[modVersion] = presigned.URL
			break
		}
		if url, err := SaveSource(ctx, db, bootstrapVersion); err != nil {
			return fmt.Errorf("error saving source code for %s (needed for bootstrap): %w", bootstrapVersion, err)
		} else {
			lambdaInput.SourceURLs[bootstrapVersion] = url
		}
		bootstrapVersion = toolchain.BootstrapToolchain(bootstrapVersion)
	}

	obj := toolchainObjectName(version.ModVersion())
	presigned, err := presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(S3Bucket),
		Key:    aws.String(obj),
	}, s3.WithPresignExpires(24*time.Hour))
	if err != nil {
		return fmt.Errorf("error presigning zip upload: %w", err)
	}
	lambdaInput.ZipUploadURL = presigned.URL

	logObj := logObjectName(version.ModVersion())
	presignedLog, err := presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(S3Bucket),
		Key:    aws.String(logObj),
	}, s3.WithPresignExpires(24*time.Hour))
	if err != nil {
		return fmt.Errorf("error presigning log upload: %w", err)
	}
	lambdaInput.LogUploadURL = presignedLog.URL

	payload, err := json.Marshal(lambdaInput)
	if err != nil {
		return fmt.Errorf("error encoding lambda payload: %w", err)
	}

	log.Printf("invoking lambda %s for %s.%s-%s", LambdaFunc, version.GoVersion, version.GOOS, version.GOARCH)
	start := time.Now()
	out, err := newLambdaClient().Invoke(ctx, &lambda.InvokeInput{
		FunctionName: aws.String(LambdaFunc),
		Payload:      payload,
	})
	duration := time.Since(start)
	if err != nil {
		return storeBuildResult(ctx, db, modversion, &buildResult{
			Status:        buildFailed,
			Message:       sqlString(err.Error()),
			BuildDuration: sqlInterval(duration),
			LogFile:       sqlString(logObj),
		})
	}
	if out.FunctionError != nil {
		return storeBuildResult(ctx, db, modversion, &buildResult{
			Status:        buildFailed,
			Message:       sqlString(string(out.Payload)),
			BuildDuration: sqlInterval(duration),
			LogFile:       sqlString(logObj),
		})
	}

	tmpf, err := os.CreateTemp("", "download-*")
	if err != nil {
		return fmt.Errorf("error creating temp file: %w", err)
	}
	defer os.Remove(tmpf.Name())
	defer tmpf.Close()

	s3client := newS3Client()
	getOut, err := s3client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(S3Bucket),
		Key:    aws.String(obj),
	})
	if err != nil {
		return fmt.Errorf("error downloading built toolchain: %w", err)
	}
	defer getOut.Body.Close()
	if _, err := io.Copy(tmpf, getOut.Body); err != nil {
		return fmt.Errorf("error saving built toolchain: %w", err)
	}
	if err := tmpf.Close(); err != nil {
		return fmt.Errorf("error saving built toolchain: %w", err)
	}

	hashStr, err := dirhash.HashZip(tmpf.Name(), dirhash.Hash1)
	if err != nil {
		return fmt.Errorf("error hashing built toolchain: %w", err)
	}
	hb, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(hashStr, "h1:"))
	if err != nil {
		return fmt.Errorf("error decoding hash: %w", err)
	}
	status := buildEqual
	if !bytes.Equal(hb, expectedSHA256) {
		status = buildUnequal
	}

	return storeBuildResult(ctx, db, modversion, &buildResult{
		Status:        status,
		BuildDuration: sqlInterval(duration),
		LogFile:       sqlString(logObj),
	})
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
