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
	"path/filepath"
	"strings"
)

const allowed = `:_.abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789`

// EscapePath works like systemd-escape --path
// FIMXE: use "github.com/coreos/go-systemd/unit" once we stop worry about
//        compatibility for go1.3
func EscapePath(in string) string {
	out := []byte{}

	in = filepath.Clean(in)
	in = strings.TrimLeft(in, "/")
	if len(in) == 0 {
		in = "/"
	}
	in = strings.Replace(in, "/", "-", -1)

	for i := 0; i < len(in); i++ {
		if strings.IndexByte(allowed, in[i]) >= 0 {
			out = append(out, in[i])
		} else {
			out = append(out, byte(in[i]))
		}
	}

	return string(out)
}
