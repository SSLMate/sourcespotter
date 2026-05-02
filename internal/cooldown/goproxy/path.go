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
)

type ModulePath string // if not empty, holds a valid module path

func (path ModulePath) IsEmpty() bool {
	return path == ""
}

func (path ModulePath) IsSet() bool {
	return path != ""
}

func (path ModulePath) String() string {
	return string(path)
}

func (path ModulePath) Escaped() string {
	escaped, err := module.EscapePath(string(path))
	if err != nil {
		panic("goproxy.ModulePath contains an invalid path that could not be escaped: " + err.Error())
	}
	return escaped
}

func (path *ModulePath) UnmarshalText(text []byte) error {
	str := string(text)
	if err := module.CheckPath(str); err != nil {
		return err
	}
	*path = ModulePath(str)
	return nil
}

func UnescapeModulePath(escaped string) (ModulePath, error) {
	str, err := module.UnescapePath(escaped)
	return ModulePath(str), err
}

func MakeModulePath(str string) (ModulePath, error) {
	if err := module.CheckPath(str); err != nil {
		return "", err
	}
	return ModulePath(str), nil
}
