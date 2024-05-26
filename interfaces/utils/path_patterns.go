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

	"github.com/ddkwork/golibrary/mylog"
)

type PathPattern struct {
	pattern string
	regex   *regexp.Regexp
}

const maxGroupDepth = 50

type GlobFlags int

const (
	globDefault GlobFlags = 1 << iota
	globNull
)

// createRegex converts the apparmor-like glob sequence into a regex. Loosely
// using this as reference:
// https://gitlab.com/apparmor/apparmor/-/blob/master/parser/parser_regex.c#L107
func createRegex(pattern string, glob GlobFlags, allowCommas bool) (string, error) {
	regex := "^"

	appendGlob := func(defaultGlob, nullGlob string) {
		var pattern string
		switch glob {
		case globDefault:
			pattern = defaultGlob
		case globNull:
			pattern = nullGlob
		}
		regex += pattern
	}

	const (
		noSlashOrNull = `[^/\x00]`
		noSlash       = `[^/]`
	)

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
			regex += string(ch)
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
					appendGlob(noSlashOrNull, noSlash)
				}
			}

			if len(pattern) > i+1 && pattern[i+1] == '*' {
				// Handle **
				appendGlob("[^\\x00]*", ".*")
				skipNext = true
			} else {
				appendGlob(noSlashOrNull+"*", noSlash+"*")
			}
		case '?':
			appendGlob(noSlashOrNull, noSlash)
		case '[':
			inCharClass = true
			regex += string(ch)
		case ']':
			if !inCharClass {
				return "", fmt.Errorf("pattern contains unmatching ']': %q", pattern)
			}
			inCharClass = false
			regex += string(ch)
		case '{':
			currentGroupLevel++
			if currentGroupLevel > maxGroupDepth {
				return "", fmt.Errorf("maximum group depth exceeded: %q", pattern)
			}
			itemCountInGroup[currentGroupLevel] = 0
			regex += "("
		case '}':
			if currentGroupLevel <= 0 {
				return "", fmt.Errorf("invalid closing brace, no matching open { found: %q", pattern)
			}
			if itemCountInGroup[currentGroupLevel] == 0 {
				return "", fmt.Errorf("invalid number of items between {}: %q", pattern)
			}
			currentGroupLevel--
			regex += ")"
		case ',':
			if currentGroupLevel > 0 {
				itemCountInGroup[currentGroupLevel]++
				regex += "|"
			} else if allowCommas {
				// treat commas outside of groups as literal commas
				regex += ","
			} else {
				return "", fmt.Errorf("cannot use ',' outside of group or character class")
			}
		default:
			// take literal character (with quoting if needed)
			regex += regexp.QuoteMeta(string(ch))
		}
	}

	if currentGroupLevel > 0 {
		return "", fmt.Errorf("missing %d closing brace(s): %q", currentGroupLevel, pattern)
	}
	if inCharClass {
		return "", fmt.Errorf("missing closing bracket ']': %q", pattern)
	}
	if escapeNext {
		return "", fmt.Errorf("expected character after '\\': %q", pattern)
	}

	regex += "$"
	return regex, nil
}

func NewPathPattern(pattern string, allowCommas bool) (*PathPattern, error) {
	regexPattern := mylog.Check2(createRegex(pattern, globDefault, allowCommas))

	regex := regexp.MustCompile(regexPattern)

	pp := &PathPattern{pattern, regex}
	return pp, nil
}

func (pp *PathPattern) Matches(path string) bool {
	return pp.regex.MatchString(path)
}
