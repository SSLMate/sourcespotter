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
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

type ModuleVersion string // if not empty, holds a valid module version

func (version ModuleVersion) IsEmpty() bool {
	return version == ""
}

func (version ModuleVersion) IsSet() bool {
	return version != ""
}

func (version ModuleVersion) String() string {
	return string(version)
}

func (version ModuleVersion) Escaped() string {
	escaped, err := module.EscapeVersion(string(version))
	if err != nil {
		panic("goproxy.ModuleVersion contains an invalid version that could not be escaped: " + err.Error())
	}
	return escaped
}

func (version *ModuleVersion) UnmarshalText(text []byte) error {
	str := string(text)
	if _, err := module.EscapeVersion(str); err != nil {
		return err
	}
	*version = ModuleVersion(str)
	return nil
}

func UnescapeModuleVersion(escaped string) (ModuleVersion, error) {
	str, err := module.UnescapeVersion(escaped)
	return ModuleVersion(str), err
}

func MakeModuleVersion(str string) (ModuleVersion, error) {
	if _, err := module.EscapeVersion(str); err != nil {
		return "", err
	}
	return ModuleVersion(str), nil
}

func (version ModuleVersion) Compare(other ModuleVersion) int {
	return semver.Compare(string(version), string(other))
}
