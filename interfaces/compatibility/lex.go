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
	"strings"
	"unicode"
	"unicode/utf8"
)

// ItemType identifies the type of lex items.
type ItemType int

const (
	ItemError ItemType = iota // error occurred; value is text of error
	ItemString
	ItemInteger
	ItemAND
	ItemOR
	ItemLeftParen
	ItemRightParen
	ItemRangeLeftInt
	ItemRangeRightInt
	ItemEOF
)

// Values for the different item types
var itemVal = [...]string{
	"error", "string", "integer", "AND", "OR", "(", ")", "(", ")", "EOF"}

func (typ ItemType) String() string {
	return fmt.Sprintf("%s", itemVal[typ])
}

const eof = -1

// Item represents a token or text string returned from the scanner.
type Item struct {
	Typ ItemType // The type of this item.
	Val string   // The value of this item.
}

func (i Item) String() string {
	switch i.Typ {
	case ItemError:
		fallthrough
	case ItemString:
		fallthrough
	case ItemInteger:
		if len(i.Val) > 20 {
			return fmt.Sprintf(`"%s: %.20s..."`, itemVal[i.Typ], i.Val)
		} else {
			return fmt.Sprintf(`"%s: %s"`, itemVal[i.Typ], i.Val)
		}
	}
	return fmt.Sprintf("%q", itemVal[i.Typ])
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
	l.tokens = append(l.tokens, Item{Typ: ItemError, Val: errMsg})
	return nil
}

// isSpace returns true for the usual spaces. We acoid using isSpace as
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

//
// Definition of the lexer states
//

func lexSpace(l *lexer) stateFn {
	r := l.eatSpaces()
	switch {
	case strings.HasPrefix(l.input[l.pos:], "AND"):
		return lexAND
	case strings.HasPrefix(l.input[l.pos:], "OR"):
		return lexOR
	case unicode.IsLower(r):
		return lexString
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
	return lexOperator(l, ItemAND)
}

func lexOR(l *lexer) stateFn {
	return lexOperator(l, ItemOR)
}

func lexOperator(l *lexer, itemTyp ItemType) stateFn {
	l.tokens = append(l.tokens, Item{Typ: itemTyp})
	l.pos += len(itemVal[itemTyp])

	r := l.peek()
	switch {
	case isSpace(r):
		return lexSpace
	case r == '(':
		return lexLeftParen
	case r == eof:
		return nil
	}
	return l.finishWithError(fmt.Sprintf("unexpected rune after %s: %c", itemVal[itemTyp], r))
}

func lexLeftParen(l *lexer) stateFn {
	return lexParen(l, ItemLeftParen)
}

func lexRightParen(l *lexer) stateFn {
	return lexParen(l, ItemRightParen)
}

func lexParen(l *lexer, itemTyp ItemType) stateFn {
	l.tokens = append(l.tokens, Item{Typ: itemTyp})
	l.pos += len(itemVal[itemTyp])

	return lexSpace
}

func lexString(l *lexer) stateFn {
	start := l.pos
	// After a lowercase character we can have digits in the string
	for unicode.IsLower(l.peek()) || unicode.IsDigit(l.peek()) {
		l.next()
	}
	l.tokens = append(l.tokens, Item{Typ: ItemString, Val: l.input[start:l.pos]})

	r := l.peek()
	switch {
	case isSpace(r):
		return lexNoLabel
	case r == '-':
		l.pos += 1
		return lexStringIntegerOrRange
	case r == ')':
		return lexRightParen
	case r == eof:
		return nil
	}
	return l.finishWithError(fmt.Sprintf("unexpected rune after string: %c", r))
}

func lexNoLabel(l *lexer) stateFn {
	// We expect something that is not a label
	r := l.eatSpaces()
	switch {
	case strings.HasPrefix(l.input[l.pos:], "AND"):
		return lexAND
	case strings.HasPrefix(l.input[l.pos:], "OR"):
		return lexOR
	case r == ')':
		return lexRightParen
	case r == eof:
		return nil
	}
	return l.finishWithError(fmt.Sprintf("unexpected rune: %c", r))
}

func lexStringIntegerOrRange(l *lexer) stateFn {
	r := l.peek()
	switch {
	case unicode.IsLower(r):
		return lexString
	case unicode.IsDigit(r):
		return lexInteger
	case r == '(':
		l.pos += 1
		return lexRangeLeftInteger
	case r == eof:
		return l.finishWithError("no rune after dash")
	}
	return l.finishWithError(fmt.Sprintf("unexpected rune after dash: %c", r))
}

func readInteger(l *lexer, typ ItemType) error {
	start := l.pos
	for unicode.IsDigit(l.peek()) {
		l.next()
	}
	if start == l.pos {
		return fmt.Errorf("not an integer: %c", l.peek())
	}
	intStr := l.input[start:l.pos]
	if len(intStr) > 1 && intStr[0] == '0' {
		return fmt.Errorf("integers not allowed to start with 0: %s", intStr)
	}
	if len(intStr) > 8 {
		return fmt.Errorf("integer with more than 8 digits: %s", intStr)
	}
	l.tokens = append(l.tokens, Item{Typ: typ, Val: intStr})
	return nil
}

func lexInteger(l *lexer) stateFn {
	if err := readInteger(l, ItemInteger); err != nil {
		return l.finishWithError(err.Error())
	}

	r := l.peek()
	switch {
	case isSpace(r):
		return lexSpace
	case r == '-':
		l.pos += 1
		return lexStringIntegerOrRange
	case r == ')':
		return lexRightParen
	case r == eof:
		return nil
	}
	return l.finishWithError(fmt.Sprintf("unexpected rune after integer: %c", r))
}

func lexRangeLeftInteger(l *lexer) stateFn {
	if l.peek() == eof {
		return l.finishWithError("no range left value after (")
	}
	if err := readInteger(l, ItemRangeLeftInt); err != nil {
		return l.finishWithError(err.Error())
	}

	r := l.peek()
	switch {
	case strings.HasPrefix(l.input[l.pos:], ".."):
		l.pos += 2
		return lexRangeRightInteger
	case r == eof:
		return l.finishWithError("no .. after range left value")
	}
	return l.finishWithError(fmt.Sprintf("unexpected rune after range left integer: %c", r))
}

func lexRangeRightInteger(l *lexer) stateFn {
	if l.peek() == eof {
		return l.finishWithError("no range right value after ..")
	}
	if err := readInteger(l, ItemRangeRightInt); err != nil {
		return l.finishWithError(err.Error())
	}

	r := l.next()
	if r == eof {
		return l.finishWithError("no ) after range right value")
	}
	if r != ')' {
		return l.finishWithError(
			fmt.Sprintf("unexpected rune after range right integer: %c", r))
	}

	r = l.peek()
	switch {
	case isSpace(r):
		return lexSpace
	case r == '-':
		l.pos += 1
		return lexStringIntegerOrRange
	case r == ')':
		return lexRightParen
	case r == eof:
		return nil
	}
	return l.finishWithError(fmt.Sprintf("unexpected rune after integer range: %c", r))
}
