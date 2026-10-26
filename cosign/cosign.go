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

// Package cosign implements timestamped Ed25519 checkpoint cosignatures
// (signature type 0x04), as specified by https://c2sp.org/tlog-cosignature
// and https://c2sp.org/signed-note.
package cosign

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// algorithm is the signed-note signature type identifier for
	// timestamped Ed25519 checkpoint cosignatures.
	algorithm = 0x04

	keyHashLen   = 4
	timestampLen = 8

	// SignatureLen is the length of the signature value (excluding the key
	// hash): an eight-byte big-endian timestamp followed by an Ed25519 signature.
	SignatureLen = timestampLen + ed25519.SignatureSize

	// CosignatureLen is the length of a complete cosignature, as encoded in a
	// signature line: the key hash followed by the signature value.
	CosignatureLen = keyHashLen + SignatureLen

	skeyPrefix = "PRIVATE+KEY+"
)

var (
	errBadName        = errors.New("cosign: invalid key name")
	errBadSignerKey   = errors.New("cosign: malformed signer key")
	errBadVerifierKey = errors.New("cosign: malformed verifier key")
	errBadCosignature = errors.New("cosign: malformed cosignature")
	errWrongKeyHash   = errors.New("cosign: cosignature was not produced by this key")
	errInvalidSig     = errors.New("cosign: invalid signature")
)

// isValidName reports whether name is a valid signed-note key name.
func isValidName(name string) bool {
	return name != "" && utf8.ValidString(name) && strings.IndexFunc(name, unicode.IsSpace) < 0 && !strings.Contains(name, "+")
}

func keyHash(name string, pubkey ed25519.PublicKey) uint32 {
	h := sha256.New()
	h.Write([]byte(name))
	h.Write([]byte{'\n', algorithm})
	h.Write(pubkey)
	return binary.BigEndian.Uint32(h.Sum(nil)[:keyHashLen])
}

// signedMessage returns the bytes that are signed to produce a cosignature of
// the given checkpoint at the given time.
func signedMessage(timestamp uint64, checkpoint []byte) []byte {
	msg := make([]byte, 0, 32+len(checkpoint))
	msg = append(msg, "cosignature/v1\ntime "...)
	msg = strconv.AppendUint(msg, timestamp, 10)
	msg = append(msg, '\n')
	msg = append(msg, checkpoint...)
	return msg
}

// A Signer produces cosignatures with an Ed25519 private key.
type Signer struct {
	name string
	hash uint32
	priv ed25519.PrivateKey
}

// GenerateKey generates a new cosigner key with the given name.  It returns
// the encoded signer (private) key and verifier (public) key.
func GenerateKey(rand io.Reader, name string) (skey, vkey string, err error) {
	if !isValidName(name) {
		return "", "", errBadName
	}
	pub, priv, err := ed25519.GenerateKey(rand)
	if err != nil {
		return "", "", err
	}
	hash := keyHash(name, pub)
	skey = fmt.Sprintf("%s%s+%08x+%s", skeyPrefix, name, hash, base64.StdEncoding.EncodeToString(append([]byte{algorithm}, priv.Seed()...)))
	vkey = fmt.Sprintf("%s+%08x+%s", name, hash, base64.StdEncoding.EncodeToString(append([]byte{algorithm}, pub...)))
	return skey, vkey, nil
}

// NewSigner constructs a Signer from an encoded signer key, as returned by
// GenerateKey.
func NewSigner(skey string) (*Signer, error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(skey), skeyPrefix)
	if !ok {
		return nil, errBadSignerKey
	}
	name, rest, _ := strings.Cut(rest, "+")
	hash16, key64, _ := strings.Cut(rest, "+")
	hash, err1 := strconv.ParseUint(hash16, 16, 32)
	key, err2 := base64.StdEncoding.DecodeString(key64)
	if len(hash16) != 8 || err1 != nil || err2 != nil || !isValidName(name) {
		return nil, errBadSignerKey
	}
	if len(key) != 1+ed25519.SeedSize || key[0] != algorithm {
		return nil, errBadSignerKey
	}
	priv := ed25519.NewKeyFromSeed(key[1:])
	if keyHash(name, priv.Public().(ed25519.PublicKey)) != uint32(hash) {
		return nil, errBadSignerKey
	}
	return &Signer{name: name, hash: uint32(hash), priv: priv}, nil
}

// Name returns the key name.
func (s *Signer) Name() string { return s.name }

// KeyHash returns the key hash (key ID).
func (s *Signer) KeyHash() uint32 { return s.hash }

// VerifierKey returns the encoded verifier key corresponding to this signer.
func (s *Signer) VerifierKey() string {
	pub := s.priv.Public().(ed25519.PublicKey)
	return fmt.Sprintf("%s+%08x+%s", s.name, s.hash, base64.StdEncoding.EncodeToString(append([]byte{algorithm}, pub...)))
}

// Cosign produces a cosignature of checkpoint (the text of a checkpoint note,
// including the final newline but not the blank line or signature lines) at
// the given time, which is truncated to the second.  The returned cosignature
// is CosignatureLen bytes long and is what gets base64-encoded in a
// signature line.
func (s *Signer) Cosign(checkpoint []byte, timestamp time.Time) []byte {
	ts := uint64(timestamp.Unix())
	cosig := make([]byte, 0, CosignatureLen)
	cosig = binary.BigEndian.AppendUint32(cosig, s.hash)
	cosig = binary.BigEndian.AppendUint64(cosig, ts)
	cosig = append(cosig, ed25519.Sign(s.priv, signedMessage(ts, checkpoint))...)
	return cosig
}

// A Verifier verifies cosignatures with an Ed25519 public key.  It satisfies
// the golang.org/x/mod/sumdb/note.Verifier interface.
type Verifier struct {
	name string
	hash uint32
	pub  ed25519.PublicKey
}

// NewVerifier constructs a Verifier from an encoded verifier key, as returned
// by GenerateKey or Signer.VerifierKey.
func NewVerifier(vkey string) (*Verifier, error) {
	name, rest, _ := strings.Cut(strings.TrimSpace(vkey), "+")
	hash16, key64, _ := strings.Cut(rest, "+")
	hash, err1 := strconv.ParseUint(hash16, 16, 32)
	key, err2 := base64.StdEncoding.DecodeString(key64)
	if len(hash16) != 8 || err1 != nil || err2 != nil || !isValidName(name) {
		return nil, errBadVerifierKey
	}
	if len(key) != 1+ed25519.PublicKeySize || key[0] != algorithm {
		return nil, errBadVerifierKey
	}
	pub := ed25519.PublicKey(key[1:])
	if keyHash(name, pub) != uint32(hash) {
		return nil, errBadVerifierKey
	}
	return &Verifier{name: name, hash: uint32(hash), pub: pub}, nil
}

// Name returns the key name.
func (v *Verifier) Name() string { return v.name }

// KeyHash returns the key hash (key ID).
func (v *Verifier) KeyHash() uint32 { return v.hash }

// Verify reports whether sig is a valid signature value (timestamp followed
// by Ed25519 signature, without the key hash) over the checkpoint text msg.
// This is the method required by golang.org/x/mod/sumdb/note.Verifier.
func (v *Verifier) Verify(msg, sig []byte) bool {
	_, err := v.verify(msg, sig)
	return err == nil
}

func (v *Verifier) verify(checkpoint, sig []byte) (time.Time, error) {
	if len(sig) != SignatureLen {
		return time.Time{}, errBadCosignature
	}
	ts := binary.BigEndian.Uint64(sig[:timestampLen])
	if !ed25519.Verify(v.pub, signedMessage(ts, checkpoint), sig[timestampLen:]) {
		return time.Time{}, errInvalidSig
	}
	return time.Unix(int64(ts), 0).UTC(), nil
}

// VerifyCosignature verifies a complete cosignature (key hash followed by
// signature value, as produced by Signer.Cosign) over the given checkpoint
// text, and returns the time at which the cosignature was produced.
func (v *Verifier) VerifyCosignature(checkpoint, cosignature []byte) (time.Time, error) {
	if len(cosignature) != CosignatureLen {
		return time.Time{}, errBadCosignature
	}
	if binary.BigEndian.Uint32(cosignature[:keyHashLen]) != v.hash {
		return time.Time{}, errWrongKeyHash
	}
	return v.verify(checkpoint, cosignature[keyHashLen:])
}

// FormatSignatureLine formats a signed-note signature line (including the
// trailing newline) for a cosignature produced by the key with the given name.
func FormatSignatureLine(name string, cosignature []byte) string {
	return "— " + name + " " + base64.StdEncoding.EncodeToString(cosignature) + "\n"
}
