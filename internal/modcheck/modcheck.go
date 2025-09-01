// Copyright (C) 2025 Opsmate, Inc.
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

package modcheck

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"software.sslmate.com/src/sourcespotter"
	"src.agwa.name/go-dbutil"
)

type moduleRow struct {
	Version       string    `sql:"version"`
	SourceSHA256  []byte    `sql:"source_sha256"`
	GomodSHA256   []byte    `sql:"gomod_sha256"`
	ObservedAt    time.Time `sql:"observed_at"`
}

func ServeModcheck(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	module := req.URL.Query().Get("module")
	if module == "" {
		http.Error(w, "module parameter is required", http.StatusBadRequest)
		return
	}

	// Load the rows from the record table
	var rows []moduleRow
	if err := dbutil.QueryAll(ctx, sourcespotter.DB, &rows, `SELECT version, source_sha256, gomod_sha256, observed_at FROM record WHERE module = $1`, module); err != nil {
		log.Printf("ServeModcheck: error querying record for module %q: %s", module, err)
		http.Error(w, "Internal Database Error", 500)
		return
	}

	// Create a temporary directory and defer its complete removal
	tempDir, err := os.MkdirTemp("", "modcheck-")
	if err != nil {
		log.Printf("ServeModcheck: error creating temp directory: %s", err)
		http.Error(w, "Internal Server Error", 500)
		return
	}
	defer os.RemoveAll(tempDir)

	// Send response headers
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Create a JSON encoder for writing to the response body
	encoder := json.NewEncoder(w)
	
	// Create a mutex to protect concurrent writes to the response
	var mu sync.Mutex

	// Create an errgroup
	g, ctx := errgroup.WithContext(ctx)
	
	// For every row: execute the following in the errgroup
	for _, row := range rows {
		row := row // capture loop variable
		g.Go(func() error {
			// Exec the command `go mod download -json module@version` with environment variables
			cmd := exec.CommandContext(ctx, "go", "mod", "download", "-json", fmt.Sprintf("%s@%s", module, row.Version))
			cmd.Env = append(os.Environ(),
				"GOPROXY=direct",
				"GOPATH="+tempDir,
				"GOSUMDB=off",
			)
			
			stdout, err := cmd.Output()
			if err != nil {
				// Log the error but don't fail the entire operation
				log.Printf("ServeModcheck: error running go mod download for %s@%s: %s", module, row.Version, err)
				return nil
			}

			// Parse the stdout (which is json) into a map[string]any
			var result map[string]any
			if err := json.Unmarshal(stdout, &result); err != nil {
				log.Printf("ServeModcheck: error parsing JSON for %s@%s: %s", module, row.Version, err)
				return nil
			}

			// Write the map to the json encoder and flush the response body. Lock a mutex while doing this
			mu.Lock()
			defer mu.Unlock()
			
			if err := encoder.Encode(result); err != nil {
				log.Printf("ServeModcheck: error encoding JSON for %s@%s: %s", module, row.Version, err)
				return err
			}
			
			// Flush the response
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			
			return nil
		})
	}

	// Wait for the errgroup to finish
	if err := g.Wait(); err != nil {
		log.Printf("ServeModcheck: errgroup error: %s", err)
	}
}