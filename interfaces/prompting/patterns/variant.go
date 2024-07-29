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
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type componentType int

// Component types in order from lowest to highest precedence.
//
// A literal exactly matches the next non-zero number of characters, so it has
// the highest precedence. The '?' character matches exactly one non-separator
// character, so it has precedence over globstars; the only way a '?' can match
// a path at the same component position as another lower-precedence component
// is if the '?' were to occur after a '*' (and zero or more '?'s), and if this
// were the case, then the '?' would give additional information about the
// length of the path component matched by the '*', so it is higher precedence
// than the separator or terminal following the '*'. Next, a separator ('/') is
// like a literal, except that when following a '*', it precludes any more
// information being given about the length or content of the path prior to the
// separator (e.g. "/foo*bar/baz" clearly has precedence over "/foo*/baz"), so
// '/' has lower precedence than literals or '?' but higher than the rest. Next,
// terminals match when there is no path left, so they have precedence over all
// the variable-length component types, which may match zero or more characters.
// The next three component types relate to doublestars, which always follow a
// '/' character, and may match zero or more characters; in order to know
// whether a '/' is followed by a "**" or not without looking ahead in the list
// of components, we group these components together, along with their suffix
// when relevant: the non-terminal "/**" must be followed by a component which
// gives more information about the matching path, so it has the highest
// precedence of the three doublestar components; the terminal "/**/" component
// means that the variant only matches directories, while the terminal "/**"
// component can match files or directories, so the former has precedence over
// the latter. Lastly, '*' has the lowest precedence, since all other component
// types begin with more information about the length or content of the next
// characters in the path: "/foo/**" has precedence over "/foo*" since the
// former matches "/foo" exactly or a path in the "/foo" directory, while
// "/foo*" matches any path which happens to begin with "/foo".
const (
	compUnset componentType = iota
	compGlobstar
	compSeparatorDoublestarTerminal          // need to bundle separator and terminal marker with /**
	compSeparatorDoublestarSeparatorTerminal // need to bundle separators and terminal marker with /**/
	compSeparatorDoublestar
	compTerminal // marker of end of pattern.
	compSeparator
	compAnySingle // ? has precedence over / so that /foo*?/ has precedence over /foo*/
	compLiteral
)

type component struct {
	compType componentType
	compText string
}

// String returns the globstar-style pattern string associated with the given
// component.
func (c component) String() string {
	switch c.compType {
	case compGlobstar:
		return "*"
	case compSeparatorDoublestarTerminal:
		return "/**"
	case compSeparatorDoublestarSeparatorTerminal:
		return "/**/"
	case compSeparatorDoublestar:
		return "/**"
	case compTerminal: // end of pattern
		return ""
	case compSeparator:
		return "/"
	case compAnySingle:
		return "?"
	case compLiteral:
		return c.compText
	}
	return "###ERR_UNKNOWN_COMPONENT_TYPE###" // Should not occur
}

// componentRegex returns a regular expression corresponding to the bash-style
// globstar matching behavior of the receiving component.
//
// For example, "*" matches any non-separator characters, so we return the regex
// `((?:[^/]|\\/)*)` for the globstar component type.
//
// The returned regexps should each be enclosed in a capturing group with no
// capturing groups within. This allows a single regex to be constructed for a
// given pattern variant by concatenating all the component regular expressions
// together, and the resulting regex has exactly one capturing group for each
// component, in order.
func (c component) componentRegex() string {
	switch c.compType {
	case compGlobstar:
		return `((?:[^/]|\\/)*)`
	case compSeparatorDoublestarTerminal:
		return `((?:/.+)?/?)`
	case compSeparatorDoublestarSeparatorTerminal:
		return `((?:/.+)?/)`
	case compSeparatorDoublestar:
		return `((?:/.+)?)`
	case compTerminal:
		return `(/?)`
	case compSeparator:
		return `(/)`
	case compAnySingle:
		return `([^/])` // does escaped '/' (e.g. `\\/`) count as one character?
	case compLiteral:
		return `(` + regexp.QuoteMeta(unescapeLiteral(c.compText)) + `)`
	}
	return `()`
}

var escapeFinder = regexp.MustCompile(`\\(.)`)

// unescapeLiteral removes any `\` characters which are used to escape another
// character. Note that escaped `\` characters are not removed, since they are
// not acting as an escape character in those instances. That is, `\\` is
// reduced to `\`.
func unescapeLiteral(literal string) string {
	return escapeFinder.ReplaceAllString(literal, "${1}")
}

type PatternVariant struct {
	variant    string
	components []component
	regex      *regexp.Regexp
}

// String returns the rendered string associated with the pattern variant.
func (v PatternVariant) String() string {
	return v.variant
}

// ParsePatternVariant parses a rendered variant string into a PatternVariant
// whose precedence can be compared against others.
func ParsePatternVariant(variant string) (PatternVariant, error) {
	var components []component
	var runes []rune

	// prevComponentsAre returns true if the most recent components have types
	// matching the given target.
	prevComponentsAre := func(target []componentType) bool {
		if len(components) < len(target) {
			return false
		}
		for i, t := range target {
			if components[len(components)-len(target)+i].compType != t {
				return false
			}
		}
		return true
	}

	// addGlobstar adds a globstar to the components if the previous component
	// was not a globstar or a doublestar.
	addGlobstar := func() {
		if !prevComponentsAre([]componentType{compGlobstar}) && !prevComponentsAre([]componentType{compSeparatorDoublestar}) {
			components = append(components, component{compType: compGlobstar})
		}
	}

	// reducePrevDoublestar checks if the most recent component was a
	// doublestar, and if it was, replaces it with a globstar '*'.
	// This is necessary because a doublestar followed by anything except a
	// separator is treated as a globstar '*' instead.
	reducePrevDoublestar := func() {
		if prevComponentsAre([]componentType{compSeparatorDoublestar}) {
			// SeparatorDoublestar followed by anything except separator should
			// replaced by a separator '/' and globstar '*'.
			components[len(components)-1] = component{compType: compSeparator}
			components = append(components, component{compType: compGlobstar})
		}
	}

	// consumeText writes any accumulated runes as a literal component.
	consumeText := func() {
		if len(runes) > 0 {
			reducePrevDoublestar()
			components = append(components, component{compType: compLiteral, compText: string(runes)})
			runes = nil
		}
	}

	preparedVariant := prepareVariantForParsing(variant)

	rr := strings.NewReader(preparedVariant)
	for {
		r, _, err := rr.ReadRune()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			// Should not occur, err is only set if no rune available to read
			return PatternVariant{}, fmt.Errorf("internal error: failed to read rune while scanning variant: %w", err)
		}

		switch r {
		case '/':
			consumeText()
			if prevComponentsAre([]componentType{compSeparatorDoublestar, compSeparator, compGlobstar}) {
				// Replace previous /**/* with /*/** before adding separator
				components[len(components)-3] = component{compType: compSeparator}
				components[len(components)-2] = component{compType: compGlobstar}
				components[len(components)-1] = component{compType: compSeparatorDoublestar}
			}
			if !prevComponentsAre([]componentType{compSeparator}) {
				// Collapse repeated separators
				components = append(components, component{compType: compSeparator})
			}
		case '?':
			reducePrevDoublestar()
			consumeText()
			if prevComponentsAre([]componentType{compGlobstar}) {
				// Insert '?' before previous '*'
				components[len(components)-1] = component{compType: compAnySingle}
				components = append(components, component{compType: compGlobstar})
			} else {
				components = append(components, component{compType: compAnySingle})
			}
		case '⁑':
			consumeText()
			if prevComponentsAre([]componentType{compSeparatorDoublestar, compSeparator}) {
				// Reduce /**/** to simply /** by removing most recent separator
				components = components[:len(components)-1]
			} else if prevComponentsAre([]componentType{compSeparator}) {
				// Replace previous separator with separatorDoublestar
				components[len(components)-1] = component{compType: compSeparatorDoublestar}
			} else {
				// Reduce to * since previous component is not a separator
				addGlobstar()
			}
		case '*':
			reducePrevDoublestar()
			consumeText()
			addGlobstar()
		case '\\':
			r2, _, err := rr.ReadRune()
			if err != nil {
				// Should be impossible, we just rendered this variant
				return PatternVariant{}, errors.New(`internal error: trailing unescaped '\' character`)
			}
			switch r2 {
			case '*', '?', '[', ']', '{', '}', '\\':
				runes = append(runes, r, r2)
			default:
				// do not add r to runes if it's unnecessary
				runes = append(runes, r2)
			}
		case '[', ']', '{', '}':
			// Should be impossible, we just rendered this variant
			return PatternVariant{}, fmt.Errorf(`internal error: unexpected unescaped '%v' character`, r)
		default:
			runes = append(runes, r)
		}
	}

	consumeText()

	if prevComponentsAre([]componentType{compSeparatorDoublestar, compSeparator, compGlobstar}) {
		// If components end with /**/*, strip trailing /*
		components = components[:len(components)-2]
	}

	// Add terminal marker or convert existing doublestar to terminal doublestar
	if prevComponentsAre([]componentType{compSeparatorDoublestar, compSeparator}) {
		components = components[:len(components)-2]
		components = append(components, component{compType: compSeparatorDoublestarSeparatorTerminal})
	} else if prevComponentsAre([]componentType{compSeparatorDoublestar}) {
		components[len(components)-1] = component{compType: compSeparatorDoublestarTerminal}
	} else {
		components = append(components, component{compType: compTerminal})
	}

	var variantBuf strings.Builder
	var regexBuf strings.Builder
	regexBuf.WriteRune('^')
	for _, c := range components {
		variantBuf.WriteString(c.String())
		regexBuf.WriteString(c.componentRegex())
	}
	regexBuf.WriteRune('$')
	regex := regexpMustCompileLongest(regexBuf.String())

	v := PatternVariant{
		variant:    variantBuf.String(),
		components: components,
		regex:      regex,
	}

	return v, nil
}

// regexpMustCompileLongest compiles the given string into a Regexp and then
// calls Longest() on it before returning it.
func regexpMustCompileLongest(str string) *regexp.Regexp {
	re := regexp.MustCompile(str)
	re.Longest()
	return re
}

// Need to escape any unescaped literal "⁑" runes before we use that symbol to
// indicate the presence of a "**" doublestar.
var doublestarEscaper = regexpMustCompileLongest(`((\\)*)⁑`)

// Need to replace unescaped "**", but must be careful about an escaped '\\'
// before the first '*', since that doesn't escape the first '*'.
var doublestarReplacer = regexpMustCompileLongest(`((\\)*)\*\*`)

// prepareVariantForParsing escapes any unescaped '⁑' characters and then
// replaces any unescaped "**" with a single '⁑' so that doublestars can be
// identified without needing to look ahead whenever a '*' is seen.
func prepareVariantForParsing(variant string) string {
	escaped := doublestarEscaper.ReplaceAllStringFunc(variant, func(s string) string {
		if (len(s)-len("⁑"))%2 == 1 {
			// Odd number of leading '\\'s, so already escaped
			return s
		}
		// Escape any unescaped literal "⁑"
		return s[:len(s)-len("⁑")] + `\` + "⁑"
	})
	prepared := doublestarReplacer.ReplaceAllStringFunc(escaped, func(s string) string {
		if (len(s)-len("**"))%2 == 1 {
			// Odd number of leading '\\'s, so escaped
			return s
		}
		// Discard trailing "**", add "⁑" instead
		return s[:len(s)-2] + "⁑"
	})
	return prepared
}

type componentReader struct {
	components []component
	submatches []string
	index      int
}

func (r *componentReader) next() (*component, string) {
	if r.index >= len(r.components) {
		return &component{compType: compTerminal}, ""
	}
	comp := &r.components[r.index]
	submatch := r.submatches[r.index]
	r.index++
	return comp, submatch
}

// Compare returns the relative precence of the receiver and the given pattern
// variant when considering the given matching path.
//
// Returns one of the following, if no error occurs:
// -1 if v has lower precedence than other
// 0 if v and other have equal precedence (only possible if v == other)
// 1 if v has higher precedence than other.
func (v PatternVariant) Compare(other PatternVariant, matchingPath string) (int, error) {
	selfSubmatches := v.regex.FindStringSubmatch(matchingPath)
	switch {
	case selfSubmatches == nil:
		return 0, fmt.Errorf("internal error: no matches for pattern variant against given path:\ncomponents: %+v\nregex: %s\npath: %s", v.components, v.regex.String(), matchingPath)
	case len(selfSubmatches)-1 != len(v.components):
		return 0, fmt.Errorf("internal error: submatch count not equal to component count:\ncomponents: %+v\nregex: %s\npath: %s", v.components, v.regex.String(), matchingPath)
	}

	otherSubmatches := other.regex.FindStringSubmatch(matchingPath)
	if otherSubmatches == nil {
		return 0, fmt.Errorf("internal error: no matches for pattern variant against given path\ncomponents: %+v\nregex: %s\npath: %s", other.components, other.regex.String(), matchingPath)
	} else if len(otherSubmatches)-1 != len(other.components) {
		return 0, fmt.Errorf("internal error: submatch count not equal to component count:\ncomponents: %+v\nregex: %s\npath: %s", other.components, other.regex.String(), matchingPath)
	}

	selfReader := componentReader{components: v.components, submatches: selfSubmatches[1:]}
	otherReader := componentReader{components: other.components, submatches: otherSubmatches[1:]}

loop:
	for {
		selfComp, selfSubmatch := selfReader.next()
		otherComp, otherSubmatch := otherReader.next()
		if selfComp.compType < otherComp.compType {
			return -1, nil
		} else if selfComp.compType > otherComp.compType {
			return 1, nil
		}
		switch selfComp.compType {
		case compGlobstar, compSeparatorDoublestar:
			// Prioritize shorter matches for variable-width non-terminal components
			if len(selfSubmatch) > len(otherSubmatch) {
				return -1, nil
			} else if len(selfSubmatch) < len(otherSubmatch) {
				return 1, nil
			}
		case compSeparatorDoublestarTerminal, compSeparatorDoublestarSeparatorTerminal, compTerminal:
			break loop
		case compLiteral:
			// Prioritize longer literals (which must match exactly)
			if selfSubmatch < otherSubmatch {
				return -1, nil
			} else if selfSubmatch > otherSubmatch {
				return 1, nil
			}
		default:
			continue
		}
	}
	return 0, nil
}
