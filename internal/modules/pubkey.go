package modules

import (
	"crypto/ed25519"
	"crypto/mldsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
)

// Public keys are stored in the database in a type-prefixed form: an ed25519
// public key is stored as pubkeyTypeEd25519 followed by the 32 byte key itself,
// and an ML-DSA public key is stored as pubkeyTypeMLDSA followed by the SHA-256
// hash of the key, which is far too large to store directly.
const (
	pubkeyTypeEd25519 = 0x00
	pubkeyTypeMLDSA   = 0x01
)

func ed25519DBKey(pubkey ed25519.PublicKey) []byte {
	return append([]byte{pubkeyTypeEd25519}, pubkey...)
}

func mldsaDBKey(pubkeyHash []byte) []byte {
	return append([]byte{pubkeyTypeMLDSA}, pubkeyHash...)
}

// mldsaParameters returns the ML-DSA parameter set whose public keys are
// publicKeySize bytes long.
func mldsaParameters(publicKeySize int) (mldsa.Parameters, error) {
	for _, params := range []mldsa.Parameters{mldsa.MLDSA44(), mldsa.MLDSA65(), mldsa.MLDSA87()} {
		if publicKeySize == params.PublicKeySize() {
			return params, nil
		}
	}
	return mldsa.Parameters{}, errors.New("wrong length")
}

// parsePubkeyParam returns the database representation of the public key named
// by the mldsa or ed25519 query parameter.  It returns nil if neither parameter
// is present.
func parsePubkeyParam(query url.Values) ([]byte, error) {
	mldsaParam := query.Get("mldsa")
	ed25519Param := query.Get("ed25519")
	switch {
	case mldsaParam != "" && ed25519Param != "":
		return nil, errors.New("only one of the mldsa and ed25519 parameters may be specified")
	case mldsaParam != "":
		hash, err := hex.DecodeString(mldsaParam)
		if err != nil {
			return nil, errors.New("invalid mldsa parameter: invalid hex")
		}
		if len(hash) != sha256.Size {
			return nil, errors.New("invalid mldsa parameter: SHA-256 hash has wrong length")
		}
		return mldsaDBKey(hash), nil
	case ed25519Param != "":
		pubkey, err := base64.StdEncoding.DecodeString(ed25519Param)
		if err != nil {
			return nil, errors.New("invalid ed25519 parameter: invalid base64")
		}
		if len(pubkey) != ed25519.PublicKeySize {
			return nil, errors.New("invalid ed25519 parameter: wrong length")
		}
		return ed25519DBKey(pubkey), nil
	}
	return nil, nil
}

// parseRequestPubkey parses the ML-DSA or ed25519 public key sent in an
// authorization request, returning the key's database representation and a
// function for verifying signatures made by the corresponding private key.
func parseRequestPubkey(mldsaPubkey []byte, ed25519Pubkey []byte) ([]byte, func(message, signature []byte) bool, error) {
	switch {
	case len(mldsaPubkey) > 0 && len(ed25519Pubkey) > 0:
		return nil, nil, errors.New("only one of the MLDSA and Ed25519 fields may be specified")
	case len(mldsaPubkey) > 0:
		params, err := mldsaParameters(len(mldsaPubkey))
		if err != nil {
			return nil, nil, fmt.Errorf("invalid ML-DSA public key: %w", err)
		}
		pubkey, err := mldsa.NewPublicKey(params, mldsaPubkey)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid ML-DSA public key: %w", err)
		}
		hash := sha256.Sum256(mldsaPubkey)
		verify := func(message, signature []byte) bool {
			return mldsa.Verify(pubkey, message, signature, nil) == nil
		}
		return mldsaDBKey(hash[:]), verify, nil
	case len(ed25519Pubkey) > 0:
		if len(ed25519Pubkey) != ed25519.PublicKeySize {
			return nil, nil, errors.New("invalid ed25519 public key: wrong length")
		}
		verify := func(message, signature []byte) bool {
			return ed25519.Verify(ed25519Pubkey, message, signature)
		}
		return ed25519DBKey(ed25519Pubkey), verify, nil
	}
	return nil, nil, errors.New("one of the MLDSA and Ed25519 fields is required")
}
