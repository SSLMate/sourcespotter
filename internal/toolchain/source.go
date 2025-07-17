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

package toolchain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"src.agwa.name/go-dbutil"
)

func saveSource(ctx context.Context, goversion string, url string) ([]byte, error) {
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(60*time.Second))
	defer cancel()

	source, err := downloadBytes(ctx, url)
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

	if _, err := tx.ExecContext(ctx, `UPDATE toolchain_source SET sha256 = $1 WHERE version = $2`, sha256, goversion); err != nil {
		return "", fmt.Errorf("error updating toolchain_source row: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("error committing database transaction: %w", err)
	}

	presignedURL, err := presignGetObject(ctx, sourceObjectName(goversion))
	if err != nil {
		return "", fmt.Errorf("error presigning source download: %w", err)
	}

	return presignedURL, nil
}
