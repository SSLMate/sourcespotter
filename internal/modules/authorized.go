package modules

import (
	"bufio"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"software.sslmate.com/src/sourcespotter"
)

const (
	hashPrefix = "h1:"
	hashLen    = 32
)

func parseHash(input string) ([]byte, error) {
	if !strings.HasPrefix(input, hashPrefix) {
		return nil, errors.New("unrecognized hash type")
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(input, hashPrefix))
	if err != nil {
		return nil, err
	}
	if len(b) != hashLen {
		return nil, errors.New("SHA-256 hash has wrong length")
	}
	return b, nil
}

func ReceiveAuthorized(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Ed25519   []byte
		GoSum     string
		Signature []byte
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, req.Body, 1000000))
	if err := dec.Decode(&body); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if dec.More() {
		http.Error(w, "Invalid JSON: trailing data", http.StatusBadRequest)
		return
	}
	if !ed25519.Verify(body.Ed25519, []byte(body.GoSum), body.Signature) {
		http.Error(w, "Permission Denied: signature validation failed", http.StatusForbidden)
		return
	}

	tx, err := sourcespotter.DB.BeginTx(req.Context(), nil)
	if err != nil {
		log.Print(err)
		http.Error(w, "Internal Database Error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	scanner := bufio.NewScanner(strings.NewReader(body.GoSum))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) != 3 {
			http.Error(w, fmt.Sprintf("Invalid go.sum line %d", lineNum), http.StatusBadRequest)
			return
		}
		module := fields[0]
		version := fields[1]
		hash, err := parseHash(fields[2])
		if err != nil {
			http.Error(w, fmt.Sprintf("Invalid hash on line %d: %v", lineNum, err), http.StatusBadRequest)
			return
		}
		columnName := "source_sha256"
		if s, ok := strings.CutSuffix(version, "/go.mod"); ok {
			columnName = "gomod_sha256"
			version = s
		}
		if _, err := tx.ExecContext(req.Context(), `INSERT INTO authorized_record (pubkey, module, version, `+columnName+`) VALUES ($1,$2,$3,$4) ON CONFLICT (pubkey,module,version) DO UPDATE SET `+columnName+`=excluded.`+columnName, body.Ed25519, module, version, hash); err != nil {
			log.Print(err)
			http.Error(w, "Internal Database Error", http.StatusInternalServerError)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		http.Error(w, "Invalid go.sum: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Print(err)
		http.Error(w, "Internal Database Error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func ServeAuthorized(w http.ResponseWriter, req *http.Request) {
	pubkeyParam := req.URL.Query().Get("ed25519")
	if pubkeyParam == "" {
		http.Error(w, "Missing ed25519 parameter", http.StatusBadRequest)
		return
	}
	pubkey, err := base64.StdEncoding.DecodeString(pubkeyParam)
	if err != nil {
		http.Error(w, "Invalid ed25519 parameter: invalid base64", http.StatusBadRequest)
		return
	}
	if len(pubkey) != ed25519.PublicKeySize {
		http.Error(w, "Invalid ed25519 parameter: wrong length", http.StatusBadRequest)
		return
	}

	rows, err := sourcespotter.DB.QueryContext(req.Context(), `SELECT module, version, source_sha256, gomod_sha256 FROM authorized_record WHERE pubkey = $1 ORDER BY module, version`, pubkey)
	if err != nil {
		log.Print(err)
		http.Error(w, "Internal Database Error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	for rows.Next() {
		var module string
		var version string
		var sourceHash []byte
		var gomodHash []byte
		if err := rows.Scan(&module, &version, &sourceHash, &gomodHash); err != nil {
			log.Print(err)
			return
		}
		if len(sourceHash) > 0 {
			fmt.Fprintf(w, "%s %s %s%s\n", module, version, hashPrefix, base64.StdEncoding.EncodeToString(sourceHash))
		}
		if len(gomodHash) > 0 {
			fmt.Fprintf(w, "%s %s/go.mod %s%s\n", module, version, hashPrefix, base64.StdEncoding.EncodeToString(gomodHash))
		}
	}
	if err := rows.Err(); err != nil {
		log.Print(err)
	}
}
