// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package strutil

import (
	"unicode"
	"unicode/utf8"
)

// UnsafeString is a string that cannot be shown to the user without
// further processing, as it might contain control characters.
type UnsafeString string

// Clean any control characters off the UnsafeString.
func (un UnsafeString) Clean() string {
	return clean(string(un), false)
}

// UnsafeParagraph is a string that cannot be shown to the user without
// further processing, as it might contain control characters other than \n.
type UnsafeParagraph string

// Clean any control characters other than \n off the UnsafeParagraph.
func (un UnsafeParagraph) Clean() string {
	return clean(string(un), true)
}

func clean(uns string, paragraph bool) string {
	// taken with permission from github.com/chipaca/term/escapes
	out := make([]byte, 0, len(uns))
	var r rune
	var c byte
	var i, j, sz int
	for j < len(uns) {
		// 0x80 is utf8.RuneSelf (below that, bytes are runes)
		if uns[j] < 0x80 {
			c = uns[j]
			// 0x00..0x19 and 0x7f is the the first half of Cc
			// 0x20..0x7e is all of printable ASCII
			if (paragraph && c == '\n') || 0x20 <= c && c <= 0x7e {
				out = append(out, c)
			}
			j++
		} else {
			r, sz = utf8.DecodeRuneInString(uns[j:])
			if sz == 0 {
				// end of the line
				// (shouldn't happen given the loop guard)
				break
			}
			i = j
			j += sz
			// 0x80..0x9f is the second half of Cc
			if r != utf8.RuneError && r > 0x9f && !unicode.Is(ctrl, r) {
				out = append(out, uns[i:j]...)
			}
		}
	}

	return string(out)
}
