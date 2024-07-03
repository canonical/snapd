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
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	doublestar "github.com/bmatcuk/doublestar/v4"
)

var ErrNoPatterns = errors.New("cannot establish precedence: no patterns given")

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

// RenderAllVariants enumerates every alternative for each group in the path
// pattern and renders each one into a PatternVariant which is then passed into
// the given observe closure, along with the index of the variant.
//
// The given observe closure should perform some action with the rendered
// variant, such as adding it to a data structure.
func (p *PathPattern) RenderAllVariants(observe func(index int, variant *PatternVariant)) {
	renderAllVariants(p.renderTree, observe)
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
	// If pattern ends in '/', it only matches paths which also end in '/'
	if strings.HasSuffix(pattern, "/") && !strings.HasSuffix(path, "/") {
		return false, nil
	}
	matched, err := doublestar.Match(pattern, path)
	if err != nil {
		// Pattern should not be malformed, since it was rendered internally
		return false, err
	}
	if matched {
		return true, nil
	}
	// If pattern already has trailing '/', don't try to match with additional
	// trailing '/'.
	if strings.HasSuffix(pattern, "/") {
		return false, nil
	}
	// Try again with a '/' appended to the pattern, so patterns like `/foo`
	// match paths like `/foo/`.
	return doublestar.Match(pattern+"/", path)
}

// HighestPrecedencePattern determines which of the given path patterns is the
// most specific, and thus has the highest precedence.
//
// Precedence is only defined between patterns which match the same path.
// Precedence is determined according to which pattern places the earliest and
// greatest restriction on the path.
func HighestPrecedencePattern(patterns []*PatternVariant) (*PatternVariant, error) {
	switch len(patterns) {
	case 0:
		return nil, ErrNoPatterns
	case 1:
		return patterns[0], nil
	}

	currHighest := patterns[0]
	for _, contender := range patterns[1:] {
		if currHighest.Compare(contender) < 0 {
			currHighest = contender
		}
	}
	return currHighest, nil
}
