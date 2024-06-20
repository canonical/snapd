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

package patterns

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	doublestar "github.com/bmatcuk/doublestar/v4"
)

// Limit the number of expanded path patterns for a particular pattern.
// When fully expanded, the number of patterns for a given unexpanded pattern
// may not exceed this limit.
const maxExpandedPatterns = 1000

// PathPattern is an iterator which yields expanded path patterns.
type PathPattern struct {
	original   string
	renderTree renderNode
}

// ParsePathPattern validates the given pattern and parses it into a PathPattern
// from which expanded path patterns can be iterated, and returns it.
func ParsePathPattern(pattern string) (*PathPattern, error) {
	pathPattern := &PathPattern{}
	if err := pathPattern.parse(pattern); err != nil {
		return nil, err
	}
	return pathPattern, nil
}

// parse validates the given pattern and parses it into a PathPattern from
// which expanded path patterns can be iterated, overwriting the receiver.
func (p *PathPattern) parse(pattern string) error {
	prefix := fmt.Sprintf("cannot parse path pattern %q", pattern)
	if pattern == "" {
		return fmt.Errorf("%s: pattern has length 0", prefix)
	}
	if pattern[0] != '/' {
		return fmt.Errorf("%s: pattern must start with '/'", prefix)
	}
	if strings.HasSuffix(pattern, `\`) && !strings.HasSuffix(pattern, `\\`) {
		return fmt.Errorf(`%s: trailing unescaped '\' character`, prefix)
	}
	tokens, err := scan(pattern)
	if err != nil {
		return fmt.Errorf("%s: %w", prefix, err)
	}
	tree, err := parse(tokens)
	if err != nil {
		return fmt.Errorf("%s: %w", prefix, err)
	}
	if count := tree.NumVariants(); count > maxExpandedPatterns {
		return fmt.Errorf("%s: exceeded maximum number of expanded path patterns (%d): %d", prefix, maxExpandedPatterns, count)
	}
	p.original = pattern
	p.renderTree = tree
	return nil
}

// Match returns true if the path pattern matches the given path.
func (p *PathPattern) Match(path string) (bool, error) {
	return PathPatternMatches(p.original, path)
}

// MarshalJSON implements json.Marshaller for PathPattern.
func (p *PathPattern) MarshalJSON() ([]byte, error) {
	return []byte(p.original), nil
}

// UnmarshalJSON implements json.Unmarshaller for PathPattern.
func (p *PathPattern) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	return p.parse(s)
}

// NumVariants returns the total number of expanded path patterns for the
// given path pattern.
func (p *PathPattern) NumVariants() int {
	return p.renderTree.NumVariants()
}

func (p *PathPattern) RenderAllVariants(observe func(int, string)) {
	cleanThenObserve := func(i int, buf *bytes.Buffer) {
		expanded := buf.String()
		cleaned := cleanPattern(expanded)
		observe(i, cleaned)
	}
	RenderAllVariants(p.renderTree, cleanThenObserve)
}

var (
	duplicateSlashes    = regexp.MustCompile(`(^|[^\\])/+`)
	charsDoublestar     = regexp.MustCompile(`([^/\\])\*\*+`)
	doublestarChars     = regexp.MustCompile(`([^\\])\*\*+([^/])`)
	duplicateDoublestar = regexp.MustCompile(`/\*\*(/\*\*)+`) // relies on charsDoublestar running first
	starsAnyMaybeStars  = regexp.MustCompile(`([^\\])\*+(\?\**)+`)
)

func cleanPattern(pattern string) string {
	pattern = duplicateSlashes.ReplaceAllString(pattern, `${1}/`)
	pattern = charsDoublestar.ReplaceAllString(pattern, `${1}*`)
	pattern = doublestarChars.ReplaceAllString(pattern, `${1}*${2}`)
	pattern = duplicateDoublestar.ReplaceAllString(pattern, `/**`)
	pattern = starsAnyMaybeStars.ReplaceAllStringFunc(pattern, func(s string) string {
		deleteStars := func(r rune) rune {
			if r == '*' {
				return -1
			}
			return r
		}
		return strings.Map(deleteStars, s) + "*"
	})
	if strings.HasSuffix(pattern, "/**/*") {
		// Strip trailing "/*" from suffix
		return pattern[:len(pattern)-len("/*")]
	}
	return pattern
}

// PathPatternMatches returns true if the given pattern matches the given path.
//
// Paths to directories are received with trailing slashes, but we don't want
// to require the user to include a trailing '/' if they want to match
// directories (and end their pattern with `{,/}` if they want to match both
// directories and non-directories). Thus, we want to ensure that patterns
// without trailing slashes match paths with trailing slashes. However,
// patterns with trailing slashes should not match paths without trailing
// slashes.
//
// The doublestar package (v4.6.1) has special cases for patterns ending in
// `/**` and `/**/`: `/foo/**`, and `/foo/**/` both match `/foo` and `/foo/`.
// We want to override this behavior to make `/foo/**/` not match `/foo`.
// We also want to override doublestar to make `/foo` match `/foo/`.
func PathPatternMatches(pattern string, path string) (bool, error) {
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
