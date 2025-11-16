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

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

func getModfetch(w http.ResponseWriter, req *http.Request) error {
	ctx := req.Context()
	module := req.URL.Query().Get("module")
	if !strings.Contains(module, "@") {
		http.Error(w, "module does not contain @", 400)
		return nil
	}
	cmd := goCommand(ctx, "/", "go", "mod", "download", "-json", "--", module)
	cmd.Env = append(cmd.Env,
		"GOPROXY=direct",
		"GOSUMDB=off",
		"GOVCS=*:git",
	)
	cmd.Dir = "/"
	out, execErr := cmd.Output()
	var moduleObject struct {
		Path     string
		Version  string
		Error    string `json:",omitempty"`
		Info     any    `json:",omitempty"`
		GoMod    string `json:",omitempty"`
		Sum      string `json:",omitempty"`
		GoModSum string `json:",omitempty"`
	}
	if err := json.Unmarshal(out, &moduleObject); err != nil && execErr == nil {
		return fmt.Errorf("error unmarshalling JSON from 'go mod download': %w", err)
	} else if execErr != nil && moduleObject.Error == "" {
		return fmt.Errorf("error running 'go mod download': %w", execErr)
	}
	if infoPath, ok := moduleObject.Info.(string); ok && infoPath != "" {
		if content, err := os.ReadFile(infoPath); err == nil {
			moduleObject.Info = json.RawMessage(content)
		} else {
			moduleObject.Info = ""
		}
	}
	if moduleObject.GoMod != "" {
		if content, err := os.ReadFile(moduleObject.GoMod); err == nil {
			moduleObject.GoMod = string(content)
		} else {
			moduleObject.GoMod = ""
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	json.NewEncoder(w).Encode(moduleObject)
	return nil
}
