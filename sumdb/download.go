// Copyright (C) 2023 Opsmate, Inc.
//
// This Source Code Form is subject to the terms of the Mozilla
// Public License, v. 2.0. If a copy of the MPL was not distributed
// with this file, You can obtain one at http://mozilla.org/MPL/2.0/.
//
// This software is distributed WITHOUT A WARRANTY OF ANY KIND.
// See the Mozilla Public License for details.

package sumdb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	mathrand "math/rand/v2"
	"net/http"
	"time"
)

var httpClient = http.DefaultClient

const (
	TileSize       = 8
	RecordsPerTile = 1 << TileSize
)

func splitRecords(input []byte) [][]byte {
	records := make([][]byte, 0, RecordsPerTile)
	for {
		nlnl := bytes.Index(input, []byte{'\n', '\n'})
		if nlnl == -1 {
			records = append(records, input)
			break
		}
		records = append(records, input[:nlnl+1])
		input = input[nlnl+2:]
	}
	return records
}

func formatTileIndex(tile uint64) string {
	str := ""
	for {
		rem := tile % 1000
		tile = tile / 1000

		if str == "" {
			str = fmt.Sprintf("%03d", rem)
		} else {
			str = fmt.Sprintf("x%03d/%s", rem, str)
		}

		if tile == 0 {
			break
		}
	}
	return str
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "sourcespotter")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("error reading response from %s: %w", url, err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s: %s", url, resp.Status, body)
	}
	return body, nil
}

func fetchRecords(ctx context.Context, address string, begin, end uint64) ([]*Record, error) {
	tile := begin / RecordsPerTile
	skip := begin % RecordsPerTile
	count := end - tile*RecordsPerTile
	if count > RecordsPerTile {
		count = RecordsPerTile
	}
	//log.Printf("fetchrecords: [%d, %d): tile=%d, skip=%d, count=%d", begin, end, tile, skip, count)

	url := fmt.Sprintf("https://%s/tile/%d/data/%s", address, TileSize, formatTileIndex(tile))
	if count < RecordsPerTile {
		url += fmt.Sprintf(".p/%d", count)
	}

	response, err := fetch(ctx, url)
	if err != nil {
		return nil, err
	}

	records := splitRecords(response)
	if uint64(len(records)) != count {
		return nil, fmt.Errorf("%s returned %d records instead of %d", url, len(records), count)
	}
	records = records[skip:]

	parsedRecords := make([]*Record, len(records))
	for i, recordBytes := range records {
		if parsedRecord, err := ParseRecord(recordBytes); err != nil {
			return nil, fmt.Errorf("%s returned invalid record at %d: %w", url, skip+uint64(i), err)
		} else {
			parsedRecords[i] = parsedRecord
		}
	}
	return parsedRecords, nil
}

func DownloadRecords(ctx context.Context, address string, begin, end uint64, recordsOut chan<- *Record) error {
	numRetries := 0

	for begin < end && ctx.Err() == nil {
		records, err := fetchRecords(ctx, address, begin, end)
		if err != nil {
			log.Printf("%s: error downloading records [%d,%d): %s", address, begin, end, err)
			if err := randomSleep(ctx, 1*time.Second*(1<<numRetries), 2*time.Second*(1<<numRetries)); err != nil {
				return err
			}
			if numRetries < 8 {
				numRetries++
			}
			continue
		}
		numRetries = 0
		if err := sendAllRecords(ctx, recordsOut, records); err != nil {
			return err
		}
		begin += uint64(len(records))
	}
	return ctx.Err()
}

func DownloadAndAuthenticateSTH(ctx context.Context, address string, key []byte) (*STH, error) {
	url := fmt.Sprintf("https://%s/latest", address)

	response, err := fetch(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("error downloading STH: %w", err)
	}

	sth, err := ParseSTH(response, address)
	if err != nil {
		return nil, fmt.Errorf("error parsing STH downloaded from %s: %w", url, err)
	}

	if err := sth.Authenticate(key); err != nil {
		return nil, fmt.Errorf("error authenticating STH downloaded from %s: %w", url, err)
	}

	return sth, nil
}

func sendAllRecords(ctx context.Context, ch chan<- *Record, values []*Record) error {
	for _, value := range values {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ch <- value:
		}
	}
	return nil
}

func randomSleep(ctx context.Context, minDuration time.Duration, maxDuration time.Duration) error {
	duration := minDuration + mathrand.N(maxDuration-minDuration)
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
