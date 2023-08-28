package gosum

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"software.sslmate.com/src/certspotter/merkletree"
)

const (
	sha256Len    = 32
	sha256Prefix = "h1:"
)

type Record struct {
	Module       string
	Version      string
	SourceSHA256 []byte
	GomodSHA256  []byte
}

func parseRecordHash(input string) ([]byte, error) {
	if !strings.HasPrefix(input, sha256Prefix) {
		return nil, errors.New("Unrecognized hash type")
	}
	input = strings.TrimPrefix(input, sha256Prefix)
	hash, err := base64.StdEncoding.DecodeString(input)
	if err != nil {
		return nil, err
	}
	if len(hash) != sha256Len {
		return nil, errors.New("SHA-256 hash has wrong length")
	}
	return hash, nil
}

func parseRecordLine(input []byte) ([][]byte, []byte) {
	newline := bytes.IndexByte(input, '\n')
	if newline == -1 {
		return nil, nil
	}
	line, rest := input[:newline], input[newline+1:]
	return bytes.Split(line, []byte{' '}), rest
}

func ParseRecord(input []byte) (*Record, error) {
	// See https://golang.org/cmd/go/#hdr-Module_authentication_using_go_sum

	sourceLine, input := parseRecordLine(input)
	gomodLine, input := parseRecordLine(input)
	if sourceLine == nil || gomodLine == nil {
		return nil, errors.New("Premature end of go.sum record")
	}
	if len(input) > 0 {
		return nil, errors.New("Garbage at end of go.sum record")
	}
	if len(sourceLine) != 3 || len(gomodLine) != 3 {
		return nil, errors.New("go.sum line does not have exactly three fields")
	}

	module := string(sourceLine[0])
	version := string(sourceLine[1])
	sourceSHA256, err := parseRecordHash(string(sourceLine[2]))
	if err != nil {
		return nil, fmt.Errorf("go.sum line contains invalid hash: %w", err)
	}
	if string(gomodLine[0]) != module || string(gomodLine[1]) != version+"/go.mod" {
		return nil, errors.New("go.sum source line does not match go.mod line")
	}
	gomodSHA256, err := parseRecordHash(string(gomodLine[2]))
	if err != nil {
		return nil, fmt.Errorf("go.sum line contains invalid hash: %w", err)
	}

	return &Record{
		Module:       module,
		Version:      version,
		SourceSHA256: sourceSHA256,
		GomodSHA256:  gomodSHA256,
	}, nil
}

func (record *Record) formatSourceLine() string {
	return fmt.Sprintf("%s %s h1:%s\n", record.Module, record.Version, base64.StdEncoding.EncodeToString(record.SourceSHA256))
}

func (record *Record) formatGomodLine() string {
	return fmt.Sprintf("%s %s/go.mod h1:%s\n", record.Module, record.Version, base64.StdEncoding.EncodeToString(record.GomodSHA256))
}

func (record *Record) Format() []byte {
	return []byte(record.formatSourceLine() + record.formatGomodLine())
}

func (record *Record) Hash() merkletree.Hash {
	return merkletree.HashLeaf(record.Format())
}
