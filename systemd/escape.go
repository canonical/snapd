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

// EscapePath works like systemd-escape --path
// FIXME: use github.com/coreos/go-systemd/unit/escape.go
//        once we can update go-systemd. we can not right now
//        because versions >= 2 are not compatible with go1.3 from
//        15.04
func EscapePath(in string) string {
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
		in = in[1:len(in)]
	}

	// replace all special chars
	for i := 0; i < len(in); i++ {
		if strings.IndexByte(allowed, in[i]) >= 0 {
			fmt.Fprintf(buf, "%c", in[i])
		} else {
			fmt.Fprintf(buf, `\x%x`, (in[i]))
		}
	}
	// now replace the special char "/" with "-"
	res := bytes.Replace(buf.Bytes(), []byte(`\x2f`), []byte(`-`), -1)

	return string(res)
}
