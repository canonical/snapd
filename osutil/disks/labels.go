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

package disks

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// BlkIDEncodeLabel encodes a name for use as a partition or filesystem
// label symlink by udev. The result matches the output of blkid_encode_string()
// from libblkid.
func BlkIDEncodeLabel(in string) string {
	const allowed = `#+-.:=@_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789`

	buf := &bytes.Buffer{}

	for _, r := range in {
		switch {
		case utf8.RuneLen(r) > 1:
			buf.WriteRune(r)
		case !strings.ContainsRune(allowed, r):
			fmt.Fprintf(buf, `\x%02x`, r)
		default:
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

type blkIdDecodeState int

const (
	stNormal blkIdDecodeState = iota
	stSlashEscape
	stSlashEscapeX
	stSlashEscapeXNum
)

// BlkIDDecodeLabel decodes a string such as a filesystem or partition label
// encoded by udev in BlkIDEncodeLabel for normal comparison, i.e.
// "BIOS\x20Boot" becomes "BIOS Boot"
func BlkIDDecodeLabel(in string) (string, error) {
	out := strings.Builder{}
	escapedHexDigits := []rune{}
	st := stNormal
	for _, r := range in {
		switch st {
		case stNormal:
			// check if this char is the beginning of an escape sequence
			if r == '\\' {
				st = stSlashEscape
				continue
			}
			// otherwise write it
			out.WriteRune(r)
		case stSlashEscape:
			// next char to check is 'x'
			if r == 'x' {
				st = stSlashEscapeX
				continue
			}
			// otherwise it's a format error, "\" is not in the set of
			// characters allowed, so if we see one that is not followed by an
			// x, then the string is malformed and can't be decoded
			return "", fmt.Errorf("string is malformed, unexpected '\\' character not part of a valid escape sequence")
		case stSlashEscapeX:
			// now we expect exactly two hex digits, since the encoding would
			// have written valid multi-byte runes that are UTF8 directly
			// without escaping, the only possible escaped runes are those which
			// are one byte and not in the allowed set

			// TODO: though can one have multi-byte runes that are not UTF8
			// encodable? it seems the only possibilities are runes that are
			// either in the surrogate range or that are larger than the maximum
			// rune value - for now we will just ignore those

			if strings.ContainsRune(`0123456789abcedf`, r) {
				escapedHexDigits = append(escapedHexDigits, r)
				st = stSlashEscapeXNum
				continue
			}
			return "", fmt.Errorf("string is malformed, unexpected %q character not part of a valid escape sequence", r)
		case stSlashEscapeXNum:
			// got one digit, make sure we get a second digit
			if strings.ContainsRune(`0123456789abcedf`, r) {
				escapedHexDigits = append(escapedHexDigits, r)

				// the escapedHexDigits can now be decoded and written out
				v, err := strconv.ParseUint(string(escapedHexDigits), 16, 8)
				if err != nil {
					// should be logically impossible, we ensured that only
					// rune digits in the hexadecimal range above were put into this rune
					// buffer
					return "", fmt.Errorf("internal error, unable to parse escape sequence: %v", err)
				}
				escapedHexDigits = []rune{}
				out.WriteRune(rune(v))
				st = stNormal
				continue
			}
			return "", fmt.Errorf("string is malformed, unexpected %q character not part of a valid escape sequence", r)
		default:
			return "", fmt.Errorf("internal error, unexpected parsing state")
		}
	}

	// check that we had a valid end state
	switch st {
	case stNormal:
		return out.String(), nil
	case stSlashEscape, stSlashEscapeX, stSlashEscapeXNum:
		return "", fmt.Errorf("string is malformed, unfinished escape sequence")
	default:
		return "", fmt.Errorf("internal error, unexpected parsing state")
	}
}
