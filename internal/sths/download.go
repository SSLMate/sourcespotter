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

package sths

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"software.sslmate.com/src/sourcespotter/sumdb"
)

func Download(ctx context.Context, sumdbid int32, db *sql.DB) error {
	var address string
	var key []byte
	if err := db.QueryRowContext(ctx, `SELECT address, key FROM db WHERE db_id = $1`, sumdbid).Scan(&address, &key); err != nil {
		return fmt.Errorf("error loading info for sumdb %d: %w", sumdbid, err)
	}

	sth, err := sumdb.DownloadAndAuthenticateSTH(ctx, address, key)
	if err != nil {
		log.Printf("%s: %s", address, err)
		return nil
	}

	if err := insert(ctx, db, sumdbid, sth, "https://"+address+"/latest"); err != nil {
		return fmt.Errorf("error inserting downloaded STH for sumdb %d: %w", sumdbid, err)
	}

	return nil
}
