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
	"bytes"
	"testing"
)

func TestSTH(t *testing.T) {
	sthString := "go.sum database tree\n1262203\nsQ1Biyw3NQ7OBmLpfA5zZrs6xiB+o2ZjybBDj9cmnKA=\n\n\u2014 sum.golang.org Az3grpikEWo01N06qu0EoiC1BoYoyFuxaFTTMxfFiKnPadtWHsUDgXAUSfNZhEruQBzhzzIxYDroLaJwCZMVDXZRwAQ=\n"
	sth, err := ParseSTH([]byte(sthString), "sum.golang.org")
	if err != nil {
		t.Errorf("ParseSTH: error: %s", err)
		return
	}
	if sth.TreeSize != 1262203 {
		t.Errorf("ParseSTH: wrong tree size")
		return
	}
	if !bytes.Equal(sth.RootHash[:], []byte{0xb1, 0x0d, 0x41, 0x8b, 0x2c, 0x37, 0x35, 0x0e, 0xce, 0x06, 0x62, 0xe9, 0x7c, 0x0e, 0x73, 0x66, 0xbb, 0x3a, 0xc6, 0x20, 0x7e, 0xa3, 0x66, 0x63, 0xc9, 0xb0, 0x43, 0x8f, 0xd7, 0x26, 0x9c, 0xa0}) {
		t.Errorf("ParseSTH: wrong root hash")
		return
	}
	err = sth.Authenticate([]byte{0x01, 0xce, 0x33, 0x72, 0xd7, 0x5a, 0xd1, 0xee, 0x5e, 0xcd, 0xaf, 0x87, 0x27, 0x29, 0x3d, 0x4b, 0x11, 0x1d, 0x87, 0xeb, 0x37, 0x53, 0x1d, 0x7c, 0x86, 0xd4, 0xd3, 0x00, 0x3f, 0x0e, 0xb8, 0x09, 0xfc})
	if err != nil {
		t.Errorf("ParseSTH: authenticate failed (signature %x): %s", sth.Signature, err)
		return
	}
}
