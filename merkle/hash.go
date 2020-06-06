package merkle

import (
	"crypto/sha256"
)

type Hash []byte

func HashNothing() Hash {
	return sha256.New().Sum(nil)
}

func HashLeaf(leafBytes Hash) Hash {
	hasher := sha256.New()
	hasher.Write([]byte{0x00})
	hasher.Write(leafBytes)
	return hasher.Sum(nil)
}

func HashChildren(left, right Hash) Hash {
	hasher := sha256.New()
	hasher.Write([]byte{0x01})
	hasher.Write(left)
	hasher.Write(right)
	return hasher.Sum(nil)
}
