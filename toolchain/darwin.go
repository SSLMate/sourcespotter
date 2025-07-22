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
//
// Copyright 2009 The Go Authors.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
//
//    * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//    * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//    * Neither the name of Google LLC nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

package toolchain

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"strings"
)

// StripDarwinSig parses data as a Mach-O executable, strips the macOS code signature from it,
// and returns the resulting Mach-O executable. It edits data directly, in addition to returning
// a shortened version.
// If data is not a Mach-O executable, StripDarwinSig silently returns it unaltered.
func StripDarwinSig(name string, data []byte) ([]byte, error) {
	// Binaries only expected in bin and pkg/tool.
	// This is an archive path, not a host file system path, so always forward slash.
	if !strings.Contains(name, "/bin/") && !strings.Contains(name, "/pkg/tool/") {
		return data, nil
	}
	// Check 64-bit Mach-O magic before trying to parse, to keep log quiet.
	if len(data) < 4 || string(data[:4]) != "\xcf\xfa\xed\xfe" {
		return data, nil
	}

	h, err := macho.NewFile(bytes.NewReader(data))
	if err != nil {
		return data, fmt.Errorf("macho %s: %v", name, err)
	}
	if len(h.Loads) < 4 {
		return data, fmt.Errorf("macho %s: too few loads", name)
	}

	// at returns the uint32 at the given data offset.
	// If the offset is out of range, at returns 0.
	le := binary.LittleEndian
	at := func(off int) uint32 {
		if off < 0 || off+4 < 0 || off+4 > len(data) {
			return 0
		}
		return le.Uint32(data[off : off+4])
	}

	// LC_CODE_SIGNATURE must be the last load.
	raw := h.Loads[len(h.Loads)-1].Raw()
	const LC_CODE_SIGNATURE = 0x1d
	if len(raw) != 16 || le.Uint32(raw[0:]) != LC_CODE_SIGNATURE || le.Uint32(raw[4:]) != 16 {
		// OK not to have a signature. No logging.
		return data, nil
	}
	sigOff := le.Uint32(raw[8:])
	sigSize := le.Uint32(raw[12:])
	if int64(sigOff) >= int64(len(data)) {
		return data, fmt.Errorf("macho %s: invalid signature", name)
	}

	// Find __LINKEDIT segment (3rd or 4th load, usually).
	// Each load command has its size as the second uint32 of the command.
	// We maintain the offset in the file as we walk, since we need to edit
	// the loads later.
	off := 32
	load := 0
	for {
		if load >= len(h.Loads) {
			return data, fmt.Errorf("macho %s: cannot find __LINKEDIT", name)
		}
		lc64, ok := h.Loads[load].(*macho.Segment)
		if ok && lc64.Name == "__LINKEDIT" {
			break
		}
		off += int(at(off + 4))
		load++
	}
	if at(off) != uint32(macho.LoadCmdSegment64) {
		return data, fmt.Errorf("macho %s: confused finding __LINKEDIT", name)
	}
	linkOff := off + 4 + 4 + 16 + 8 // skip cmd, len, name, addr
	if linkOff < 0 || linkOff+32 < 0 || linkOff+32 > len(data) {
		return data, fmt.Errorf("macho %s: confused finding __LINKEDIT", name)
	}
	for ; load < len(h.Loads)-1; load++ {
		off += int(at(off + 4))
	}
	if off < 0 || off+16 < 0 || off+16 > len(data) {
		return data, fmt.Errorf("macho %s: confused finding signature load", name)
	}

	// Point of no return: edit data to strip signature.

	// Delete LC_CODE_SIGNATURE entry in load table
	le.PutUint32(data[16:], at(16)-1)  // ncmd--
	le.PutUint32(data[20:], at(20)-16) // cmdsz -= 16
	copy(data[off:], make([]byte, 16)) // clear LC_CODE_SIGNATURE

	// Update __LINKEDIT file and memory size to not include signature.
	//	filesz -= sigSize
	//	memsz = filesz
	// We can't do memsz -= sigSize because the Apple signer rounds memsz
	// to a page boundary. Go always sets memsz = filesz (unrounded).
	fileSize := le.Uint64(data[linkOff+16:]) - uint64(sigSize)
	le.PutUint64(data[linkOff:], fileSize)    // memsz
	le.PutUint64(data[linkOff+16:], fileSize) // filesize

	// Remove signature bytes at end of file.
	data = data[:sigOff]

	return data, nil
}
