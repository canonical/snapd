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

package common

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	doublestar "github.com/bmatcuk/doublestar/v4"
)

var ErrNoPatterns = errors.New("no patterns given, cannot establish precedence")

var (
	// The default previously-expanded prefixes to which new patterns or
	// expanded groups are concatenated. This must be a slice containing the
	// empty string, since at the beginning of the pattern, we have only one
	// prefix to which to concatenate, and that prefix is the empty string.
	// Importantly, this cannot be an empty slice, since concatenating every
	// entry in an empty slice with every entry in a slice of expanded patterns
	// would again result in an empty slice.
	defaultPrefixes = []string{""}
)

// ExpandPathPattern expands all groups in the given path pattern.
//
// Groups are enclosed by '{' '}'. Returns a list of all the expanded path
// patterns, or an error if the given pattern is invalid.
func ExpandPathPattern(pattern string) ([]string, error) {
	if len(pattern) == 0 {
		return nil, fmt.Errorf(`invalid path pattern: pattern has length 0`)
	}
	if strings.HasSuffix(pattern, `\`) && !strings.HasSuffix(pattern, `\\`) {
		return nil, fmt.Errorf(`invalid path pattern: trailing unescaped '\' character: %q`, pattern)
	}
	reader := strings.NewReader(pattern)
	currPrefixes := defaultPrefixes
	currLiteralStart := 0
	for {
		r, _, err := reader.ReadRune()
		if err != nil {
			// No more runes
			break
		}
		if r == '\\' {
			// Skip next rune.
			reader.ReadRune() // Since suffix is not '\\', must have next rune
			continue
		}
		if r == '}' {
			return nil, fmt.Errorf(`invalid path pattern: unmatched '}' character: %q`, pattern)
		}
		if r != '{' {
			continue
		}
		// Saw start of new group, so get the string from currLiteralStart to
		// the opening '{' of the new group. Do this before expanding.
		infix := prevStringFromIndex(reader, currLiteralStart)
		groupExpanded, err := expandPathPatternRecursively(reader)
		if err != nil {
			return nil, err
		}
		// Now that group has been expanded, record index of next rune in reader
		currLiteralStart = indexOfNextRune(reader)
		newExpanded := make([]string, 0, len(currPrefixes)*len(groupExpanded))
		for _, prefix := range currPrefixes {
			for _, suffix := range groupExpanded {
				newExpanded = append(newExpanded, prefix+infix+suffix)
			}
		}
		currPrefixes = newExpanded
	}
	expanded := currPrefixes
	if len(expanded) == 1 && expanded[0] == "" {
		// Didn't expand any groups, so return whole pattern.
		return []string{cleanPattern(pattern)}, nil
	}
	// Append trailing literal string, if any, to all previously-expanded
	// patterns, and clean the resulting patterns.
	alreadySeen := make(map[string]bool, len(expanded))
	uniqueExpanded := make([]string, 0, len(expanded))
	suffix := pattern[currLiteralStart:]
	for _, prefix := range expanded {
		cleaned := cleanPattern(prefix + suffix)
		if alreadySeen[cleaned] {
			continue
		}
		alreadySeen[cleaned] = true
		uniqueExpanded = append(uniqueExpanded, cleaned)
	}
	return uniqueExpanded, nil
}

// Return the substring from the given start index until the index of the
// previous rune read by the reader.
func prevStringFromIndex(reader *strings.Reader, startIndex int) string {
	if err := reader.UnreadRune(); err != nil {
		panic(err) // should only occur if used incorrectly internally
	}
	defer reader.ReadRune() // re-read rune so index is unchanged
	currIndex := indexOfNextRune(reader)
	buf := make([]byte, currIndex-startIndex)
	reader.ReadAt(buf, int64(startIndex))
	return string(buf)
}

// Return the byte index of the next rune in the reader.
func indexOfNextRune(reader *strings.Reader) int {
	index, _ := reader.Seek(0, io.SeekCurrent)
	return int(index)
}

// Expands the contents of a group in the path pattern read by the given reader
// until a '}' is seen.
//
// The reader current position of the reader should be the rune immediately
// following the opening '{' character of the group.
//
// Returns the list of expanded strings. Whenever a ',' character is
// encountered, cuts off the current sub-pattern and begins a new one.
// Any '\'-escaped '{', ',', and '}' characters are treated as literals.
//
// If the pattern terminates before a non-escaped '}' is seen, returns an error.
func expandPathPatternRecursively(reader *strings.Reader) ([]string, error) {
	// Record total list of expanded patterns, to which other lists are appended
	expanded := []string{}
	alreadySeenExpanded := make(map[string]bool)
	// Within the current group option, record the current list of previously-
	// expanded prefixes, and the start index of the subpattern following the
	// most recent group.
	currPrefixes := defaultPrefixes
	currSubpatternStart := indexOfNextRune(reader)
	for {
		r, _, err := reader.ReadRune()
		if err != nil {
			break
		}
		if r == '\\' {
			// Skip next rune.
			reader.ReadRune() // Since suffix is not '\\', must have next rune
			continue
		}
		if r == '{' {
			infix := prevStringFromIndex(reader, currSubpatternStart)
			groupExpanded, err := expandPathPatternRecursively(reader)
			if err != nil {
				return nil, err
			}
			// Now that group has been expanded, record index of next rune in reader
			currSubpatternStart = indexOfNextRune(reader)
			alreadySeen := make(map[string]bool, len(currPrefixes)*len(groupExpanded))
			newPrefixes := make([]string, 0, len(currPrefixes)*len(groupExpanded))
			for _, prefix := range currPrefixes {
				for _, suffix := range groupExpanded {
					newPrefix := prefix + infix + suffix
					if alreadySeen[newPrefix] {
						continue
					}
					alreadySeen[newPrefix] = true
					newPrefixes = append(newPrefixes, newPrefix)
				}
			}
			currPrefixes = newPrefixes
			continue
		}
		if r == ',' || r == '}' {
			suffix := prevStringFromIndex(reader, currSubpatternStart)
			newExpanded := make([]string, len(expanded), len(expanded)+len(currPrefixes))
			copy(newExpanded, expanded)
			expanded = newExpanded
			for _, prefix := range currPrefixes {
				newSubPattern := prefix + suffix
				if alreadySeenExpanded[newSubPattern] {
					continue
				}
				alreadySeenExpanded[newSubPattern] = true
				expanded = append(expanded, newSubPattern)
			}
			currPrefixes = defaultPrefixes
			currSubpatternStart = indexOfNextRune(reader)
		}
		if r == '}' {
			return expanded, nil
		}
	}
	// Group missing closing '}' character, so return an error.
	origPatternBuf := make([]byte, reader.Size())
	reader.ReadAt(origPatternBuf, 0)
	return nil, fmt.Errorf(`invalid path pattern: unmatched '{' character: %q`, string(origPatternBuf))
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

type priorityType int

const (
	worstPriority priorityType = iota
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

// GetHighestPrecedencePattern determines which of the given path patterns is
// the most specific.
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
func GetHighestPrecedencePattern(patterns []string) (string, error) {
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
	currIndex := indexOfNextRune(reader)
	_, err := reader.ReadAt(rBytes, int64(currIndex))
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

// ValidatePathPattern returns nil if the pattern is valid, otherwise an error.
func ValidatePathPattern(pattern string) error {
	if !doublestar.ValidatePattern(pattern) {
		return fmt.Errorf("invalid path pattern: %q", pattern)
	}
	if pattern == "" || pattern[0] != '/' {
		return fmt.Errorf("invalid path pattern: must start with '/': %q", pattern)
	}
	maxNumGroups := 10
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
			if totalGroups > maxNumGroups {
				return fmt.Errorf("invalid path pattern: exceeded maximum number of groups (%d): %q", maxNumGroups, pattern)
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
	return nil
}

// PathPatternMatch returns true if the given pattern matches the given path.
//
// The pattern should not contain groups, and should likely have been an output
// of ExpandPathPattern.
//
// Paths to directories are received with trailing slashes, but we don't want
// to require the user to including a trailing '/' if they want to match
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
	// Try again with a '/' appended to the pattern, so patterns like `/foo`
	// match paths like `/foo/`.
	return doublestar.Match(pattern+"/", path)
}
