// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2024 Canonical Ltd
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
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SizeToStr converts the given size in bytes to a readable string
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

// IntsToCommaSeparated converts an int array to a comma-separated string without whitespace
func IntsToCommaSeparated(vals []int) string {
	b := &strings.Builder{}
	last := len(vals) - 1
	for i, v := range vals {
		b.WriteString(strconv.Itoa(v))
		if i != last {
			b.WriteRune(',')
		}
	}
	return b.String()
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

// SortedListsUniqueMerge merges the two given sorted lists of strings,
// repeated values will appear once in the result.
func SortedListsUniqueMerge(sl1, sl2 []string) []string {
	n1 := len(sl1)
	n2 := len(sl2)
	sz := n1
	if n2 > sz {
		sz = n2
	}
	if sz == 0 {
		return nil
	}
	m := make([]string, 0, sz)
	appendUnique := func(s string) {
		if l := len(m); l > 0 && m[l-1] == s {
			return
		}
		m = append(m, s)
	}
	i, j := 0, 0
	for i < n1 && j < n2 {
		var s string
		if sl1[i] < sl2[j] {
			s = sl1[i]
			i++
		} else {
			s = sl2[j]
			j++
		}
		appendUnique(s)
	}
	if i < n1 {
		for ; i < n1; i++ {
			appendUnique(sl1[i])
		}
	} else if j < n2 {
		for ; j < n2; j++ {
			appendUnique(sl2[j])
		}
	}
	return m
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
		return 0, fmt.Errorf("%s%s", errPrefix, err)
	}
	if unit == "" {
		return 0, fmt.Errorf("%sneed a number with a unit as input", errPrefix)
	}
	if val < 0 {
		return 0, fmt.Errorf("%ssize cannot be negative", errPrefix)
	}

	mul, ok := unitMultiplier[strings.ToUpper(unit)]
	if !ok {
		return 0, fmt.Errorf("%stry 'kB' or 'MB'", errPrefix)
	}

	return val * mul, nil
}

// CommaSeparatedList takes a comma-separated series of identifiers,
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

// MultiCommaSeparatedList parses each string in strs with CommaSeparatedList
// and returns the concatenation of all parsed values.
func MultiCommaSeparatedList(strs []string) []string {
	var values []string
	for _, s := range strs {
		values = append(values, CommaSeparatedList(s)...)
	}
	return values
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

// Deduplicate returns a newly allocated slice with the same contents
// as the input, excluding duplicates.
func Deduplicate(sl []string) []string {
	dedup := make([]string, 0, len(sl))
	seen := make(map[string]struct{}, len(sl))

	for _, str := range sl {
		if _, ok := seen[str]; !ok {
			seen[str] = struct{}{}
			dedup = append(dedup, str)
		}
	}

	return dedup
}

// runesLastIndexSpace returns the index of the last whitespace rune
// in the text. If the text has no whitespace, returns -1.
func runesLastIndexSpace(text []rune) int {
	for i := len(text) - 1; i >= 0; i-- {
		if unicode.IsSpace(text[i]) {
			return i
		}
	}
	return -1
}

// WordWrap wraps the given text to the given width, prefixing the
// first line with indent and the remaining lines with indent2
func WordWrap(out io.Writer, text []rune, indent, indent2 string, termWidth int) error {
	// Note: this is _wrong_ for much of unicode (because the width of a rune on
	//       the terminal is anything between 0 and 2, not always 1 as this code
	//       assumes) but fixing that is Hard. Long story short, you can get close
	//       using a couple of big unicode tables (which is what wcwidth
	//       does). Getting it 100% requires a terminfo-alike of unicode behaviour.
	//       However, before this we'd count bytes instead of runes, so we'd be
	//       even more broken. Think of it as successive approximations... at least
	//       with this work we share tabwriter's opinion on the width of things!
	indentWidth := utf8.RuneCountInString(indent)
	delta := indentWidth - utf8.RuneCountInString(indent2)
	width := termWidth - indentWidth
	if width < 1 {
		width = 1
	}

	// establish the indent of the whole block
	var err error
	for len(text) > width && err == nil {
		// find a good place to chop the text
		idx := runesLastIndexSpace(text[:width+1])
		if idx < 0 {
			// there's no whitespace; just chop at line width
			idx = width
		}
		_, err = fmt.Fprint(out, indent, string(text[:idx]), "\n")
		// prune any remaining whitespace before the start of the next line
		for idx < len(text) && unicode.IsSpace(text[idx]) {
			idx++
		}
		text = text[idx:]
		width += delta
		indent = indent2
		delta = 0
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(out, indent, string(text), "\n")
	return err
}

// WordWrapPadded wraps the given text, assumed to be part of a block-style yaml
// string, to fit into termWidth, preserving the line's indent, and
// writes it out prepending padding to each line.
func WordWrapPadded(out io.Writer, text []rune, pad string, termWidth int) error {
	// discard any trailing whitespace
	text = []rune(strings.TrimRightFunc(string(text), unicode.IsSpace))
	// establish the indent of the whole block
	idx := 0
	for idx < len(text) && unicode.IsSpace(text[idx]) {
		idx++
	}
	indent := pad + string(text[:idx])
	text = text[idx:]
	if len(indent) > termWidth/2 {
		// If indent is too big there's not enough space for the actual
		// text, in the pathological case the indent can even be bigger
		// than the terminal which leads to lp:1828425.
		// Rather than let that happen, give up.
		indent = pad + "  "
	}
	return WordWrap(out, text, indent, indent, termWidth)
}

// JoinNonEmpty concatenates non-empty strings using sep as separator,
// and trimming sep from beginning and end of the strings. This
// overcomes a problem with strings.Join, which will introduce
// separators for empty strings.
func JoinNonEmpty(strs []string, sep string) string {
	nonEmpty := make([]string, 0, len(strs))
	for _, s := range strs {
		s = strings.Trim(s, sep)
		if s != "" {
			nonEmpty = append(nonEmpty, s)
		}
	}
	return strings.Join(nonEmpty, sep)
}

// CommonPrefix returns the prefix common to a slice of strings.
//
// The prefix for an empty slice is an empty string.
// The prefix for a slice with one element is the element itself.
func CommonPrefix(elems []string) string {
	if len(elems) == 0 {
		return ""
	}

	prefix := elems[0]
	for _, s := range elems {
		for !strings.HasPrefix(s, prefix) {
			prefix = prefix[:len(prefix)-1]
			if prefix == "" {
				return prefix
			}
		}
	}

	return prefix
}
