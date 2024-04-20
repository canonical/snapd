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
	"io"
	"regexp"
	"strings"

	doublestar "github.com/bmatcuk/doublestar/v4"
)

// Limit the number of expanded path patterns for a particular pattern.
// When fully expanded, the number of patterns for a given unexpanded pattern
// may not exceed this limit.
const maxExpandedPatterns = 1000

// The default previously-expanded prefixes to which new patterns or expanded
// groups are concatenated. This must be a slice containing the empty string,
// since at the beginning of the pattern, we have only one prefix to which to
// concatenate, and that prefix is the empty string. Importantly, this cannot
// be an empty slice, since concatenating every entry in an empty slice with
// every entry in a slice of expanded patterns would again result in an empty
// slice.
var defaultPrefixes = []string{""}

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
		newCount := len(currPrefixes) * len(groupExpanded)
		if newCount > maxExpandedPatterns {
			return nil, fmt.Errorf("invalid path pattern: exceeded maximum number of expanded path patterns (%d): %q", maxExpandedPatterns, pattern)
		}
		newExpanded := make([]string, 0, newCount)
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
// until a '}' is seen. Also takes the current number of groups seen prior to
// the group which this function call will expand.
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
			newCount := len(currPrefixes) * len(groupExpanded)
			if newCount > maxExpandedPatterns {
				return nil, fmt.Errorf("invalid path pattern: exceeded maximum number of expanded path patterns (%d): %q", maxExpandedPatterns, origPatternFromReader(reader))
			}
			alreadySeen := make(map[string]bool, newCount)
			newPrefixes := make([]string, 0, newCount)
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
			newCount := len(expanded) + len(currPrefixes)
			if newCount > maxExpandedPatterns {
				return nil, fmt.Errorf("invalid path pattern: exceeded maximum number of expanded path patterns (%d): %q", maxExpandedPatterns, origPatternFromReader(reader))
			}
			newExpanded := make([]string, len(expanded), newCount)
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
	return nil, fmt.Errorf(`invalid path pattern: unmatched '{' character: %q`, origPatternFromReader(reader))
}

func origPatternFromReader(reader *strings.Reader) string {
	origPatternBuf := make([]byte, reader.Size())
	reader.ReadAt(origPatternBuf, 0)
	return string(origPatternBuf)
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

type countStack []int

func (c *countStack) push(x int) {
	*c = append(*c, x)
}

func (c *countStack) pop() int {
	x := (*c)[len(*c)-1]
	*c = (*c)[:len(*c)-1]
	return x
}

// ValidatePathPattern returns nil if the pattern is valid, otherwise an error.
func ValidatePathPattern(pattern string) error {
	if pattern == "" || pattern[0] != '/' {
		return fmt.Errorf("invalid path pattern: must start with '/': %q", pattern)
	}
	depth := 0
	var currentGroupStack countStack
	var currentOptionStack countStack
	// Final currentOptionCount will be total expanded patterns for full pattern
	currentGroupCount := 0
	currentOptionCount := 1
	reader := strings.NewReader(pattern)
	for {
		r, _, err := reader.ReadRune()
		if err != nil {
			// No more runes
			break
		}
		switch r {
		case '{':
			depth++
			if depth >= maxExpandedPatterns {
				return fmt.Errorf("invalid path pattern: nested group depth exceeded maximum number of expanded path patterns (%d): %q", maxExpandedPatterns, pattern)
			}
			currentGroupStack.push(currentGroupCount)
			currentOptionStack.push(currentOptionCount)
			currentGroupCount = 0
			currentOptionCount = 1
		case ',':
			if depth == 0 {
				// Ignore commas outside of groups
				break
			}
			currentGroupCount += currentOptionCount
			currentOptionCount = 1
		case '}':
			depth--
			if depth < 0 {
				return fmt.Errorf("invalid path pattern: unmatched '}' character: %q", pattern)
			}
			currentGroupCount += currentOptionCount
			currentOptionCount = currentOptionStack.pop() // option count of parent
			currentOptionCount *= currentGroupCount       // parent option count * current group count
			currentGroupCount = currentGroupStack.pop()   // group count of parent
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
	if currentOptionCount > maxExpandedPatterns {
		return fmt.Errorf("invalid path pattern: exceeded maximum number of expanded path patterns (%d): %q expands to %d patterns", maxExpandedPatterns, pattern, currentOptionCount)
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
