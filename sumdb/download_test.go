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

package sumdb

import (
	"testing"
)

func TestFormatTileIndex(t *testing.T) {
	tests := []struct {
		in  uint64
		out string
	}{
		{0, "000"},
		{1, "001"},
		{12, "012"},
		{105, "105"},
		{1000, "x001/000"},
		{1050, "x001/050"},
		{52123, "x052/123"},
		{999001, "x999/001"},
		{1999001, "x001/x999/001"},
		{15999001, "x015/x999/001"},
	}
	for i, test := range tests {
		result := formatTileIndex(test.in)
		if result != test.out {
			t.Errorf("#%d: formatTileIndex(%q) = %q, want %q", i, test.in, result, test.out)
		}
	}
}
