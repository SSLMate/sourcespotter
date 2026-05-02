// Copyright (C) 2023 Andrew Ayer
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

package goproxy

import (
	"fmt"
	"strings"
)

type Request interface {
	Path() string
}

type LatestRequest struct{}
type ListRequest struct{}
type InfoRequest struct {
	Version ModuleVersion
}
type ModRequest struct {
	Version ModuleVersion
}
type ZipRequest struct {
	Version ModuleVersion
}

func (LatestRequest) Path() string { return "@latest" }
func (ListRequest) Path() string   { return "@v/list" }
func (r InfoRequest) Path() string { return "@v/" + r.Version.Escaped() + ".info" }
func (r ModRequest) Path() string  { return "@v/" + r.Version.Escaped() + ".mod" }
func (r ZipRequest) Path() string  { return "@v/" + r.Version.Escaped() + ".zip" }

func ParseRequestPath(path string) (ModulePath, Request, error) {
	if strings.HasSuffix(path, "/@latest") {
		if modulePath, err := UnescapeModulePath(strings.TrimSuffix(path, "/@latest")); err == nil {
			return modulePath, LatestRequest{}, nil
		} else {
			return "", nil, fmt.Errorf("invalid module path: %w", err)
		}
	} else if strings.HasSuffix(path, "/@v/list") {
		if modulePath, err := UnescapeModulePath(strings.TrimSuffix(path, "/@v/list")); err == nil {
			return modulePath, ListRequest{}, nil
		} else {
			return "", nil, fmt.Errorf("invalid module path: %w", err)
		}
	} else if i := strings.LastIndex(path, "/@v/"); i != -1 {
		modulePath, err := UnescapeModulePath(path[:i])
		if err != nil {
			return "", nil, fmt.Errorf("invalid module path: %w", err)
		}
		filename := path[i+4:]
		if strings.HasSuffix(filename, ".info") {
			if version, err := UnescapeModuleVersion(strings.TrimSuffix(filename, ".info")); err == nil {
				return modulePath, InfoRequest{Version: version}, nil
			} else {
				return "", nil, fmt.Errorf("invalid module version: %w", err)
			}
		} else if strings.HasSuffix(filename, ".mod") {
			if version, err := UnescapeModuleVersion(strings.TrimSuffix(filename, ".mod")); err == nil {
				return modulePath, ModRequest{Version: version}, nil
			} else {
				return "", nil, fmt.Errorf("invalid module version: %w", err)
			}
		} else if strings.HasSuffix(filename, ".zip") {
			if version, err := UnescapeModuleVersion(strings.TrimSuffix(filename, ".zip")); err == nil {
				return modulePath, ZipRequest{Version: version}, nil
			} else {
				return "", nil, fmt.Errorf("invalid module version: %w", err)
			}
		}
	}
	return "", nil, fmt.Errorf("unrecognized proxy request")
}
