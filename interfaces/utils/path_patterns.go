// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package utils

import (
	"fmt"
	"regexp"
)

type PathPattern struct {
	pattern string
	regex   *regexp.Regexp
}

const maxGroupDepth = 50

// createRegex converts the apparmor-like glob sequence into a regex. Loosely
// using this as reference:
// https://gitlab.com/apparmor/apparmor/-/blob/master/parser/parser_regex.c#L107
func createRegex(pattern string) (string, error) {
	regex := "^"

	escapeNext := false
	currentGroupLevel := 0
	inCharClass := false
	skipNext := false
	itemCountInGroup := new([maxGroupDepth + 1]int)
	for i, ch := range pattern {
		if escapeNext {
			regex += regexp.QuoteMeta(string(ch))
			escapeNext = false
			continue
		}
		if skipNext {
			skipNext = false
			continue
		}
		if inCharClass && ch != '\\' && ch != ']' {
			// no characters are special other than '\' and ']'
			regex += regexp.QuoteMeta(string(ch))
			continue
		}
		switch ch {
		case '\\':
			escapeNext = true
		case '*':
			if regex[len(regex)-1] == '/' {
				// if the * is at the end of the pattern or is followed by a
				// '/' we don't want it to match an empty string:
				// /foo/* -> should not match /foo/
				// /foo/*bar -> should match /foo/bar
				// /*/foo -> should not match //foo
				pos := i + 1
				for len(pattern) > pos && pattern[pos] == '*' {
					pos++
				}
				if len(pattern) <= pos || pattern[pos] == '/' {
					regex += "[^/]"
				}
			}

			if len(pattern) > i+1 && pattern[i+1] == '*' {
				// Handle **
				regex += ".*"
				skipNext = true
			} else {
				regex += "[^/]*"
			}
		case '?':
			regex += "[^/]"
		case '[':
			inCharClass = true
			regex += string(ch)
		case ']':
			if !inCharClass {
				return "", fmt.Errorf("Pattern contains unmatching ']': %q", pattern)
			}
			inCharClass = false
			regex += string(ch)
		case '{':
			currentGroupLevel++
			if currentGroupLevel > maxGroupDepth {
				return "", fmt.Errorf("Maximum group depth exceeded: %q", pattern)
			}
			itemCountInGroup[currentGroupLevel] = 0
			regex += "("
		case '}':
			if currentGroupLevel <= 0 {
				return "", fmt.Errorf("Invalid closing brace, no matching open { found: %q", pattern)
			}
			if itemCountInGroup[currentGroupLevel] == 0 {
				return "", fmt.Errorf("Invalid number of items between {}: %q", pattern)
			}
			currentGroupLevel--
			regex += ")"
		case ',':
			if currentGroupLevel > 0 {
				itemCountInGroup[currentGroupLevel]++
				regex += "|"
			} else {
				regex += ","
			}
		default:
			// take literal character (with quoting if needed)
			regex += regexp.QuoteMeta(string(ch))
		}
	}

	if currentGroupLevel > 0 {
		return "", fmt.Errorf("Missing %d closing brace(s): %q", currentGroupLevel, pattern)
	}
	if inCharClass {
		return "", fmt.Errorf("Missing closing bracket ']': %q", pattern)
	}
	if escapeNext {
		return "", fmt.Errorf("Expected character after '\\': %q", pattern)
	}

	regex += "$"
	return regex, nil
}

func NewPathPattern(pattern string) (*PathPattern, error) {
	regexPattern, err := createRegex(pattern)
	if err != nil {
		return nil, err
	}

	regex := regexp.MustCompile(regexPattern)

	pp := &PathPattern{pattern, regex}
	return pp, nil
}

func (pp *PathPattern) Matches(path string) bool {
	return pp.regex.MatchString(path)
}
