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
