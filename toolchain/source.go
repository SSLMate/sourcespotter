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
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"src.agwa.name/go-dbutil"
)

func download(ctx context.Context, getURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", getURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &url.Error{Op: "Get", URL: getURL, Err: errors.New(resp.Status)}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &url.Error{Op: "Get", URL: getURL, Err: err}
	}

	return data, nil
}

func saveSource(ctx context.Context, goversion string, url string) ([]byte, error) {
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(60*time.Second))
	defer cancel()

	source, err := download(ctx, url)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(source)

	client := newS3Client()
	key := sourceObjectName(goversion)
	if _, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(S3Bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(source),
	}); err != nil {
		return nil, err
	}
	log.Printf("saved Go source %s to S3 bucket %s", goversion, S3Bucket)

	return sum[:], nil
}

func SaveSource(ctx context.Context, db *sql.DB, goversion string) (string, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("error starting database transaction: %w", err)
	}
	defer tx.Rollback()

	url := fmt.Sprintf("https://go.dev/dl/%s.src.tar.gz", goversion)

	if err := dbutil.MustAffectRow(tx.ExecContext(ctx, `INSERT INTO toolchain_source (version, url) VALUES($1, $2) ON CONFLICT (version) DO NOTHING`, goversion, url)); err == sql.ErrNoRows {
		if err := tx.Rollback(); err != nil {
			return "", fmt.Errorf("error rolling back database transaction: %w", err)
		}
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("error inserting toolchain_source row: %w", err)
	}

	sha256, err := saveSource(ctx, goversion, url)
	if err != nil {
		return "", err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE toolchain_source SET sha256 = $1 WHERE version = $2`, goversion, sha256); err != nil {
		return "", fmt.Errorf("error updating toolchain_source row: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("error committing database transaction: %w", err)
	}

	presigner := s3.NewPresignClient(newS3Client())
	presigned, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(S3Bucket),
		Key:    aws.String(sourceObjectName(goversion)),
	}, s3.WithPresignExpires(24*time.Hour))
	if err != nil {
		return "", fmt.Errorf("error presigning source download: %w", err)
	}

	return presigned.URL, nil
}
