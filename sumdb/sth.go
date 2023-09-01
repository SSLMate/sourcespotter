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
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"

	"software.sslmate.com/src/certspotter/merkletree"
)

const (
	sthPreamble    = "go.sum database tree"
	keytypeEd25519 = 0x01
)

type STH struct {
	TreeSize  uint64
	RootHash  merkletree.Hash
	Signature []byte
}

func chompSTHLine(input []byte) ([]byte, []byte) {
	newline := bytes.IndexByte(input, '\n')
	if newline == -1 {
		return nil, nil
	}
	return input[:newline], input[newline+1:]
}

func ParseSTH(input []byte, address string) (*STH, error) {
	preamble, input := chompSTHLine(input)
	sizeLine, input := chompSTHLine(input)
	hashLine, input := chompSTHLine(input)
	blankLine, input := chompSTHLine(input)
	if !bytes.Equal(preamble, []byte(sthPreamble)) {
		return nil, errors.New("doesn't look like an STH")
	}
	treeSize, err := strconv.ParseUint(string(sizeLine), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("malformed tree size: %w", err)
	}
	rootHash, err := base64.StdEncoding.DecodeString(string(hashLine))
	if err != nil {
		return nil, fmt.Errorf("malformed root hash: %w", err)
	}
	if len(rootHash) != merkletree.HashLen {
		return nil, fmt.Errorf("root hash has wrong length (should be %d bytes long, not %d)", merkletree.HashLen, len(rootHash))
	}
	if len(blankLine) != 0 {
		return nil, errors.New("missing blank line at end of STH")
	}
	var signature []byte
	signaturePrefix := []byte("\u2014 " + address + " ")
	for {
		var signatureLine []byte
		signatureLine, input = chompSTHLine(input)
		if signatureLine == nil {
			break
		}
		if bytes.HasPrefix(signatureLine, signaturePrefix) {
			signature = bytes.TrimPrefix(signatureLine, signaturePrefix)
			break
		}
	}
	if signature == nil {
		return nil, fmt.Errorf("doesn't have a signature from %s", address)
	}
	signature, err = base64.StdEncoding.DecodeString(string(signature))
	if err != nil {
		return nil, fmt.Errorf("malformed signature: %w", err)
	}
	return &STH{
		TreeSize:  treeSize,
		RootHash:  (merkletree.Hash)(rootHash),
		Signature: signature,
	}, nil
}

func (sth *STH) formatMessage() string {
	return fmt.Sprintf("go.sum database tree\n%d\n%s\n", sth.TreeSize, sth.RootHash.Base64String())
}

func (sth *STH) Authenticate(key []byte) error {
	if len(key) == 0 {
		return errors.New("key is too short")
	}
	keyType, keyData := key[0], key[1:]
	if len(sth.Signature) < 4 {
		return errors.New("signature is too short")
	}
	signature := sth.Signature[4:] // first four bytes are pointless key hash
	input := []byte(sth.formatMessage())

	switch keyType {
	case keytypeEd25519:
		return authenticateEd25519(keyData, input, signature)
	default:
		return fmt.Errorf("unsupported key type %x", keyType)
	}
}
func authenticateEd25519(key []byte, input []byte, signature []byte) error {
	if !ed25519.Verify(key, input, signature) {
		return errors.New("signature is invalid")
	}
	return nil
}

func ParseAndAuthenticateSTH(input []byte, address string, key []byte) (*STH, error) {
	sth, err := ParseSTH(input, address)
	if err != nil {
		return nil, fmt.Errorf("error parsing STH: %w", err)
	}
	if err := sth.Authenticate(key); err != nil {
		return nil, fmt.Errorf("error authenticating STH: %w", err)
	}
	return sth, nil
}

func (sth *STH) Format(address string) string {
	return fmt.Sprintf("%s\n\u2014 %s %s\n", sth.formatMessage(), address, base64.StdEncoding.EncodeToString(sth.Signature))
}
