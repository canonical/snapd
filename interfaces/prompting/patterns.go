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

// patternComponent is a component of a path pattern which has one or more
// configurations which may be rendered into an expanded path pattern.
type patternComponent interface {
	// Next advances the pattern component to its next configuration.
	// If all configurations have been explored, resets to the initial
	// configuration, after which subsequent calls to Next continue to cycle
	// through configurations again. Returns true if the Next call resulted in
	// the pattern component being reset to its initial configuration.
	Next() bool
	// NumConfigurations returns the number of configurations into which the
	// pattern component is able to be set.
	NumConfigurations() int
	// Render writes the current configuration of the pattern component to the
	// given buffer.
	Render(*bytes.Buffer)
}

// patternLiteral is a literal string which does not contain any groups.
type patternLiteral string

func (l patternLiteral) Next() bool {
	return true
}

func (l patternLiteral) NumConfigurations() int {
	return 1
}

func (l patternLiteral) Render(b *bytes.Buffer) {
	b.Write([]byte(l))
}

// patternSequence is a sequence of path components, which may themselves be
// either literals or groups, which are all concatenated when rendering.
type patternSequence []patternComponent

func newPatternSequence() patternSequence {
	var s patternSequence
	return s
}

func (s *patternSequence) add(c patternComponent) {
	*s = append(*s, c)
}

func (s patternSequence) Next() bool {
	for i := len(s) - 1; i >= 0; i-- {
		if !(s)[i].Next() {
			return false
		}
	}
	return true
}

func (s patternSequence) NumConfigurations() int {
	count := 1
	for _, component := range s {
		count *= component.NumConfigurations()
	}
	return count
}

func (s patternSequence) Render(b *bytes.Buffer) {
	for _, component := range s {
		component.Render(b)
	}
}

// patternGroup is a list of options, one of which is rendered at a time.
// If an option has more than one configuration, Next advances the
// configuration of that option recursively before continuing on to the next
// option.
type patternGroup struct {
	options []patternComponent
	index   int
}

func newPatternGroup() *patternGroup {
	var s patternGroup
	return &s
}

func (g *patternGroup) add(c patternComponent) {
	g.options = append(g.options, c)
}

func (g *patternGroup) Next() bool {
	if !g.options[g.index].Next() {
		return false
	}
	g.index = (g.index + 1) % len(g.options)
	return g.index == 0
}

func (g *patternGroup) NumConfigurations() int {
	count := 0
	for _, option := range g.options {
		count += option.NumConfigurations()
	}
	return count
}

func (g *patternGroup) Render(b *bytes.Buffer) {
	g.options[g.index].Render(b)
}

type sequenceStack []patternSequence

func (s *sequenceStack) push(x patternSequence) {
	*s = append(*s, x)
}

func (s *sequenceStack) pop() patternSequence {
	x := (*s)[len(*s)-1]
	*s = (*s)[:len(*s)-1]
	return x
}

type groupStack []*patternGroup

func (s *groupStack) push(x *patternGroup) {
	*s = append(*s, x)
}

func (s *groupStack) pop() *patternGroup {
	x := (*s)[len(*s)-1]
	*s = (*s)[:len(*s)-1]
	return x
}

// PathPattern is an iterator which yields expanded path patterns.
type PathPattern struct {
	original   string
	components patternSequence
	renderBuf  bytes.Buffer
}

// ParsePathPattern validates the given pattern and parses it into a PathPattern
// from which expanded path patterns can be iterated, and returns it.
func ParsePathPattern(pattern string) (*PathPattern, error) {
	pathPattern := &PathPattern{}
	if err := pathPattern.Parse(pattern); err != nil {
		return nil, err
	}
	return pathPattern, nil
}

// Parse validates the given pattern and parses it into a PathPattern from
// which expanded path patterns can be iterated, overwriting the receiver.
func (p *PathPattern) Parse(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("invalid path pattern: pattern has length 0")
	}
	if pattern[0] != '/' {
		return fmt.Errorf("invalid path pattern: pattern must start with '/': %q", pattern)
	}
	if strings.HasSuffix(pattern, `\`) && !strings.HasSuffix(pattern, `\\`) {
		return fmt.Errorf(`invalid path pattern: trailing unescaped '\' character: %q`, pattern)
	}
	var currentSequenceStack sequenceStack
	var currentGroupStack groupStack
	currentSequence := newPatternSequence()
	currentGroup := newPatternGroup()
	depth := 0
	reader := strings.NewReader(pattern)
	currentStartIndex := 0
	// index of current rune
	currentIndex := 0
	// index of next rune
	nextIndex := 0
	for {
		currentIndex = nextIndex
		r, size, err := reader.ReadRune()
		if err != nil {
			// No more runes.
			// Append final literal, which may be empty.
			currentLiteral := patternLiteral(pattern[currentStartIndex:])
			currentSequence.add(currentLiteral)
			break
		}
		nextIndex += size
		switch r {
		case '{':
			depth++
			if depth >= maxExpandedPatterns {
				return fmt.Errorf("invalid path pattern: nested group depth exceeded maximum number of expanded path patterns (%d): %q", maxExpandedPatterns, pattern)
			}
			currentLiteral := patternLiteral(pattern[currentStartIndex:currentIndex])
			currentStartIndex = nextIndex
			currentSequence.add(currentLiteral)
			currentSequenceStack.push(currentSequence)
			currentGroupStack.push(currentGroup)
			currentSequence = newPatternSequence()
			currentGroup = newPatternGroup()
		case ',':
			if depth == 0 {
				// Ignore commas outside of groups
				break
			}
			currentLiteral := patternLiteral(pattern[currentStartIndex:currentIndex])
			currentStartIndex = nextIndex
			currentSequence.add(currentLiteral)
			currentGroup.add(currentSequence)
			currentSequence = newPatternSequence()
		case '}':
			depth--
			if depth < 0 {
				return fmt.Errorf("invalid path pattern: unmatched '}' character: %q", pattern)
			}
			currentLiteral := patternLiteral(pattern[currentStartIndex:currentIndex])
			currentStartIndex = nextIndex
			currentSequence.add(currentLiteral)
			currentGroup.add(currentSequence)
			// done with current sequence
			currentSequence = currentSequenceStack.pop()
			currentSequence.add(currentGroup)
			// done with current group
			currentGroup = currentGroupStack.pop()
		case '\\':
			// Skip next rune, already verified can't have trailing '/'
			_, size, _ = reader.ReadRune()
			nextIndex += size
		case '[', ']':
			return fmt.Errorf("invalid path pattern: cannot contain unescaped '[' or ']' character: %q", pattern)
		}
	}
	if depth != 0 {
		return fmt.Errorf("invalid path pattern: unmatched '{' character: %q", pattern)
	}
	// currentSequence is sequence for complete path
	if count := currentSequence.NumConfigurations(); count > maxExpandedPatterns {
		return fmt.Errorf("invalid path pattern: exceeded maximum number of expanded path patterns (%d): %q expands to %d patterns", maxExpandedPatterns, pattern, count)
	}
	if !doublestar.ValidatePattern(pattern) {
		return fmt.Errorf("invalid path pattern: %q", pattern)
	}
	p.original = pattern
	p.components = currentSequence
	return nil
}

// String returns the original path pattern string, without group expansion.
func (p *PathPattern) String() string {
	return p.original
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
	return p.Parse(s)
}

// NumConfigurations returns the total number of expanded path patterns for the
// given path pattern.
func (p *PathPattern) NumConfigurations() int {
	return p.components.NumConfigurations()
}

// Next renders the current path pattern expansion, advances it to the next
// configuration, then returns the rendered expansion along with true if the
// next configuration is the initial one, indicating that all expansions have
// been explored and returned.
func (p *PathPattern) Next() (string, bool) {
	p.renderBuf.Truncate(0)
	p.components.Render(&p.renderBuf)
	expansion := p.renderBuf.String()
	cleaned := cleanPattern(expansion)
	finished := p.components.Next()
	return cleaned, finished
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
