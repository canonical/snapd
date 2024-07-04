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

func (c *component) String() string {
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

func (c *component) componentRegex() string {
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

func unescapeLiteral(literal string) string {
	return escapeFinder.ReplaceAllString(literal, "${1}")
}

type PatternVariant struct {
	variant    string
	components []component
	regex      *regexp.Regexp
}

func (v *PatternVariant) String() string {
	return v.variant
}

// ParsePatternVariant parses a rendered variant string into a PatternVariant
// whose precedence can be compared against others.
func ParsePatternVariant(variant string) (*PatternVariant, error) {
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
	// was not a globstar.
	addGlobstar := func() {
		if !prevComponentsAre([]componentType{compGlobstar}) {
			components = append(components, component{compType: compGlobstar})
		}
	}

	// reducePrevDoublestar checks if the most recent component was a
	// doublestar, and if it was, replaces it with a globstar '*'.
	reducePrevDoublestar := func() {
		if components[len(components)-1].compType == compSeparatorDoublestar {
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
loop:
	for {
		r, _, err := rr.ReadRune()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break loop
			}
			// Should not occur, err is only set if no rune available to read
			return nil, fmt.Errorf("internal error: failed to read rune while scanning variant: %w", err)
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
				return nil, errors.New(`internal error: trailing unescaped '\' character`)
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
			return nil, fmt.Errorf(`internal error: unexpected unescaped '%v' character`, r)
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
	regex := regexp.MustCompile(regexBuf.String())
	regex.Longest()

	v := PatternVariant{
		variant:    variantBuf.String(),
		components: components,
		regex:      regex,
	}

	return &v, nil
}

// Need to replace unescaped "**", but must be careful about an escaped '\\'
// before the first '*', since that doesn't escape the first '*'.
var doublestarReplacer = regexp.MustCompile(`([^\\](\\\\)*)\*\*`)

func prepareVariantForParsing(variant string) string {
	prepared := doublestarReplacer.ReplaceAllStringFunc(variant, func(s string) string {
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
func (v *PatternVariant) Compare(other *PatternVariant, matchingPath string) (int, error) {
	selfSubmatches := v.regex.FindStringSubmatch(matchingPath)
	if selfSubmatches == nil {
		return 0, fmt.Errorf("internal error: no matches for pattern variant against given path:\ncomponents: %+v\nregex: %s\npath: %s", v.components, v.regex.String(), matchingPath)
	} else if len(selfSubmatches)-1 != len(v.components) {
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
