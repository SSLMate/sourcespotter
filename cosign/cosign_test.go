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

package cosign

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"time"

	"golang.org/x/mod/sumdb/note"
)

const testCheckpoint = "go.sum database tree\n1262203\nsQ1Biyw3NQ7OBmLpfA5zZrs6xiB+o2ZjybBDj9cmnKA=\n"

func TestRoundTrip(t *testing.T) {
	skey, vkey, err := GenerateKey(rand.Reader, "example.com/toolchain")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(skey, "PRIVATE+KEY+example.com/toolchain+") {
		t.Errorf("unexpected signer key format: %s", skey)
	}
	if !strings.HasPrefix(vkey, "example.com/toolchain+") {
		t.Errorf("unexpected verifier key format: %s", vkey)
	}
	signer, err := NewSigner(skey)
	if err != nil {
		t.Fatal(err)
	}
	if signer.VerifierKey() != vkey {
		t.Errorf("Signer.VerifierKey() = %q, want %q", signer.VerifierKey(), vkey)
	}
	verifier, err := NewVerifier(vkey)
	if err != nil {
		t.Fatal(err)
	}
	if signer.Name() != verifier.Name() || signer.KeyHash() != verifier.KeyHash() {
		t.Errorf("signer and verifier identities differ")
	}

	when := time.Date(2026, 10, 23, 12, 34, 56, 789, time.UTC)
	cosig := signer.Cosign([]byte(testCheckpoint), when)
	if len(cosig) != CosignatureLen {
		t.Fatalf("cosignature is %d bytes, want %d", len(cosig), CosignatureLen)
	}
	got, err := verifier.VerifyCosignature([]byte(testCheckpoint), cosig)
	if err != nil {
		t.Fatalf("VerifyCosignature: %s", err)
	}
	if !got.Equal(when.Truncate(time.Second)) {
		t.Errorf("timestamp = %s, want %s", got, when.Truncate(time.Second))
	}

	// Tampering with any part must fail.
	for _, tc := range []struct {
		name       string
		checkpoint string
		cosig      []byte
	}{
		{"different checkpoint", strings.Replace(testCheckpoint, "1262203", "1262204", 1), cosig},
		{"different timestamp", testCheckpoint, flip(cosig, 11)},
		{"different key hash", testCheckpoint, flip(cosig, 0)},
		{"different signature", testCheckpoint, flip(cosig, CosignatureLen-1)},
		{"truncated", testCheckpoint, cosig[:CosignatureLen-1]},
	} {
		if _, err := verifier.VerifyCosignature([]byte(tc.checkpoint), tc.cosig); err == nil {
			t.Errorf("%s: VerifyCosignature unexpectedly succeeded", tc.name)
		}
	}

	// A different key must not verify.
	_, vkey2, _ := GenerateKey(rand.Reader, "example.com/toolchain")
	verifier2, _ := NewVerifier(vkey2)
	if _, err := verifier2.VerifyCosignature([]byte(testCheckpoint), cosig); err == nil {
		t.Errorf("VerifyCosignature with wrong key unexpectedly succeeded")
	}
}

func flip(b []byte, i int) []byte {
	c := bytes.Clone(b)
	c[i] ^= 0x01
	return c
}

// A cosigned checkpoint must be a valid signed note that the x/mod note
// package can open using Verifier as a note.Verifier.
func TestNoteInterop(t *testing.T) {
	skey, vkey, err := GenerateKey(rand.Reader, "example.com/toolchain")
	if err != nil {
		t.Fatal(err)
	}
	signer, _ := NewSigner(skey)
	verifier, _ := NewVerifier(vkey)

	cosig := signer.Cosign([]byte(testCheckpoint), time.Now())
	msg := testCheckpoint + "\n" + FormatSignatureLine(signer.Name(), cosig)

	n, err := note.Open([]byte(msg), note.VerifierList(verifier))
	if err != nil {
		t.Fatalf("note.Open: %s", err)
	}
	if n.Text != testCheckpoint {
		t.Errorf("note text = %q, want %q", n.Text, testCheckpoint)
	}
	if len(n.Sigs) != 1 || n.Sigs[0].Name != "example.com/toolchain" || n.Sigs[0].Hash != verifier.KeyHash() {
		t.Errorf("unexpected signatures: %+v", n.Sigs)
	}
	if len(n.UnverifiedSigs) != 0 {
		t.Errorf("unexpected unverified signatures: %+v", n.UnverifiedSigs)
	}
}

func TestBadKeys(t *testing.T) {
	skey, vkey, _ := GenerateKey(rand.Reader, "example.com/toolchain")
	for _, name := range []string{"", "has space", "has+plus", "tab\there"} {
		if _, _, err := GenerateKey(rand.Reader, name); err == nil {
			t.Errorf("GenerateKey(%q) unexpectedly succeeded", name)
		}
	}
	// Ed25519 (type 0x01) keys from the note package must be rejected.
	noteSkey, noteVkey, _ := note.GenerateKey(rand.Reader, "example.com/toolchain")
	for _, k := range []string{"", "PRIVATE+KEY+", noteSkey, vkey, strings.Replace(skey, "toolchain", "other", 1)} {
		if _, err := NewSigner(k); err == nil {
			t.Errorf("NewSigner(%q) unexpectedly succeeded", k)
		}
	}
	for _, k := range []string{"", noteVkey, skey, strings.Replace(vkey, "toolchain", "other", 1)} {
		if _, err := NewVerifier(k); err == nil {
			t.Errorf("NewVerifier(%q) unexpectedly succeeded", k)
		}
	}
}

// Known-answer test: check signedMessage against the example in
// https://c2sp.org/tlog-cosignature, and check that the key hash and
// cosignature layout match the specs when computed independently.
func TestKnownAnswer(t *testing.T) {
	checkpoint := []byte("example.com/behind-the-sofa\n20852163\nCsUYapGGPo4dkMgIAUqom/Xajj7h2fB2MPA3j2jxq2I=\n")
	want := "cosignature/v1\ntime 1679315147\nexample.com/behind-the-sofa\n20852163\nCsUYapGGPo4dkMgIAUqom/Xajj7h2fB2MPA3j2jxq2I=\n"
	if got := string(signedMessage(1679315147, checkpoint)); got != want {
		t.Errorf("signedMessage = %q, want %q", got, want)
	}

	const name = "example.com/witness"
	priv := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	pub := priv.Public().(ed25519.PublicKey)
	h := sha256.Sum256(append(append([]byte(name+"\n"), 0x04), pub...))
	wantHash := binary.BigEndian.Uint32(h[:4])

	skey := fmt.Sprintf("PRIVATE+KEY+%s+%08x+%s", name, wantHash, base64.StdEncoding.EncodeToString(append([]byte{0x04}, priv.Seed()...)))
	signer, err := NewSigner(skey)
	if err != nil {
		t.Fatal(err)
	}
	if signer.KeyHash() != wantHash {
		t.Errorf("KeyHash() = %08x, want %08x", signer.KeyHash(), wantHash)
	}
	wantVkey := fmt.Sprintf("%s+%08x+%s", name, wantHash, base64.StdEncoding.EncodeToString(append([]byte{0x04}, pub...)))
	if signer.VerifierKey() != wantVkey {
		t.Errorf("VerifierKey() = %q, want %q", signer.VerifierKey(), wantVkey)
	}

	cosig := signer.Cosign(checkpoint, time.Unix(1679315147, 0))
	if got := binary.BigEndian.Uint32(cosig[:4]); got != wantHash {
		t.Errorf("cosignature key hash = %08x, want %08x", got, wantHash)
	}
	if got := binary.BigEndian.Uint64(cosig[4:12]); got != 1679315147 {
		t.Errorf("cosignature timestamp = %d, want 1679315147", got)
	}
	if !ed25519.Verify(pub, []byte(want), cosig[12:]) {
		t.Errorf("cosignature does not verify as a raw Ed25519 signature over the spec message")
	}
}

// Cross-implementation test: a checkpoint cosigned by the Sunlight witness
// witness.navigli.sunlight.geomys.org, retrieved on 2026-10-26 from
// https://witness.navigli.skylight.geomys.org/77cf16e9c572682301e78fcff5de3f8dc0ea8be352829e77b0e29ec141761ef2/checkpoint
// (the tlog-witness monitoring API for origin "geomys.org/witness/test-log").
// The witness's verifier key is published at
// https://witness.navigli.sunlight.geomys.org/witness.v0.json
func TestSunlightWitnessVector(t *testing.T) {
	const (
		vkey       = "witness.navigli.sunlight.geomys.org+a3e00fe2+BNy/co4C1Hn1p+INwJrfUlgz7W55dSZReusH/GhUhJ/G"
		name       = "witness.navigli.sunlight.geomys.org"
		checkpoint = "geomys.org/witness/test-log\n1\nBCml5C32yqMcl0gjTrcSOeNVx59oPnSdytBzDGBO5k0=\n"
		logSigLine = "\u2014 geomys.org/witness/test-log wHh/9BPsoBNr2x0Ol3qPBYasIN0HI2ZiBg5ac0v3LQq/7F+YO7U4oWbDeJn1VaWVrlbSEM30Gr7WWYQjj2SBxRoJ/Ao=\n"
		cosigLine  = "\u2014 witness.navigli.sunlight.geomys.org o+AP4gAAAABq37a78GlUJuVjp3Apg0SU7ebagw2X60NTvoJzjl4WJhOO9HC5X5hlUGxfx4PQpG1Pk0dcuavycIeZf+J5qXDH0AveCA==\n"
		// The witness also cosigns with an ML-DSA-44 key (type 0x06), which
		// this package doesn't implement; truncated here since only the key
		// hash matters for the test (note.Open ignores unknown keys).
		mldsaLine = "\u2014 witness.navigli.sunlight.geomys.org a8RCSQAAAABq37a7m/icBlEKDN8LgCF79vwO7Wf24b/Tk8Cm6/Ecg7lGAxt/xKTPhx3pZG3XUfAQ4iT2EwqrnNT1X3AXUrfwTpJJm32zeSpy+U6GZYAT4OExG1FBA8/Vy621VTEckkViDMsBjwU/kSD6/VypDdRI\n"
	)
	verifier, err := NewVerifier(vkey)
	if err != nil {
		t.Fatal(err)
	}
	if verifier.Name() != name || verifier.KeyHash() != 0xa3e00fe2 {
		t.Errorf("verifier identity = %s+%08x, want %s+a3e00fe2", verifier.Name(), verifier.KeyHash(), name)
	}

	cosig, _ := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(cosigLine, "\u2014 "+name+" "), "\n"))
	ts, err := verifier.VerifyCosignature([]byte(checkpoint), cosig)
	if err != nil {
		t.Fatalf("VerifyCosignature: %s", err)
	}
	if want := time.Unix(0x6adfb6bb, 0).UTC(); !ts.Equal(want) {
		t.Errorf("timestamp = %s, want %s", ts, want)
	}
	if FormatSignatureLine(name, cosig) != cosigLine {
		t.Errorf("FormatSignatureLine does not reproduce the original line")
	}

	n, err := note.Open([]byte(checkpoint+"\n"+logSigLine+cosigLine+mldsaLine), note.VerifierList(verifier))
	if err != nil {
		t.Fatalf("note.Open: %s", err)
	}
	if len(n.Sigs) != 1 || n.Sigs[0].Name != name || n.Sigs[0].Hash != 0xa3e00fe2 {
		t.Errorf("note.Open verified signatures = %+v, want just the Ed25519 witness cosignature", n.Sigs)
	}
	if len(n.UnverifiedSigs) != 2 {
		t.Errorf("note.Open left %d unverified signatures, want 2 (log signature and ML-DSA cosignature)", len(n.UnverifiedSigs))
	}
}
