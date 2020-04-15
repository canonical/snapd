// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package bootloadertest

import (
	"bytes"
	"encoding/binary"
	"unicode/utf16"
)

// UTF16Bytes converts the given string into its UTF16
// encoding. Convenient for use together with efi.MockVars.
func UTF16Bytes(s string) []byte {
	r16 := utf16.Encode(bytes.Runes([]byte(s)))
	b := make([]byte, (len(r16)+1)*2)
	i := 0
	for _, r := range r16 {
		binary.LittleEndian.PutUint16(b[i:], r)
		i += 2
	}
	// zero termination
	binary.LittleEndian.PutUint16(b[i:], 0)
	return b
}
