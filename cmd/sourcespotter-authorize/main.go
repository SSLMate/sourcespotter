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

// sourcespotter-authorize is a command for authorizing module versions in a local Git repo
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/mldsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/sync/errgroup"
	"software.sslmate.com/src/sourcespotter/gosum"
)

const (
	defaultDomain = "sourcespotter.com"
)

// algorithm names used in the private key file
const (
	mldsa44Algorithm = "mldsa44"
	mldsa65Algorithm = "mldsa65"
	mldsa87Algorithm = "mldsa87"
	ed25519Algorithm = "ed25519"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: sourcespotter-authorize [-keygen|-pubkey|-feed|-import|-export] [TAG...]")
	flag.PrintDefaults()
	os.Exit(2)
}

func main() {
	log.SetPrefix("sourcespotter-authorize: ")
	log.SetFlags(0)

	keygen := flag.Bool("keygen", false, "Generate a new ML-DSA-44 private key")
	pubkey := flag.Bool("pubkey", false, "Print the public key identifier used in feed URLs")
	feed := flag.Bool("feed", false, "Print the modules feed URL")
	doImport := flag.Bool("import", false, "Authorize the module versions in the go.sum file read from stdin")
	doExport := flag.Bool("export", false, "Write the currently-authorized module versions to stdout in go.sum format")
	flag.Usage = usage
	flag.Parse()

	modeCount := 0
	for _, enabled := range []bool{*keygen, *pubkey, *feed, *doImport, *doExport} {
		if enabled {
			modeCount++
		}
	}
	if modeCount > 1 {
		usage()
	}

	args := flag.Args()
	switch {
	case *keygen:
		if len(args) != 0 {
			usage()
		}
		if err := runKeygen(); err != nil {
			log.Fatal(err)
		}
	case *pubkey:
		if len(args) != 0 {
			usage()
		}
		if err := runPubkey(); err != nil {
			log.Fatal(err)
		}
	case *feed:
		if len(args) != 0 {
			usage()
		}
		if err := runFeed(); err != nil {
			log.Fatal(err)
		}
	case *doImport:
		if len(args) != 0 {
			usage()
		}
		if err := runImport(); err != nil {
			log.Fatal(err)
		}
	case *doExport:
		if len(args) != 0 {
			usage()
		}
		if err := runExport(); err != nil {
			log.Fatal(err)
		}
	default:
		if len(args) == 0 {
			usage()
		}
		if err := runAuthorize(args); err != nil {
			log.Fatal(err)
		}
	}
}

func runKeygen() error {
	priv, err := mldsa.GenerateKey(mldsa.MLDSA44())
	if err != nil {
		return err
	}

	keyPath, err := keyPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0777); err != nil {
		return err
	}

	content := fmt.Sprintf("%s\n%s\n", mldsa44Algorithm, base64.StdEncoding.EncodeToString(priv.Bytes()))
	file, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.WriteString(content); err != nil {
		return fmt.Errorf("error writing private key file %q: %w", keyPath, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("error writing private key file %q: %w", keyPath, err)
	}
	return nil
}

func runPubkey() error {
	priv, err := readPrivateKey()
	if err != nil {
		return err
	}
	_, pubkeyValue := priv.feedParam()
	fmt.Println(pubkeyValue)
	return nil
}

func runFeed() error {
	priv, err := readPrivateKey()
	if err != nil {
		return err
	}
	pubkeyParam, pubkeyValue := priv.feedParam()

	modulePath, err := modulePathFromGoEnv()
	if err != nil {
		return err
	}

	domain := sourcespotterDomain()
	feedURL := fmt.Sprintf(
		"https://feeds.api.%s/modules/versions.atom?module=%s&%s=%s",
		domain,
		url.QueryEscape(modulePath),
		pubkeyParam,
		url.QueryEscape(pubkeyValue),
	)
	fmt.Println(feedURL)
	return nil
}

func runAuthorize(tags []string) error {
	repoRoot, err := gitRoot()
	if err != nil {
		return err
	}
	priv, err := readPrivateKey()
	if err != nil {
		return err
	}

	goSumLines := make([]string, len(tags))
	group := errgroup.Group{}
	group.SetLimit(runtime.GOMAXPROCS(0))
	for i, tag := range tags {
		group.Go(func() error {
			var err error
			goSumLines[i], err = gosum.CreateFromGitTag(repoRoot, tag)
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}

	return authorize(priv, strings.Join(goSumLines, ""))
}

func runImport() error {
	priv, err := readPrivateKey()
	if err != nil {
		return err
	}
	goSum, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("error reading go.sum from stdin: %w", err)
	}
	return authorize(priv, string(goSum))
}

func runExport() error {
	priv, err := readPrivateKey()
	if err != nil {
		return err
	}
	pubkeyParam, pubkeyValue := priv.feedParam()

	endpoint := fmt.Sprintf(
		"https://v1.api.%s/modules/authorized?%s=%s",
		sourcespotterDomain(),
		pubkeyParam,
		url.QueryEscape(pubkeyValue),
	)

	resp, err := http.Get(endpoint)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		message := strings.TrimSpace(string(msg))
		if message == "" {
			message = resp.Status
		}
		return fmt.Errorf("export request failed: %s", message)
	}
	if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
		return fmt.Errorf("error writing go.sum to stdout: %w", err)
	}
	return nil
}

// authorize signs the given go.sum file and sends it to the authorization endpoint.
func authorize(priv *privateKey, goSum string) error {
	sig, err := priv.sign([]byte(goSum))
	if err != nil {
		return err
	}

	payload := struct {
		MLDSA     []byte `json:",omitzero"`
		Ed25519   []byte `json:",omitzero"`
		GoSum     string
		Signature []byte
	}{
		MLDSA:     priv.mldsaPublicKey(),
		Ed25519:   priv.ed25519PublicKey(),
		GoSum:     goSum,
		Signature: sig,
	}

	endpoint := fmt.Sprintf("https://v1.api.%s/modules/authorized", sourcespotterDomain())
	return postAuthorized(endpoint, payload)
}

func keyPath() (string, error) {
	if envPath := os.Getenv("SOURCESPOTTER_AUTHORIZE_KEY"); envPath != "" {
		return envPath, nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "sourcespotter-authorize", "private_key"), nil
}

func sourcespotterDomain() string {
	if domain := os.Getenv("SOURCESPOTTER_DOMAIN"); domain != "" {
		return domain
	}
	return defaultDomain
}

// privateKey is the key used to sign authorizations.  Exactly one of the fields
// is set.  -keygen only generates ML-DSA-44 keys, but a key file written by
// hand can use any ML-DSA parameter set, and ed25519 keys generated by older
// versions of this command remain supported.
type privateKey struct {
	mldsa   *mldsa.PrivateKey
	ed25519 ed25519.PrivateKey
}

func (priv *privateKey) sign(message []byte) ([]byte, error) {
	if priv.mldsa != nil {
		return priv.mldsa.Sign(nil, message, nil)
	}
	return ed25519.Sign(priv.ed25519, message), nil
}

func (priv *privateKey) mldsaPublicKey() []byte {
	if priv.mldsa == nil {
		return nil
	}
	return priv.mldsa.PublicKey().Bytes()
}

func (priv *privateKey) ed25519PublicKey() []byte {
	if priv.ed25519 == nil {
		return nil
	}
	return priv.ed25519.Public().(ed25519.PublicKey)
}

// feedParam returns the name and value of the query string parameter that
// identifies this key's public key in a feed URL.
func (priv *privateKey) feedParam() (string, string) {
	if priv.mldsa != nil {
		hash := sha256.Sum256(priv.mldsaPublicKey())
		return "mldsa", hex.EncodeToString(hash[:])
	}
	return "ed25519", base64.StdEncoding.EncodeToString(priv.ed25519PublicKey())
}

func readPrivateKey() (*privateKey, error) {
	keyPath, err := keyPath()
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(keyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("private key file %q not found: run 'sourcespotter-authorize -keygen' to generate it or set $SOURCESPOTTER_AUTHORIZE_KEY to a different path", keyPath)
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 {
		return nil, fmt.Errorf("invalid private key file %q: expected two lines", keyPath)
	}
	algorithm := strings.TrimSpace(lines[0])
	keyBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil {
		return nil, err
	}
	if params, ok := mldsaAlgorithm(algorithm); ok {
		key, err := mldsa.NewPrivateKey(params, keyBytes)
		if err != nil {
			return nil, fmt.Errorf("invalid private key file %q: %w", keyPath, err)
		}
		return &privateKey{mldsa: key}, nil
	}
	if algorithm == ed25519Algorithm {
		if len(keyBytes) != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("invalid private key file %q: invalid length", keyPath)
		}
		return &privateKey{ed25519: ed25519.PrivateKey(keyBytes)}, nil
	}
	return nil, fmt.Errorf("invalid private key file %q: unsupported algorithm %q", keyPath, algorithm)
}

// mldsaAlgorithm returns the ML-DSA parameter set named by an algorithm name
// from a private key file.
func mldsaAlgorithm(algorithm string) (mldsa.Parameters, bool) {
	switch algorithm {
	case mldsa44Algorithm:
		return mldsa.MLDSA44(), true
	case mldsa65Algorithm:
		return mldsa.MLDSA65(), true
	case mldsa87Algorithm:
		return mldsa.MLDSA87(), true
	}
	return mldsa.Parameters{}, false
}

func modulePathFromGoEnv() (string, error) {
	cmd := exec.Command("go", "env", "GOMOD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(output))
	if path == "" || path == os.DevNull {
		return "", errors.New("no go.mod found: run this command from within a Go module")
	}
	return modulePathFromFile(path)
}

func modulePathFromFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	mod, err := modfile.Parse(path, content, nil)
	if err != nil {
		return "", err
	}
	if mod.Module == nil {
		return "", errors.New("go.mod missing module directive")
	}
	return mod.Module.Mod.Path, nil
}

func gitRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", errors.New("unable to locate .git directory: you must run this from within a Git repository")
}

func postAuthorized(endpoint string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		msg, _ := io.ReadAll(resp.Body)
		message := strings.TrimSpace(string(msg))
		if message == "" {
			message = resp.Status
		}
		return fmt.Errorf("authorization request failed: %s", message)
	}
	return nil
}
