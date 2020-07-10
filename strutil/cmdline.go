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
	const (
		argNone            int = iota // initial state
		argName                       // looking at argument name
		argAssign                     // looking at =
		argValue                      // looking at unquoted value
		argValueQuoteStart            // looking at start of quoted value
		argValueQuoted                // looking at quoted value
		argValueQuoteEnd              // looking at end of quoted value
	)
	var b bytes.Buffer
	var rs = []rune(s)
	var last = len(rs) - 1
	var errUnexpectedQuote = fmt.Errorf("unexpected quoting")
	var errUnbalancedQUote = fmt.Errorf("unbalanced quoting")
	var errUnexpectedArgument = fmt.Errorf("unexpected argument")
	var errUnexpectedAssignment = fmt.Errorf("unexpected assignment")
	// arguments are:
	// - arg
	// - arg=value, where value can be any string, spaces are preserve when quoting ".."
	var state = argNone
	for idx, r := range rs {
		maybeSplit := false
		switch state {
		case argNone:
			switch r {
			case '"':
				return nil, errUnexpectedQuote
			case ' ':
				maybeSplit = true
			default:
				state = argName
				b.WriteRune(r)
			}
		case argName:
			switch r {
			case ' ':
				maybeSplit = true
				state = argNone
			case '"':
				return nil, errUnexpectedQuote
			case '=':
				state = argAssign
				fallthrough
			default:
				b.WriteRune(r)
			}
		case argAssign:
			switch r {
			case ' ':
				// no value: arg=
				maybeSplit = true
				state = argNone
			case '"':
				// arg="..
				state = argValueQuoteStart
				b.WriteRune(r)
			default:
				// arg=v..
				state = argValue
				b.WriteRune(r)
			}
		case argValue:
			switch r {
			case ' ':
				state = argNone
				maybeSplit = true
			case '"':
				// arg=foo"
				return nil, errUnexpectedQuote
			default:
				// arg=value...
				b.WriteRune(r)
			}
		case argValueQuoteStart:
			switch r {
			case '"':
				// closing quote: arg=""
				state = argValueQuoteEnd
				b.WriteRune(r)
			default:
				state = argValueQuoted
				b.WriteRune(r)
			}
		case argValueQuoted:
			switch r {
			case '"':
				// closing quote: arg="foo"
				state = argValueQuoteEnd
				fallthrough
			default:
				b.WriteRune(r)
			}
		case argValueQuoteEnd:
			switch r {
			case '"':
				// arg="foo""
				return nil, errUnexpectedQuote
			case ' ':
				maybeSplit = true
				state = argNone
			case '=':
				// arg="foo"bar
				return nil, errUnexpectedAssignment
			default:
				// arg="foo"bar
				return nil, errUnexpectedArgument
			}
		}
		if maybeSplit || idx == last {
			// split now
			if b.Len() != 0 {
				out = append(out, b.String())
				b.Reset()
			}
		}
	}
	switch state {
	case argValueQuoteStart, argValueQuoted:
		// ended at arg=" or arg="foo
		return nil, errUnbalancedQUote
	}
	return out, nil
}
