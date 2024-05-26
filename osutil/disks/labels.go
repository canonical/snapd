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
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ddkwork/golibrary/mylog"
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

var hexCode = regexp.MustCompile(`\\x[0-9a-f]{2}`)

// BlkIDDecodeLabel decodes a string such as a filesystem or partition label
// encoded by udev in BlkIDEncodeLabel for normal comparison, i.e.
// "BIOS\x20Boot" becomes "BIOS Boot"
func BlkIDDecodeLabel(in string) (string, error) {
	out := strings.Builder{}
	pos := 0
	for _, m := range hexCode.FindAllStringIndex(in, -1) {
		start := m[0]
		beforeMatch := in[pos:start]
		if i := strings.IndexRune(beforeMatch, '\\'); i >= 0 {
			return "", fmt.Errorf(`string is malformed, unparsable escape sequence at "%s"`, beforeMatch[i:])
		}
		out.WriteString(beforeMatch)
		hex := in[start+2 : start+4]
		n := mylog.Check2(strconv.ParseUint(hex, 16, 8))

		// This cannot really happen, since the regexp wouldn't match otherwise

		out.WriteRune(rune(n))
		pos = m[1]
	}
	remaining := in[pos:]
	if i := strings.IndexRune(remaining, '\\'); i >= 0 {
		return "", fmt.Errorf(`string is malformed, unparsable escape sequence at "%s"`, remaining[i:])
	}
	out.WriteString(remaining)
	return out.String(), nil
}
