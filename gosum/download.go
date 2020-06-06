package gosum

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
)

const (
	tileSize       = 8
	recordsPerTile = 1 << tileSize
)

func splitRecords(input []byte) [][]byte {
	records := make([][]byte, 0, recordsPerTile)
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
	// TODO: use ctx for the request
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("Error reading response from %s: %w", url, err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s: %s", url, resp.Status, body)
	}
	return body, nil
}

func DownloadRecords(ctx context.Context, address string, begin, end uint64, recordsOut chan<- *Record) error {
	for begin < end {
		tile := begin / recordsPerTile
		skip := begin % recordsPerTile
		count := end - tile*recordsPerTile
		if count > recordsPerTile {
			count = recordsPerTile
		}
		log.Printf("DownloadRecords: [%d, %d): tile=%d, skip=%d, count=%d", begin, end, tile, skip, count)

		url := fmt.Sprintf("https://%s/tile/%d/data/%s", address, tileSize, formatTileIndex(tile))
		if count < recordsPerTile {
			url += fmt.Sprintf(".p/%d", count)
		}

		response, err := fetch(ctx, url)
		if err != nil {
			return err
		}

		records := splitRecords(response)
		if uint64(len(records)) != count {
			return fmt.Errorf("%s returned %d records instead of %d", url, len(records), count)
		}

		for i, recordBytes := range records {
			if uint64(i) < skip {
				continue
			}
			record, err := ParseRecord(recordBytes)
			if err != nil {
				return fmt.Errorf("%s returned invalid record at %d: %w", url, i, err)
			}
			select {
			case recordsOut <- record:
			case <-ctx.Done():
				return ctx.Err()
			}
			begin++
		}
	}
	return nil
}
