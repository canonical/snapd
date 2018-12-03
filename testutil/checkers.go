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

package testutil

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"regexp"
	"strings"

	"gopkg.in/check.v1"
)

type fileContentChecker struct {
	*check.CheckerInfo
	exact bool
}

// FileEquals verifies that the given file's content is equal
// to the string (or fmt.Stringer) or []byte provided.
var FileEquals check.Checker = &fileContentChecker{
	CheckerInfo: &check.CheckerInfo{Name: "FileEquals", Params: []string{"filename", "contents"}},
	exact:       true,
}

// FileContains verifies that the given file's content contains
// the string (or fmt.Stringer) or []byte provided.
var FileContains check.Checker = &fileContentChecker{
	CheckerInfo: &check.CheckerInfo{Name: "FileContains", Params: []string{"filename", "contents"}},
}

// FileMatches verifies that the given file's content matches
// the string provided.
var FileMatches check.Checker = &fileContentChecker{
	CheckerInfo: &check.CheckerInfo{Name: "FileMatches", Params: []string{"filename", "regex"}},
}

func (c *fileContentChecker) Check(params []interface{}, names []string) (result bool, error string) {
	filename, ok := params[0].(string)
	if !ok {
		return false, "Filename must be a string"
	}
	if names[1] == "regex" {
		regexpr, ok := params[1].(string)
		if !ok {
			return false, "Regex must be a string"
		}
		rx, err := regexp.Compile(regexpr)
		if err != nil {
			return false, fmt.Sprintf("Cannot compile regexp %q: %v", regexpr, err)
		}
		params[1] = rx
	}
	return fileContentCheck(filename, params[1], c.exact)
}

func fileContentCheck(filename string, content interface{}, exact bool) (result bool, error string) {
	buf, err := ioutil.ReadFile(filename)
	if err != nil {
		return false, fmt.Sprintf("Cannot read file %q: %v", filename, err)
	}
	presentableBuf := string(buf)
	if exact {
		switch content := content.(type) {
		case string:
			result = presentableBuf == content
		case []byte:
			result = bytes.Equal(buf, content)
			presentableBuf = "<binary data>"
		case fmt.Stringer:
			result = presentableBuf == content.String()
		default:
			error = fmt.Sprintf("Cannot compare file contents with something of type %T", content)
		}
	} else {
		switch content := content.(type) {
		case string:
			result = strings.Contains(presentableBuf, content)
		case []byte:
			result = bytes.Contains(buf, content)
			presentableBuf = "<binary data>"
		case *regexp.Regexp:
			result = content.Match(buf)
		case fmt.Stringer:
			result = strings.Contains(presentableBuf, content.String())
		default:
			error = fmt.Sprintf("Cannot compare file contents with something of type %T", content)
		}
	}
	if !result {
		if error == "" {
			error = fmt.Sprintf("Failed to match with file contents:\n%v", presentableBuf)
		}
		return result, error
	}
	return result, ""
}

type paddedChecker struct {
	*check.CheckerInfo
	isRegexp  bool
	isPartial bool
	isLine    bool
}

// EqualsPadded is a Checker that looks for an expected string to
// be equal to another except that the other might have been padded
// out to align with something else (so arbitrary amounts of
// horizontal whitespace is ok at the ends, and between non-whitespace
// bits).
var EqualsPadded = &paddedChecker{
	CheckerInfo: &check.CheckerInfo{Name: "EqualsPadded", Params: []string{"padded", "expected"}},
	isLine:      true,
}

// MatchesPadded is a Checker that looks for an expected regexp in
// a string that might have been padded out to align with something
// else (so whitespace in the regexp is changed to [ \t]+, and ^[ \t]*
// is added to the beginning, and [ \t]*$ to the end of it).
var MatchesPadded = &paddedChecker{
	CheckerInfo: &check.CheckerInfo{Name: "MatchesPadded", Params: []string{"padded", "expected"}},
	isRegexp:    true,
	isLine:      true,
}

// ContainsPadded is a Checker that looks for an expected string
// in another that might have been padded out to align with something
// else (so arbitrary amounts of horizontal whitespace is ok between
// non-whitespace bits).
var ContainsPadded = &paddedChecker{
	CheckerInfo: &check.CheckerInfo{Name: "ContainsPadded", Params: []string{"padded", "expected"}},
	isPartial:   true,
	isLine:      true,
}

// EqualsWrapped is a Checker that looks for an expected string to be
// equal to another except that the other might have been padded out
// and wrapped (so arbitrary amounts of whitespace is ok at the ends,
// and between non-whitespace bits).
var EqualsWrapped = &paddedChecker{
	CheckerInfo: &check.CheckerInfo{Name: "EqualsWrapped", Params: []string{"wrapped", "expected"}},
}

// MatchesWrapped is a Checker that looks for an expected regexp in a
// string that might have been padded and wrapped (so whitespace in
// the regexp is changed to \s+, and (?s)^\s* is added to the
// beginning, and \s*$ to the end of it).
var MatchesWrapped = &paddedChecker{
	CheckerInfo: &check.CheckerInfo{Name: "MatchesWrapped", Params: []string{"wrapped", "expected"}},
	isRegexp:    true,
}

// ContainsWrapped is a Checker that looks for an expected string in
// another that might have been padded out and wrapped (so arbitrary
// amounts of whitespace is ok between non-whitespace bits).
var ContainsWrapped = &paddedChecker{
	CheckerInfo: &check.CheckerInfo{Name: "EqualsWrapped", Params: []string{"wrapped", "expected"}},
	isRegexp:    false,
	isPartial:   true,
}

var spaceinator = regexp.MustCompile(`\s+`).ReplaceAllLiteralString

func (c *paddedChecker) Check(params []interface{}, names []string) (result bool, errstr string) {
	var other string
	switch v := params[0].(type) {
	case string:
		other = v
	case []byte:
		other = string(v)
	case error:
		if v != nil {
			other = v.Error()
		}
	default:
		return false, "left-hand value must be a string or []byte or error"
	}
	expected, ok := params[1].(string)
	if !ok {
		ebuf, ok := params[1].([]byte)
		if !ok {
			return false, "right-hand value must be a string or []byte"
		}
		expected = string(ebuf)
	}
	ws := `\s`
	if c.isLine {
		ws = `[\t ]`
	}
	if c.isRegexp {
		_, err := regexp.Compile(expected)
		if err != nil {
			return false, fmt.Sprintf("right-hand value must be a valid regexp: %v", err)
		}
		expected = spaceinator(expected, ws+"+")
	} else {
		fields := strings.Fields(expected)
		for i := range fields {
			fields[i] = regexp.QuoteMeta(fields[i])
		}
		expected = strings.Join(fields, ws+"+")
	}
	if !c.isPartial {
		expected = "^" + ws + "*" + expected + ws + "*$"
	}
	if !c.isLine {
		expected = "(?s)" + expected
	}

	matches, err := regexp.MatchString(expected, other)
	if err != nil {
		// can't happen (really)
		panic(err)
	}
	return matches, ""
}
