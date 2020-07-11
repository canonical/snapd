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

package strutil

import (
	"bytes"
	"fmt"
)

// KernelCommandLineSplit tries to split the string into a list of elements that
// would be passed by the bootloader as the kernel command arguments.
//
// See https://www.kernel.org/doc/html/latest/admin-guide/kernel-parameters.html for details.
func KernelCommandLineSplit(s string) (out []string, err error) {
	var quoting bool
	var b bytes.Buffer
	var rs = []rune(s)
	var last = len(rs) - 1
	// arguments are:
	// - arg
	// - arg=value, where value can be any string, spaces are preserve when quoting ".."
	for idx, r := range rs {
		maybeSplit := false
		switch r {
		case '"':
			if !quoting {
				if b.Len() < 2 || idx == 0 || (idx > 0 && rs[idx-1] != '=') {
					// either:
					// - the whole input starts with "
					// - preceding character is not =
					// - there's no at least `a=` collected so far
					return nil, fmt.Errorf("unexpected quoting")
				}
				quoting = true
			} else {
				quoting = false
			}
			b.WriteRune(r)
		case ' ':
			if quoting {
				b.WriteRune(r)
			} else {
				maybeSplit = true
			}
		default:
			b.WriteRune(r)
		}
		if maybeSplit || idx == last {
			// split now
			if b.Len() != 0 {
				out = append(out, b.String())
				b.Reset()
			}
			continue
		}
	}
	if quoting {
		return nil, fmt.Errorf("unbalanced quoting")
	}
	return out, nil
}
