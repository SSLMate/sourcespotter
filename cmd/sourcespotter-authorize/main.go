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
	"crypto/rand"
	"encoding/base64"
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

func usage() {
	fmt.Fprintln(os.Stderr, "usage: sourcespotter-authorize [-keygen|-pubkey|-feed] [TAG...]")
	flag.PrintDefaults()
	os.Exit(2)
}

func main() {
	log.SetPrefix("sourcespotter-authorize: ")
	log.SetFlags(0)

	keygen := flag.Bool("keygen", false, "Generate a new Ed25519 private key")
	pubkey := flag.Bool("pubkey", false, "Print the Ed25519 public key in base64")
	feed := flag.Bool("feed", false, "Print the modules feed URL")
	flag.Usage = usage
	flag.Parse()

	modeCount := 0
	for _, enabled := range []bool{*keygen, *pubkey, *feed} {
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
	_, priv, err := ed25519.GenerateKey(rand.Reader)
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

	content := fmt.Sprintf("ed25519\n%s\n", base64.StdEncoding.EncodeToString(priv))
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
	pub := priv.Public().(ed25519.PublicKey)
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
	return nil
}

func runFeed() error {
	priv, err := readPrivateKey()
	if err != nil {
		return err
	}
	pub := priv.Public().(ed25519.PublicKey)
	pub64 := base64.StdEncoding.EncodeToString(pub)

	modulePath, err := modulePathFromGoEnv()
	if err != nil {
		return err
	}

	domain := sourcespotterDomain()
	feedURL := fmt.Sprintf(
		"https://feeds.api.%s/modules/versions.atom?module=%s&ed25519=%s",
		domain,
		url.QueryEscape(modulePath),
		url.QueryEscape(pub64),
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
	pub := priv.Public().(ed25519.PublicKey)

	domain := sourcespotterDomain()
	endpoint := fmt.Sprintf("https://v1.api.%s/modules/authorized", domain)

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

	goSum := strings.Join(goSumLines, "")
	sig := ed25519.Sign(priv, []byte(goSum))

	payload := struct {
		Ed25519   []byte
		GoSum     string
		Signature []byte
	}{
		Ed25519:   pub,
		GoSum:     goSum,
		Signature: sig,
	}
	if err := postAuthorized(endpoint, payload); err != nil {
		return err
	}
	return nil
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

func readPrivateKey() (ed25519.PrivateKey, error) {
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
	if strings.TrimSpace(lines[0]) != "ed25519" {
		return nil, fmt.Errorf("invalid private key file %q: unsupported algorithm", keyPath)
	}
	keyBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil {
		return nil, err
	}
	if len(keyBytes) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid private key file %q: invalid length", keyPath)
	}
	return ed25519.PrivateKey(keyBytes), nil
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
