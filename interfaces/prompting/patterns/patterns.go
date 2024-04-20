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
	"io"
	"regexp"
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
// pattern and renders each one into a string which is then passed into the
// given observe closure, along with the index of the variant.
//
// The given observe closure should perform some action with the rendered
// variant, such as adding it to a data structure.
func (p *PathPattern) RenderAllVariants(observe func(index int, variant string)) {
	cleanThenObserve := func(i int, variant string) {
		cleaned := cleanPattern(variant)
		observe(i, cleaned)
	}
	renderAllVariants(p.renderTree, cleanThenObserve)
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

type priorityType int

const (
	lastPriority priorityType = iota
	priorityGlobDoublestar
	priorityTerminalDoublestar
	priorityDoublestar
	priorityGlob
	prioritySinglestar
	prioritySingleChar
	priorityTerminated
	priorityLiteral
)

type nextPatternsContainer struct {
	currPriority     priorityType
	nextPatternsList []*strings.Reader
}

func (np *nextPatternsContainer) addWithPriority(priority priorityType, reader *strings.Reader) {
	if priority < np.currPriority {
		return
	}
	if priority > np.currPriority {
		np.nextPatternsList = np.nextPatternsList[:0]
		np.currPriority = priority
	}
	np.nextPatternsList = append(np.nextPatternsList, reader)
}

func (np *nextPatternsContainer) nextPatterns() []*strings.Reader {
	return np.nextPatternsList
}

// HighestPrecedencePattern determines which of the given path patterns is the
// most specific.
//
// Assumes that all of the given patterns satisfy ValidatePathPattern(), so this
// is not verified as part of this function. Additionally, also assumes that the
// patterns have been previously expanded using ExpandPathPattern(), so there
// are no groups in any of the patterns.
//
// Below are some sample patterns, in order of precedence, though precedence is
// only guaranteed between two patterns which may match the same path:
//
//	# literals
//	- /foo/bar/baz
//	- /foo/bar/
//	# terminated
//	- /foo/bar
//	# any single character
//	- /foo/bar?baz
//	- /foo/bar?
//	- /foo/bar?/
//	# singlestars
//	- /foo/bar/*/baz
//	- /foo/bar/*/
//	- /foo/bar/*/*baz
//	- /foo/bar/*/*
//	- /foo/bar/*
//	- /foo/bar/*/**
//	# glob
//	- /foo/bar*baz
//	- /foo/bar*/baz
//	- /foo/bar*/baz/**
//	- /foo/bar*/
//	- /foo/bar*/*baz
//	- /foo/bar*/*/baz
//	- /foo/bar*/*/
//	- /foo/bar*/*
//	- /foo/bar*
//	# doublestars
//	- /foo/bar/**/baz
//	- /foo/bar/**/*baz/
//	- /foo/bar/**/*baz
//	# terminal doublestar
//	- /foo/bar/**/        # These are tough... usually, /foo/bar/**/ would have precedence over
//	- /foo/bar/**/*       # precedence over /foo/bar/**/*baz, but in this case,
//	- /foo/bar/**         # the trailing *baz adds more specificity.
//	# glob with immediate doublestar
//	- /foo/bar*/**/baz
//	- /foo/bar*/**/
//	- /foo/bar*/**
func HighestPrecedencePattern(patterns []string) (string, error) {
	if len(patterns) == 0 {
		return "", ErrNoPatterns
	}
	alreadySeen := make(map[string]bool, len(patterns))
	patternForReader := make(map[*strings.Reader]string, len(patterns))
	remainingPatterns := make([]*strings.Reader, 0, len(patterns))
	for _, pattern := range patterns {
		if alreadySeen[pattern] {
			continue
		}
		alreadySeen[pattern] = true
		if strings.HasSuffix(pattern, `\`) && !strings.HasSuffix(pattern, `\\`) {
			return "", fmt.Errorf(`invalid path pattern: trailing unescaped '\' character: %q`, pattern)
		}
		reader := strings.NewReader(pattern)
		patternForReader[reader] = pattern
		remainingPatterns = append(remainingPatterns, reader)
	}
	for len(remainingPatterns) > 1 {
		nextPatterns := nextPatternsContainer{}
		for _, reader := range remainingPatterns {
			r, _, err := reader.ReadRune()
			if err != nil {
				// No runes remaining, so pattern is terminal.
				nextPatterns.addWithPriority(priorityTerminated, reader)
				continue
			}
			// Check for '?' and '*' before '\\', since "\\*" is literal '*', etc.
			if r == '?' {
				nextPatterns.addWithPriority(prioritySingleChar, reader)
				continue
			}
			if r == '*' {
				if nextBytesEqual(reader, "/**") {
					// Next parts of pattern are "*/**".
					nextPatterns.addWithPriority(priorityGlobDoublestar, reader)
					continue
				}
				nextPatterns.addWithPriority(priorityGlob, reader)
				continue
			}
			if r == '\\' {
				// Since suffix is not unescaped '\\', must have next rune.
				r, _, _ = reader.ReadRune()
			}
			// Can safely check for '/' after '\\', since it is '/' either way
			if r != '/' || !nextRuneEquals(reader, '*') {
				// Next parts of pattern are not "/*" or "/**"
				nextPatterns.addWithPriority(priorityLiteral, reader)
				continue
			}
			// Next part of the pattern must be "/*".
			// This pattern will only be included in the next round if all the
			// other patterns also have "/*" next, so it's fine to remove that.
			reader.ReadRune()             // Discard first '*' after '/'.
			r, _, err = reader.ReadRune() // Get next rune after "/*".
			if err != nil || r != '*' {
				// Next parts of pattern are not "/**"
				reader.UnreadRune() // Discard error, which occurred if EOF.
				nextPatterns.addWithPriority(prioritySinglestar, reader)
				continue
			}
			// Next part of pattern must be "/**".
			if reader.Len() == 0 || (reader.Len() == 1 && nextRuneEquals(reader, '/')) {
				// Pattern must terminate with "/**" or "/**/".
				// We don't consider patterns terminating with "/**/*" or "/**/**"
				// here, since these are equivalent to "/**" and are replaced
				// as such by ExpandPathPatterns.
				// Terminal "/**/*/" is more selective, and is not matched here.
				nextPatterns.addWithPriority(priorityTerminalDoublestar, reader)
				continue
			}
			// Pattern has non-terminal "/**".
			nextPatterns.addWithPriority(priorityDoublestar, reader)
		}
		remainingPatterns = nextPatterns.nextPatterns()
	}
	reader := remainingPatterns[0]
	pattern := patternForReader[reader]
	return pattern, nil
}

// Return true if the next rune in the reader equals the given rune.
func nextRuneEquals(reader *strings.Reader, r rune) bool {
	ch, _, err := reader.ReadRune()
	if err != nil {
		return false
	}
	defer reader.UnreadRune()
	return ch == r
}

// Return true if the next bytes in the reader equal the given string.
func nextBytesEqual(reader *strings.Reader, s string) bool {
	if reader.Len() < len(s) {
		return false
	}
	sBytes := []byte(s)
	rBytes := make([]byte, len(sBytes))
	currIndex, _ := reader.Seek(0, io.SeekCurrent)
	_, err := reader.ReadAt(rBytes, currIndex)
	if err != nil {
		return false
	}
	for i := range sBytes {
		if rBytes[i] != sBytes[i] {
			return false
		}
	}
	return true
}
