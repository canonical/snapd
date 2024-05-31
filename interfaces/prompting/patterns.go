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
// expansions which may be rendered into an expanded path pattern.
type patternComponent interface {
	// Next advances the pattern component to its next expansion.
	// If all expansions have been explored, resets to the initial expansion,
	// after which subsequent calls to Next continue to cycle through
	// expansions again. Returns true if the Next call resulted in the pattern
	// component being reset to its initial expansion.
	Next() bool
	// NumExpansions returns the number of expansions into which the pattern
	// component is able to be rendered.
	NumExpansions() int
	// render writes the current expansion of the pattern component to the
	// given buffer.
	render(*bytes.Buffer)
	// add appends the given pattern component to the receiver, which should
	// be a sequence or group.
	add(patternComponent)
}

// patternLiteral is a literal string which does not contain any groups.
type patternLiteral string

func (l patternLiteral) Next() bool {
	return true
}

func (l patternLiteral) NumExpansions() int {
	return 1
}

func (l patternLiteral) render(b *bytes.Buffer) {
	b.Write([]byte(l))
}

func (l patternLiteral) add(c patternComponent) {
	panic("cannot add to a pattern literal")
}

// patternSequence is a sequence of path components, which may themselves be
// either literals or groups, which are all concatenated when rendering.
type patternSequence []patternComponent

func (s patternSequence) Next() bool {
	for i := len(s) - 1; i >= 0; i-- {
		if !(s)[i].Next() {
			return false
		}
	}
	return true
}

func (s patternSequence) NumExpansions() int {
	count := 1
	for _, component := range s {
		count *= component.NumExpansions()
	}
	return count
}

func (s patternSequence) render(b *bytes.Buffer) {
	for _, component := range s {
		component.render(b)
	}
}

func (s *patternSequence) add(c patternComponent) {
	*s = append(*s, c)
}

// patternGroup is a list of options, one of which is rendered at a time.
// If an option has more than one expansion, Next advances to the next
// expansion of that option recursively before continuing on to the next
// option.
type patternGroup struct {
	options []patternComponent
	index   int
}

func (g *patternGroup) Next() bool {
	if !g.options[g.index].Next() {
		return false
	}
	g.index = (g.index + 1) % len(g.options)
	return g.index == 0
}

func (g *patternGroup) NumExpansions() int {
	count := 0
	for _, option := range g.options {
		count += option.NumExpansions()
	}
	return count
}

func (g *patternGroup) render(b *bytes.Buffer) {
	g.options[g.index].render(b)
}

func (g *patternGroup) add(c patternComponent) {
	g.options = append(g.options, c)
}

type componentStack []patternComponent

func (s *componentStack) push(x patternComponent) {
	*s = append(*s, x)
}

func (s *componentStack) pop() patternComponent {
	x := (*s)[len(*s)-1]
	*s = (*s)[:len(*s)-1]
	return x
}

func (s componentStack) peek() patternComponent {
	return s[len(s)-1]
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
	if err := pathPattern.parse(pattern); err != nil {
		return nil, err
	}
	return pathPattern, nil
}

// parse validates the given pattern and parses it into a PathPattern from
// which expanded path patterns can be iterated, overwriting the receiver.
func (p *PathPattern) parse(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("invalid path pattern: pattern has length 0")
	}
	if pattern[0] != '/' {
		return fmt.Errorf("invalid path pattern: pattern must start with '/': %q", pattern)
	}
	if strings.HasSuffix(pattern, `\`) && !strings.HasSuffix(pattern, `\\`) {
		return fmt.Errorf(`invalid path pattern: trailing unescaped '\' character: %q`, pattern)
	}
	// Whenever a new group is encountered, it will be the next item in the
	// current sequence, but we won't be finished with this sequence until the
	// group has been completed and any future components are pushed to the
	// sequence, so save the sequence on a stack.
	stack := componentStack{}
	// Create the root sequence and push it to the stack.
	rootSequence := &patternSequence{}
	stack.push(rootSequence)
	depth := 0
	reader := strings.NewReader(pattern)
	// index of first non-special rune after most recent special rune
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
			// Append final literal to the last sequence.
			literal := patternLiteral(pattern[currentStartIndex:])
			stack.peek().add(literal)
			break
		}
		nextIndex += size
		switch r {
		case '{':
			depth++
			if depth >= maxExpandedPatterns {
				return fmt.Errorf("invalid path pattern: nested group depth exceeded maximum number of expanded path patterns (%d): %q", maxExpandedPatterns, pattern)
			}
			// A new group has been spotted, so add the last literal to the
			// current sequence, then create and push a new group, then create
			// and push a new sequence, the latter of which will be the first
			// option in the new group.
			literal := patternLiteral(pattern[currentStartIndex:currentIndex])
			currentStartIndex = nextIndex
			stack.peek().add(literal)
			stack.push(&patternGroup{})
			stack.push(&patternSequence{})
		case ',':
			if depth == 0 {
				// Ignore commas outside of groups
				break
			}
			// A ',' marks the end of the current option in the group (the top
			// sequence on the stack), so add the last literal to it, then
			// complete that sequence by popping it and adding it to its parent
			// group, which is now the top of the stack, and start a new sequence
			// to represent the next option in the group.
			literal := patternLiteral(pattern[currentStartIndex:currentIndex])
			currentStartIndex = nextIndex
			stack.peek().add(literal)
			completedSequence := stack.pop()
			stack.peek().add(completedSequence)
			stack.push(&patternSequence{})
		case '}':
			depth--
			if depth < 0 {
				return fmt.Errorf("invalid path pattern: unmatched '}' character: %q", pattern)
			}
			// A '}' marks the end of the current group (and its final option)
			// so add the last literal to the top sequence on the stack, then
			// complete that sequence by popping it and adding it to its parent
			// group, which is now the top of the stack, the complete that group
			// by popping it and adding it to its parent sequence, which is
			// itself now the top of the stack.
			literal := patternLiteral(pattern[currentStartIndex:currentIndex])
			currentStartIndex = nextIndex
			stack.peek().add(literal)
			completedSequence := stack.pop()
			stack.peek().add(completedSequence)
			completedGroup := stack.pop()
			stack.peek().add(completedGroup)
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
	// The rootSequence is not the only component on the stack
	if count := rootSequence.NumExpansions(); count > maxExpandedPatterns {
		return fmt.Errorf("invalid path pattern: exceeded maximum number of expanded path patterns (%d): %q expands to %d patterns", maxExpandedPatterns, pattern, count)
	}
	if !doublestar.ValidatePattern(pattern) {
		return fmt.Errorf("invalid path pattern: %q", pattern)
	}
	p.original = pattern
	p.components = *rootSequence
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
	return p.parse(s)
}

// NumExpansions returns the total number of expanded path patterns for the
// given path pattern.
func (p *PathPattern) NumExpansions() int {
	return p.components.NumExpansions()
}

// Next renders the current path pattern expansion, advances it to the next
// expansion, then returns the rendered expansion along with true if the next
// expansion is the initial one, indicating that all expansions have been
// explored and rendered.
func (p *PathPattern) Next() (string, bool) {
	p.renderBuf.Truncate(0)
	p.components.render(&p.renderBuf)
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
