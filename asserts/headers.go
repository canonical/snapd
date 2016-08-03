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
	"regexp"
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

var (
	nl   = []byte("\n")
	nlnl = []byte("\n\n")

	// for basic sanity checking of header names
	headerNameSanity = regexp.MustCompile("^[a-z][a-z0-9-]*[a-z0-9]$")
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
		// TODO: support maps

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

// checkHeader checks that the header values are strings, or nested lists with strings as the only scalars
func checkHeader(v interface{}) error {
	switch x := v.(type) {
	case string:
		return nil
	case []interface{}:
		for _, elem := range x {
			err := checkHeader(elem)
			if err != nil {
				return err
			}
		}
		return nil
	// TODO: support maps
	default:
		return fmt.Errorf("header values must be strings or nested lists with strings as the only scalars: %v", v)
	}
}

// checkHeaders checks that headers are of expected types
func checkHeaders(headers map[string]interface{}) error {
	for name, value := range headers {
		err := checkHeader(value)
		if err != nil {
			return fmt.Errorf("header %q: %v", name, err)
		}
	}
	return nil
}

// copyHeader helps deep copying header values to defend against external mutations
func copyHeader(v interface{}) interface{} {
	switch x := v.(type) {
	case string:
		return x
	case []interface{}:
		res := make([]interface{}, len(x))
		for i, elem := range x {
			res[i] = copyHeader(elem)
		}
		return res
	case map[string]interface{}:
		res := make(map[string]interface{}, len(x))
		for name, value := range x {
			if value == nil {
				continue // normalize nils out
			}
			res[name] = copyHeader(value)
		}
		return res
	default:
		panic(fmt.Sprintf("internal error: encountered unexpected value type copying headers: %v", v))
	}
}

// copyHeader helps deep copying headers to defend against external mutations
func copyHeaders(headers map[string]interface{}) map[string]interface{} {
	return copyHeader(headers).(map[string]interface{})
}

func appendEntry(buf *bytes.Buffer, intro string, v interface{}, baseIndent int) {
	switch x := v.(type) {
	case nil:
		return // omit
	case string:
		buf.WriteByte('\n')
		buf.WriteString(intro)
		if strings.IndexRune(x, '\n') != -1 {
			// multiline value => quote by 4-space indenting
			buf.WriteByte('\n')
			pfx := nestingPrefix(baseIndent, multilinePrefix)
			buf.WriteString(pfx)
			x = strings.Replace(x, "\n", "\n"+pfx, -1)
		} else {
			buf.WriteByte(' ')
		}
		buf.WriteString(x)
	case []interface{}:
		if len(x) == 0 {
			return // simply omit
		}
		buf.WriteByte('\n')
		buf.WriteString(intro)
		pfx := nestingPrefix(baseIndent, listPrefix)
		for _, elem := range x {
			appendEntry(buf, pfx, elem, baseIndent+len(listPrefix)-1)
		}
	// TODO: support maps
	default:
		panic(fmt.Sprintf("internal error: encountered unexpected value type formatting headers: %v", v))
	}
}
