// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package asserts

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

/* TODO: map support

one:
  two:
    three:

one:
  two:
      three

map-within-map:
  lev1:
    lev2: x

list-of-maps:
  -
    entry: foo
    bar: baz
  -
    entry: bar

*/

func parseHeaders(head []byte) (map[string]interface{}, error) {
	if !utf8.Valid(head) {
		return nil, fmt.Errorf("header is not utf8")
	}
	headers := make(map[string]interface{})
	lines := strings.Split(string(head), "\n")
	for i := 0; i < len(lines); {
		entry := lines[i]
		nameValueSplit := strings.Index(entry, ":")
		if nameValueSplit == -1 {
			return nil, fmt.Errorf("header entry missing ':' separator: %q", entry)
		}
		name := entry[:nameValueSplit]
		if !headerNameSanity.MatchString(name) {
			return nil, fmt.Errorf("invalid header name: %q", name)
		}

		consumed := nameValueSplit + 1
		var value interface{}
		var err error
		value, i, err = parseEntry(consumed, i, lines, 0)
		if err != nil {
			return nil, err
		}

		headers[name] = value
	}
	return headers, nil
}

const (
	multilinePrefix = "    "
	listPrefix      = "  -"
)

func nestingPrefix(baseIndent int, prefix string) string {
	return strings.Repeat(" ", baseIndent) + prefix
}

func parseEntry(consumedByIntro int, first int, lines []string, baseIndent int) (value interface{}, firstAfter int, err error) {
	entry := lines[first]
	i := first + 1
	if consumedByIntro == len(entry) {
		// multiline values
		if i < len(lines) && strings.HasPrefix(lines[i], nestingPrefix(baseIndent, listPrefix)) {
			// list
			return parseList(i, lines, baseIndent)
		}

		return parseMultilineText(i, lines, baseIndent)
	}

	// simple one-line value
	if entry[consumedByIntro] != ' ' {
		return nil, -1, fmt.Errorf("header entry should have a space or newline (for multiline) before value: %q", entry)
	}

	return entry[consumedByIntro+1:], i, nil
}

func parseMultilineText(first int, lines []string, baseIndent int) (value interface{}, firstAfter int, err error) {
	size := 0
	i := first
	j := i
	prefix := nestingPrefix(baseIndent, multilinePrefix)
	for j < len(lines) {
		iline := lines[j]
		if !strings.HasPrefix(iline, prefix) {
			break
		}
		size += len(iline) - len(prefix) + 1
		j++
	}
	if j == i {
		var cur string
		if i == len(lines) {
			cur = "EOF"
		} else {
			cur = fmt.Sprintf("%q", lines[i])
		}
		return nil, -1, fmt.Errorf("expected %d chars nesting prefix after multiline introduction %q: %s", len(prefix), lines[i-1], cur)
	}

	valueBuf := bytes.NewBuffer(make([]byte, 0, size-1))
	valueBuf.WriteString(lines[i][len(prefix):])
	i++
	for i < j {
		valueBuf.WriteByte('\n')
		valueBuf.WriteString(lines[i][len(prefix):])
		i++
	}

	return valueBuf.String(), i, nil
}

func parseList(first int, lines []string, baseIndent int) (value interface{}, firstAfter int, err error) {
	lst := []interface{}(nil)
	j := first
	prefix := nestingPrefix(baseIndent, listPrefix)
	for j < len(lines) {
		if !strings.HasPrefix(lines[j], prefix) {
			return lst, j, nil
		}
		var v interface{}
		var err error
		v, j, err = parseEntry(len(prefix), j, lines, baseIndent+len(listPrefix)-1)
		if err != nil {
			return nil, -1, err
		}
		lst = append(lst, v)
	}
	return lst, j, nil
}
