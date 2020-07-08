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

package systemd

import (
	"fmt"
	"path/filepath"
	"strings"
)

const asciiAlphanumerics = `abcdefghijklmnopqrstuvwxyz0123456789`

// EscapePath follows the same logic as systemd-escape(1) when called
// with --path. This is implemented without calling systemd-escape and as such
// is suitable for use in the initramfs where systemd-escape is not available.
// See "STRING ESCAPING FOR INCLUSION IN UNIT NAMES" in systemd.unit(5) for a
// detailed explanation of the algorithm followed.
func EscapePath(str string) string {
	if len(str) == 0 {
		return ""
	}

	clean := filepath.Clean(str)
	// easy case first, "/" becomes "-"
	if clean == "/" {
		return "-"
	}

	// if we have a leading "/" it is dropped
	if strings.HasPrefix(clean, "/") {
		clean = clean[1:]
	}

	var builder strings.Builder
	for _, b := range []byte(clean) {
		// this is not the best handling of UTF-8 characters, but this is what
		// systemd does and is reversible back to the same bag of bytes, so meh
		r := rune(b)
		switch {
		case r == '/':
			builder.WriteRune('-')
		case r == '_':
			builder.WriteRune('_')
		case strings.ContainsRune(asciiAlphanumerics, r):
			builder.WriteRune(r)
		default:
			// escape the byte value of the byte C-style with a \x
			// the slice of bytes is needed to make go pad bytes less than 16
			// display with a prefixed 0 the same way that systemd-escape(1)
			// does
			builder.WriteString(fmt.Sprintf(`\x%2x`, []byte{b}))
		}
	}

	return builder.String()
}
