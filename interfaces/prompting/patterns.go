// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package prompting

import (
	"fmt"
	"strings"

	doublestar "github.com/bmatcuk/doublestar/v4"
)

// Limit the number of groups which are allowed to occur in a pattern.
// This number includes all groups, including those nested or in series.
const maxGroupsInPattern = 10

// ValidatePathPattern returns nil if the pattern is valid, otherwise an error.
func ValidatePathPattern(pattern string) error {
	if pattern == "" || pattern[0] != '/' {
		return fmt.Errorf("invalid path pattern: must start with '/': %q", pattern)
	}
	depth := 0
	totalGroups := 0
	reader := strings.NewReader(pattern)
	for {
		r, _, err := reader.ReadRune()
		if err != nil {
			// No more runes
			break
		}
		switch r {
		case '{':
			depth += 1
			totalGroups += 1
			if totalGroups > maxGroupsInPattern {
				return fmt.Errorf("invalid path pattern: exceeded maximum number of groups (%d): %q", maxGroupsInPattern, pattern)
			}
		case '}':
			depth -= 1
			if depth < 0 {
				return fmt.Errorf("invalid path pattern: unmatched '}' character: %q", pattern)
			}
		case '\\':
			// Skip next rune
			_, _, err = reader.ReadRune()
			if err != nil {
				return fmt.Errorf(`invalid path pattern: trailing unescaped '\' character: %q`, pattern)
			}
		case '[', ']':
			return fmt.Errorf("invalid path pattern: cannot contain unescaped '[' or ']': %q", pattern)
		}
	}
	if depth != 0 {
		return fmt.Errorf("invalid path pattern: unmatched '{' character: %q", pattern)
	}
	if !doublestar.ValidatePattern(pattern) {
		return fmt.Errorf("invalid path pattern: %q", pattern)
	}
	return nil
}

// PathPatternMatch returns true if the given pattern matches the given path.
//
// The pattern should not contain groups, and should likely have been an output
// of ExpandPathPattern.
//
// Paths to directories are received with trailing slashes, but we don't want
// to require the user to include a trailing '/' if they want to match
// directories (and end their pattern with `{,/}` if they want to match both
// directories and non-directories). Thus, we want to ensure that patterns
// without trailing slashes match paths with trailing slashes. However,
// patterns with trailing slashes should not match paths without trailing
// slashes.
//
// The doublestar package has special cases for patterns ending in `/**` and
// `/**/`: `/foo/**`, and `/foo/**/` both match `/foo` and `/foo/`. We want to
// override this behavior to make `/foo/**/` not match `/foo`. We also want to
// override doublestar to make `/foo` match `/foo/`.
func PathPatternMatch(pattern string, path string) (bool, error) {
	// Check the usual doublestar match first, in case the pattern is malformed
	// and causes an error, and return the error if so.
	matched, err := doublestar.Match(pattern, path)
	if err != nil {
		return false, err
	}
	// No matter if doublestar matched, return false if pattern ends in '/' but
	// path is not a directory.
	if strings.HasSuffix(pattern, "/") && !strings.HasSuffix(path, "/") {
		return false, nil
	}
	if matched {
		return true, nil
	}
	if strings.HasSuffix(pattern, "/") {
		return false, nil
	}
	// Try again with a '/' appended to the pattern, so patterns like `/foo`
	// match paths like `/foo/`.
	return doublestar.Match(pattern+"/", path)
}
