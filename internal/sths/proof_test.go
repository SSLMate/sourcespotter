// Copyright (C) 2026 Opsmate, Inc.
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
	"errors"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/mod/sumdb/tlog"
)

// memLog is an in-memory log that serves tiles the way the sumdb does: a
// partial tile is served only while the tile is incomplete at the log's
// current size, and only for widths up to the current width.
type memLog struct {
	hashes  []tlog.Hash
	size    int64
	fetches int
}

func (l *memLog) add(record []byte) {
	stored, err := tlog.StoredHashesForRecordHash(l.size, tlog.RecordHash(record), l)
	if err != nil {
		panic(err)
	}
	l.hashes = append(l.hashes, stored...)
	l.size++
}

func (l *memLog) ReadHashes(indexes []int64) ([]tlog.Hash, error) {
	out := make([]tlog.Hash, len(indexes))
	for i, x := range indexes {
		out[i] = l.hashes[x]
	}
	return out, nil
}

func (l *memLog) tree(n int64) tlog.Tree {
	h, err := tlog.TreeHash(n, l)
	if err != nil {
		panic(err)
	}
	return tlog.Tree{N: n, Hash: h}
}

func (l *memLog) fetch(ctx context.Context, tile tlog.Tile) ([]byte, error) {
	l.fetches++
	// Width of this tile at the log's current size.
	tileLeaves := int64(1) << uint(tile.H*(tile.L+1))
	start := int64(tile.N) * tileLeaves
	if start >= l.size {
		return nil, errors.New("404 tile does not exist")
	}
	currentW := int((l.size - start) / (int64(1) << uint(tile.H*tile.L)))
	if currentW > 1<<uint(tile.H) {
		currentW = 1 << uint(tile.H)
	}
	full := tile.W == 1<<uint(tile.H)
	if (full && currentW != tile.W) || (!full && (currentW == 1<<uint(tile.H) || tile.W > currentW)) {
		return nil, fmt.Errorf("404 tile %s not available at size %d", tile.Path(), l.size)
	}
	return tlog.ReadTileData(tile, l)
}

func TestTileReader(t *testing.T) {
	log := new(memLog)
	for i := 0; i < 1000; i++ {
		log.add([]byte(fmt.Sprintf("record %d\n", i)))
	}
	// Prove records against an older tree size than the log's current size,
	// so that some tiles that were partial at that size are now full (and
	// only served as full tiles) and the right-edge tile is partial.
	const oldSize = 700
	old := log.tree(oldSize)
	log.add([]byte("record 1000\n"))

	reader := &tileReader{ctx: context.Background(), fetch: log.fetch, verified: make(map[tlog.Tile][]byte)}
	hashReader := tlog.TileHashReader(old, reader)
	for _, index := range []int64{0, 255, 256, 511, 512, 699} {
		p, err := tlog.ProveRecord(oldSize, index, hashReader)
		if err != nil {
			t.Fatalf("ProveRecord(%d): %s", index, err)
		}
		if err := tlog.CheckRecord(p, oldSize, old.Hash, index, tlog.RecordHash([]byte(fmt.Sprintf("record %d\n", index)))); err != nil {
			t.Errorf("CheckRecord(%d): %s", index, err)
		}
	}
	// Every tile needed for a tree of 700 leaves at height 8: level-0 tiles
	// 0 and 1 (full), level-0 tile 2 (partial, 188 wide, at size 700), and
	// the partial level-1 tile (2 wide).  Level-0 tile 2 is now full at the
	// log's current size, so the partial request fails and the reader
	// falls back to the full tile: 5 fetches in all.  Everything else is
	// served from memory.
	if want := 5; log.fetches != want {
		t.Errorf("fetched %d tiles, want %d", log.fetches, want)
	}
	// Re-proving must not fetch anything.
	if _, err := tlog.ProveRecord(oldSize, 3, hashReader); err != nil {
		t.Fatal(err)
	}
	if log.fetches != 5 {
		t.Errorf("re-proving fetched tiles: %d total", log.fetches)
	}

	// A tile that doesn't match the tree must be rejected.
	bad := &tileReader{ctx: context.Background(), verified: make(map[tlog.Tile][]byte), fetch: func(ctx context.Context, tile tlog.Tile) ([]byte, error) {
		data, err := log.fetch(ctx, tile)
		if err == nil {
			data[0] ^= 1
		}
		return data, err
	}}
	if _, err := tlog.ProveRecord(oldSize, 3, tlog.TileHashReader(old, bad)); err == nil {
		t.Errorf("ProveRecord accepted a corrupted tile")
	}

	// Proof encoding round trip.
	p, _ := tlog.ProveRecord(oldSize, 3, hashReader)
	decoded, err := decodeProof(encodeProof(p))
	if err != nil || len(decoded) != len(p) {
		t.Fatalf("decodeProof(encodeProof(p)) failed: %s", err)
	}
	for i := range p {
		if decoded[i] != p[i] {
			t.Errorf("hash %d differs after round trip", i)
		}
	}
	if _, err := decodeProof([]byte{1, 2, 3}); err == nil {
		t.Errorf("decodeProof accepted bad length")
	}
}

func TestWriteAttestation(t *testing.T) {
	var h1, h2 tlog.Hash
	h1[0], h2[0] = 1, 2
	var got strings.Builder
	writeAttestation(&got, []byte("a v1 h1:x\na v1/go.mod h1:y\n"), 7, tlog.RecordProof{h1, h2}, "go.sum database tree\n8\nAAAA\n\n— sum.golang.org sig\n")
	want := "a v1 h1:x\na v1/go.mod h1:y\n\nc2sp.org/tlog-proof@v1\nindex 7\n" +
		"AQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n" +
		"AgAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n" +
		"\ngo.sum database tree\n8\nAAAA\n\n— sum.golang.org sig\n"
	if got.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", got.String(), want)
	}
}
