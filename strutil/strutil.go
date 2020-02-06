// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Convert the given size in btes to a readable string
func SizeToStr(size int64) string {
	suffixes := []string{"B", "kB", "MB", "GB", "TB", "PB", "EB"}
	for _, suf := range suffixes {
		if size < 1000 {
			return fmt.Sprintf("%d%s", size, suf)
		}
		size /= 1000
	}
	panic("SizeToStr got a size bigger than math.MaxInt64")
}

// Quoted formats a slice of strings to a quoted list of
// comma-separated strings, e.g. `"snap1", "snap2"`
func Quoted(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = strconv.Quote(name)
	}

	return strings.Join(quoted, ", ")
}

// ListContains determines whether the given string is contained in the
// given list of strings.
func ListContains(list []string, str string) bool {
	for _, k := range list {
		if k == str {
			return true
		}
	}
	return false
}

// SortedListContains determines whether the given string is contained
// in the given list of strings, which must be sorted.
func SortedListContains(list []string, str string) bool {
	i := sort.SearchStrings(list, str)
	if i >= len(list) {
		return false
	}
	return list[i] == str
}

// TruncateOutput truncates input data by maxLines, imposing maxBytes limit (total) for them.
// The maxLines may be 0 to avoid the constraint on number of lines.
func TruncateOutput(data []byte, maxLines, maxBytes int) []byte {
	if maxBytes > len(data) {
		maxBytes = len(data)
	}
	lines := maxLines
	bytes := maxBytes
	for i := len(data) - 1; i >= 0; i-- {
		if data[i] == '\n' {
			lines--
		}
		if lines == 0 || bytes == 0 {
			return data[i+1:]
		}
		bytes--
	}
	return data
}

// SplitUnit takes a string of the form "123unit" and splits
// it into the number and non-number parts (123,"unit").
func SplitUnit(inp string) (number int64, unit string, err error) {
	// go after the number first, break on first non-digit
	nonDigit := -1
	for i, c := range inp {
		// ASCII digits and - only
		if (c < '0' || c > '9') && c != '-' {
			nonDigit = i
			break
		}
	}
	var prefix string
	switch {
	case nonDigit == 0:
		return 0, "", fmt.Errorf("no numerical prefix")
	case nonDigit == -1:
		// no unit
		prefix = inp
	default:
		unit = inp[nonDigit:]
		prefix = inp[:nonDigit]
	}
	number, err = strconv.ParseInt(prefix, 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("%q is not a number", prefix)
	}

	return number, unit, nil
}

// ParseByteSize parses a value like 500kB and returns the number
// in bytes. The case of the unit will be ignored for user convenience.
func ParseByteSize(inp string) (int64, error) {
	unitMultiplier := map[string]int64{
		"B": 1,
		// strictly speaking this is "kB" but we ignore cases
		"KB": 1000,
		"MB": 1000 * 1000,
		"GB": 1000 * 1000 * 1000,
		"TB": 1000 * 1000 * 1000 * 1000,
		"PB": 1000 * 1000 * 1000 * 1000 * 1000,
		"EB": 1000 * 1000 * 1000 * 1000 * 1000 * 1000,
	}

	errPrefix := fmt.Sprintf("cannot parse %q: ", inp)

	val, unit, err := SplitUnit(inp)
	if err != nil {
		return 0, fmt.Errorf(errPrefix+"%s", err)
	}
	if unit == "" {
		return 0, fmt.Errorf(errPrefix + "need a number with a unit as input")
	}
	if val < 0 {
		return 0, fmt.Errorf(errPrefix + "size cannot be negative")
	}

	mul, ok := unitMultiplier[strings.ToUpper(unit)]
	if !ok {
		return 0, fmt.Errorf(errPrefix + "try 'kB' or 'MB'")
	}

	return val * mul, nil
}

// CommaSeparatedList takes a comman-separated series of identifiers,
// and returns a slice of the space-trimmed identifiers, without empty
// entries.
// So " foo ,, bar,baz" -> {"foo", "bar", "baz"}
func CommaSeparatedList(str string) []string {
	fields := strings.FieldsFunc(str, func(r rune) bool { return r == ',' })
	filtered := fields[:0]
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			filtered = append(filtered, field)
		}
	}
	return filtered
}

// ElliptRight returns a string that is at most n runes long,
// replacing the last rune with an ellipsis if necessary. If N is less
// than 1 it's treated as a 1.
func ElliptRight(str string, n int) string {
	if n < 1 {
		n = 1
	}
	if utf8.RuneCountInString(str) <= n {
		return str
	}

	// this is expensive; look into a cheaper way maybe sometime
	return string([]rune(str)[:n-1]) + "…"
}

// ElliptLeft returns a string that is at most n runes long,
// replacing the first rune with an ellipsis if necessary. If N is less
// than 1 it's treated as a 1.
func ElliptLeft(str string, n int) string {
	if n < 1 {
		n = 1
	}
	// this is expensive; look into a cheaper way maybe sometime
	rstr := []rune(str)
	if len(rstr) <= n {
		return str
	}

	return "…" + string(rstr[len(rstr)-n+1:])
}
