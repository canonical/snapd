// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
)

const allowed = `:_.abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789`

// EscapeUnitNamePath works like systemd-escape --path
// FIXME: we could use github.com/coreos/go-systemd/unit/escape.go and EscapePath
// from it. But that's not in the archive and it won't work with go1.3
func EscapeUnitNamePath(in string) string {
	// "" is the same as "/" which is escaped to "-"
	// the filepath.Clean will turn "" into "." and make this incorrect
	if len(in) == 0 {
		return "-"
	}
	buf := bytes.NewBuffer(nil)

	// clean and trim leading/trailing "/"
	in = filepath.Clean(in)
	in = strings.Trim(in, "/")

	// empty strings is "/"
	if len(in) == 0 {
		in = "/"
	}
	// leading "." is special
	if in[0] == '.' {
		fmt.Fprintf(buf, `\x%x`, in[0])
		in = in[1:]
	}

	// replace all special chars
	for i := 0; i < len(in); i++ {
		c := in[i]
		if c == '/' {
			buf.WriteByte('-')
		} else if strings.IndexByte(allowed, c) >= 0 {
			buf.WriteByte(c)
		} else {
			fmt.Fprintf(buf, `\x%x`, []byte{in[i]})
		}
	}

	return buf.String()
}

// UnitNameFromSecurityTag converts a security tag to a unit name. It also
// verifies that no unhandled characters are present in the security tag. Valid
// characters are: a-z, A-Z, 0-9, '_', '-', '.' and '+'. All characters are
// passed through, except for the '+' character, which is converted to '\x2b'.
//
// Note that this is not the same as systemd-escape, since systemd-escape
// escapes the '-' character. Due to historical reasons, snapd uses the '-'
// character in unit names. Note that these are still valid unit names, since
// '-' is used by systemd-escape to represent the '/' character.
//
// To allow us to correctly convert between security tags and unit names (and to
// maintain snapd's usage of '-' in unit names), this implementation only
// escapes the '+' character, which was introduced with snap components.
//
// Examples of conversion:
//   - "snap.name.app" -> "snap.name.app"
//   - "snap.name+comp.hook.install" -> "snap.name\x2bcomp.hook.install"
func UnitNameFromSecurityTag(tag string) (string, error) {
	var builder strings.Builder
	for _, c := range tag {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-', c == '.':
			builder.WriteRune(c)
		case c == '+':
			builder.WriteString(`\x2b`)
		default:
			return "", fmt.Errorf("invalid character in security tag: %q", c)
		}
	}
	return builder.String(), nil
}

// SecurityTagFromUnitName converts a unit name to a security tag. Currently,
// the only character that is unescaped is the '+' character.
//
// See UnitNameFromSecurityTag for more information.
//
// Examples of conversion:
//   - "snap.name.app" -> "snap.name.app"
//   - "snap.name\x2bcomp.hook.install" -> "snap.name+comp.hook.install"
func SecurityTagFromUnitName(unitName string) string {
	return strings.ReplaceAll(unitName, `\x2b`, "+")
}
