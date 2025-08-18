// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

// Based on go's text/template lexer. See https://www.youtube.com/watch?v=HxaD_trXwRE&t=2s
// for ideas behind this implementation.
package compatibility

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ItemType identifies the type of lex items.
type ItemType int

const (
	ItemError ItemType = iota // error occurred; ErrMsg is text of error
	ItemLabel
	ItemAND
	ItemOR
	ItemLeftParen
	ItemRightParen
	ItemEOF
)

// Values for the different item types
var itemVal = [...]string{
	"error", "label", "AND", "OR", "(", ")", "EOF"}

func (typ ItemType) String() string {
	return fmt.Sprintf("%s", itemVal[typ])
}

const eof = -1

// Item represents a token or text string returned from the scanner.
type Item struct {
	Typ    ItemType    // The type of this item.
	ErrMsg string      // Set if an error
	Label  CompatField // Set for ItemLabel type
}

func (i Item) String() string {
	msg := ""
	switch i.Typ {
	case ItemLabel:
		msg = i.Label.String()
	case ItemError:
		msg = i.ErrMsg
	default:
		return fmt.Sprintf("%q", itemVal[i.Typ])
	}
	if len(msg) > 20 {
		return fmt.Sprintf(`"%s: %.20s..."`, itemVal[i.Typ], msg)
	} else {
		return fmt.Sprintf(`"%s: %s"`, itemVal[i.Typ], msg)
	}
}

// lexer holds the state of the scanner.
type lexer struct {
	input  string // the string being scanned
	pos    int
	tokens []Item
}

// stateFn represents the state of the scanner as a function that performs an
// action returns the next state.
type stateFn func(*lexer) stateFn

// Ancillary methods

// next returns the next rune in the input.
func (l *lexer) next() rune {
	if l.pos >= len(l.input) {
		return eof
	}
	r, w := utf8.DecodeRuneInString(l.input[l.pos:])
	l.pos += w
	return r
}

// peek returns but does not consume the next rune in the input.
func (l *lexer) peek() rune {
	if l.pos >= len(l.input) {
		return eof
	}
	r, _ := utf8.DecodeRuneInString(l.input[l.pos:])
	return r
}

// items returns tokens from the input. Called by the parser.
// TODO implement instead nextItem to make this concurrent. Note that we are
// using ItemError in preparation for this: we return such an error as the last
// item to adapt to a future stream processing.
func items(input string) []Item {
	l := &lexer{input: input}
	state := lexSpace
	for state != nil {
		state = state(l)
	}
	return l.tokens
}

func (l *lexer) finishWithError(errMsg string) stateFn {
	l.tokens = append(l.tokens, Item{Typ: ItemError, ErrMsg: errMsg})
	return nil
}

// isSpace returns true for the usual spaces. We avoid using isSpace as
// we prefer to error out if more esoteric space runes are found.
func isSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\r', ' ':
		return true
	}
	return false
}

func (l *lexer) eatSpaces() rune {
	var r rune
	for {
		r = l.peek()
		if !isSpace(r) {
			break
		}
		l.next()
	}
	return r
}

func (l *lexer) testAhead(str string) bool {
	return strings.HasPrefix(l.input[l.pos:], str)
}

//
// Definition of the lexer states
//

func lexSpace(l *lexer) stateFn {
	r := l.eatSpaces()
	switch {
	case l.testAhead("AND"):
		return lexAND
	case l.testAhead("OR"):
		return lexOR
	case unicode.IsLower(r):
		return lexLabel
	case r == '(':
		return lexLeftParen
	case r == ')':
		return lexRightParen
	case r == eof:
		return nil
	}
	return l.finishWithError(fmt.Sprintf("unexpected rune: %c", r))
}

func lexAND(l *lexer) stateFn {
	return lexSimpleType(l, ItemAND)
}

func lexOR(l *lexer) stateFn {
	return lexSimpleType(l, ItemOR)
}

func lexLeftParen(l *lexer) stateFn {
	return lexSimpleType(l, ItemLeftParen)
}

func lexRightParen(l *lexer) stateFn {
	return lexSimpleType(l, ItemRightParen)
}

func lexSimpleType(l *lexer, itemTyp ItemType) stateFn {
	l.tokens = append(l.tokens, Item{Typ: itemTyp})
	l.pos += len(itemVal[itemTyp])
	return lexSpace
}

func lexLabel(l *lexer) stateFn {
	compatLabel := &CompatField{}
	var currDim *CompatDimension
	tags := map[string]bool{}

intLoop:
	for {
		r := l.peek()
		switch {
		case unicode.IsLower(r):
			start := l.pos
			// After a lowercase character we can have digits in the string
			for unicode.IsLower(l.peek()) || unicode.IsDigit(l.peek()) {
				l.next()
			}
			// Append the previous dimension
			appendDimensionToLabel(compatLabel, currDim)

			tag := l.input[start:l.pos]
			if len(tag) > 32 {
				return l.finishWithError(
					fmt.Sprintf("string is longer than 32 characters: %s", tag))
			}
			if _, ok := tags[tag]; ok {
				return l.finishWithError(
					fmt.Sprintf("repeated string in label: %s", tag))
			}
			currDim = &CompatDimension{Tag: l.input[start:l.pos]}
			tags[tag] = true
		case r == '-':
			l.next()
			if !unicode.IsLower(l.peek()) && !unicode.IsDigit(l.peek()) && l.peek() != '(' {
				return l.finishWithError(
					fmt.Sprintf("unexpected character after hyphen: %s", errorRune(l)))
			}
			if l.peek() == '(' {
				intRange, err := readLabelRange(l)
				if err != nil {
					return l.finishWithError(err.Error())
				}
				currDim.Values = append(currDim.Values, intRange)
			}
		case unicode.IsDigit(r):
			singleInt, err := readInteger(l)
			if err != nil {
				return l.finishWithError(err.Error())
			}
			currDim.Values = append(currDim.Values,
				CompatRange{Min: singleInt, Max: singleInt})
		default:
			break intLoop
		}
	}
	appendDimensionToLabel(compatLabel, currDim)
	l.tokens = append(l.tokens, Item{Typ: ItemLabel, Label: *compatLabel})
	return lexSpace
}

func appendDimensionToLabel(label *CompatField, dim *CompatDimension) {
	if dim != nil {
		if len(dim.Values) == 0 {
			dim.Values = append(dim.Values, CompatRange{Min: 0, Max: 0})
		}
		label.Dimensions = append(label.Dimensions, *dim)
	}
}

func errorRune(l *lexer) string {
	if l.peek() == eof {
		return "EOF"
	}
	return string(l.peek())
}

func readLabelRange(l *lexer) (CompatRange, error) {
	// Skip '('
	l.next()
	leftInt, err := readInteger(l)
	if err != nil {
		return CompatRange{}, err
	}
	if !l.testAhead("..") {
		return CompatRange{}, fmt.Errorf("no dots in integer range")
	}
	l.pos += 2
	rightInt, err := readInteger(l)
	if err != nil {
		return CompatRange{}, err
	}
	if rightInt < leftInt {
		return CompatRange{}, fmt.Errorf("negative range specified: (%d..%d)", leftInt, rightInt)
	}
	if l.peek() != ')' {
		return CompatRange{}, fmt.Errorf("range missing closing parenthesis")
	}
	l.next()
	return CompatRange{Min: leftInt, Max: rightInt}, nil
}

func readInteger(l *lexer) (uint, error) {
	start := l.pos
	for unicode.IsDigit(l.peek()) {
		l.next()
	}
	if start == l.pos {
		return 0, fmt.Errorf("not an integer: %s", errorRune(l))
	}
	intStr := l.input[start:l.pos]
	if len(intStr) > 1 && intStr[0] == '0' {
		return 0, fmt.Errorf("integers not allowed to start with 0: %s", intStr)
	}
	if len(intStr) > 8 {
		return 0, fmt.Errorf("integer with more than 8 digits: %s", intStr)
	}
	integer, err := strconv.ParseUint(intStr, 10, 0)
	if err != nil {
		return 0, fmt.Errorf("cannot convert to an integer: %v", err)
	}
	return uint(integer), nil
}
