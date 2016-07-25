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

func nestingPrefix(nesting int, prefix string) string {
	return strings.Repeat("    ", nesting) + prefix
}

func parseEntry(consumedByIntro int, i int, lines []string, nesting int) (value interface{}, after int, err error) {
	entry := lines[i]
	i++
	if consumedByIntro == len(entry) {
		// multiline values
		if i < len(lines) && strings.HasPrefix(lines[i], nestingPrefix(nesting, "  -")) {
			// list
			return parseList(i, lines, nesting)
		}

		return parseMultilineText(i, lines, nesting)
	}

	// simple one-line value
	if entry[consumedByIntro] != ' ' {
		return nil, -1, fmt.Errorf("header entry should have a space or newline (multiline) before value: %q", entry)
	}

	return entry[consumedByIntro+1:], i, nil
}

func parseMultilineText(i int, lines []string, nesting int) (value interface{}, after int, err error) {
	size := 0
	j := i
	prefix := nestingPrefix(nesting+1, "")
	for j < len(lines) {
		iline := lines[j]
		if !strings.HasPrefix(iline, prefix) {
			break
		}
		size += len(iline) - len(prefix) + 1
		j++
	}
	if j == i {
		return nil, -1, fmt.Errorf("expected indentation %q after multiline text introduction: %q", prefix, lines[i-1])
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

func parseList(start int, lines []string, nesting int) (value interface{}, after int, err error) {
	lst := []interface{}(nil)
	j := start
	prefix := nestingPrefix(nesting, "  -")
	for j < len(lines) {
		if !strings.HasPrefix(lines[j], prefix) {
			return lst, j, nil
		}
		var v interface{}
		var err error
		v, j, err = parseEntry(len(prefix), j, lines, nesting+1)
		if err != nil {
			return nil, -1, err
		}
		lst = append(lst, v)
	}
	return lst, j, nil
}
