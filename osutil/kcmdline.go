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

package osutil

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"strings"
)

var (
	procCmdline = "/proc/cmdline"
)

// MockProcCmdline overrides the path to /proc/cmdline. For use in tests.
func MockProcCmdline(newPath string) (restore func()) {
	MustBeTestBinary("mocking can only be done from tests")
	oldProcCmdline := procCmdline
	procCmdline = newPath
	return func() {
		procCmdline = oldProcCmdline
	}
}

// KernelCommandLineSplit tries to split the string comprising full or a part
// of a kernel command line into a list of individual arguments. Returns an
// error when the input string is incorrectly formatted.
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
			case '=':
				return nil, errUnexpectedAssignment
			case ' ':
				maybeSplit = true
			default:
				state = argName
				b.WriteRune(r)
			}
		case argName:
			switch r {
			case '"':
				return nil, errUnexpectedQuote
			case ' ':
				maybeSplit = true
				state = argNone
			case '=':
				state = argAssign
				fallthrough
			default:
				b.WriteRune(r)
			}
		case argAssign:
			switch r {
			case '=':
				return nil, errUnexpectedAssignment
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
			case '"':
				// arg=foo"
				return nil, errUnexpectedQuote
			case ' ':
				state = argNone
				maybeSplit = true
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
			case ' ':
				maybeSplit = true
				state = argNone
			case '"':
				// arg="foo""
				return nil, errUnexpectedQuote
			case '=':
				// arg="foo"=
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

// KernelCommandLineKeyValues returns a map of the specified keys to the values
// set for them in the kernel command line (eg. panic=-1). If the value is
// missing from the kernel command line or it has no value (eg. quiet), the key
// is omitted from the returned map.
func KernelCommandLineKeyValues(keys ...string) (map[string]string, error) {
	cmdline, err := KernelCommandLine()
	if err != nil {
		return nil, err
	}
	params, err := KernelCommandLineSplit(cmdline)
	if err != nil {
		return nil, err
	}

	m := make(map[string]string, len(keys))

	for _, param := range params {
		for _, key := range keys {
			if strings.HasPrefix(param, fmt.Sprintf("%s=", key)) {
				res := strings.SplitN(param, "=", 2)
				// we have confirmed key= prefix, thus len(res)
				// is always 2
				m[key] = res[1]
				break
			}
		}
	}
	return m, nil
}

// KernelCommandLine returns the command line reported by the running kernel.
func KernelCommandLine() (string, error) {
	buf, err := ioutil.ReadFile(procCmdline)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buf)), nil
}
