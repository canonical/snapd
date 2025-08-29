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

const eof = -1

// Item represents a token or text string returned from the scanner.
type Item struct {
	Typ ItemType // The type of this item.
	Val string   // The value of this item.
}

func (i Item) String() string {
	switch i.Typ {
	case ItemError:
	case ItemString:
	case ItemInteger:
		if len(i.Val) > 10 {
			return fmt.Sprintf("%s: %.10q...", itemVal[i.Typ], i.Val)
		} else {
			return fmt.Sprintf("%s: %q", itemVal[i.Typ], i.Val)
		}
	}
	return fmt.Sprintf("%q", itemVal[i.Typ])
}

// lexer holds the state of the scanner.
type lexer struct {
	input  string // the string being scanned
	pos    int
	tokens []Item
	atEOF  bool
}

// stateFn represents the state of the scanner as a function that performs an
// action returns the next state.
type stateFn func(*lexer) stateFn

// Ancillary methods

// next returns the next rune in the input.
func (l *lexer) next() rune {
	if l.pos >= len(l.input) {
		l.atEOF = true
		return eof
	}
	r, w := utf8.DecodeRuneInString(l.input[l.pos:])
	l.pos += w
	return r
}

// peek returns but does not consume the next rune in the input.
func (l *lexer) peek() rune {
	r := l.next()
	l.backup()
	return r
}

// backup steps back one rune.
func (l *lexer) backup() {
	if !l.atEOF && l.pos > 0 {
		_, w := utf8.DecodeLastRuneInString(l.input[:l.pos])
		l.pos -= w
	}
}

// items returns tokens from the input. Called by the parser.
// TODO implement instead nextItem to make this concurrent. Note that we are
// using itemError in preparation for this: we return such an error as the last
// item to adapt to a future stream processing.
func items(input string) []Item {
	l := &lexer{input: input}
	state := lexSpace
	for state != nil {
		state = state(l)
	}
	return l.tokens
}

//
// Definition of the lexer states
//

func lexSpace(l *lexer) stateFn {
	var r rune
	for {
		r = l.peek()
		if !unicode.IsSpace(r) {
			break
		}
		l.next()
	}
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
	l.tokens = append(l.tokens,
		Item{Typ: ItemError, Val: fmt.Sprintf("unexpected rune: %c", r)})
	return nil
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
	case unicode.IsSpace(r):
		return lexSpace
	case r == '(':
		return lexLeftParen
	case r == ')':
		return lexRightParen
	case r == eof:
		return nil
	}
	l.tokens = append(l.tokens, Item{Typ: ItemError,
		Val: fmt.Sprintf("unexpected rune after %s: %c", itemVal[itemTyp], r)})
	return nil
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
	case unicode.IsSpace(r):
		return lexSpace
	case r == '-':
		l.pos += 1
		return lexStringIntegerOrRange
	case r == '(':
		return lexLeftParen
	case r == ')':
		return lexRightParen
	case r == eof:
		return nil
	}
	l.tokens = append(l.tokens, Item{Typ: ItemError,
		Val: fmt.Sprintf("unexpected rune after string: %c", r)})
	return nil
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
		l.tokens = append(l.tokens, Item{Typ: ItemError, Val: "no rune after dash"})
		return nil
	}
	l.tokens = append(l.tokens, Item{Typ: ItemError,
		Val: fmt.Sprintf("unexpected rune after dash: %c", r)})
	return nil
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
		l.tokens = append(l.tokens, Item{Typ: ItemError, Val: err.Error()})
		return nil
	}

	r := l.peek()
	switch {
	case unicode.IsSpace(r):
		return lexSpace
	case r == '-':
		l.pos += 1
		return lexStringIntegerOrRange
	case r == ')':
		return lexRightParen
	case r == '(':
		return lexLeftParen
	case r == eof:
		return nil
	}
	l.tokens = append(l.tokens, Item{Typ: ItemError,
		Val: fmt.Sprintf("unexpected rune after integer: %c", r)})
	return nil
}

func lexRangeLeftInteger(l *lexer) stateFn {
	if l.peek() == eof {
		l.tokens = append(l.tokens, Item{Typ: ItemError,
			Val: "no range left value after ("})
		return nil
	}
	if err := readInteger(l, ItemRangeLeftInt); err != nil {
		l.tokens = append(l.tokens, Item{Typ: ItemError, Val: err.Error()})
		return nil
	}

	r := l.peek()
	switch {
	case strings.HasPrefix(l.input[l.pos:], ".."):
		l.pos += 2
		return lexRangeRightInteger
	case r == eof:
		l.tokens = append(l.tokens, Item{Typ: ItemError,
			Val: "no .. after range left value"})
		return nil
	}
	l.tokens = append(l.tokens, Item{Typ: ItemError,
		Val: fmt.Sprintf("unexpected rune after range left integer: %c", r)})
	return nil
}

func lexRangeRightInteger(l *lexer) stateFn {
	if l.peek() == eof {
		l.tokens = append(l.tokens, Item{Typ: ItemError,
			Val: "no range right value after .."})
		return nil
	}
	if err := readInteger(l, ItemRangeRightInt); err != nil {
		l.tokens = append(l.tokens, Item{Typ: ItemError, Val: err.Error()})
		return nil
	}

	r := l.next()
	if r == eof {
		l.tokens = append(l.tokens, Item{Typ: ItemError,
			Val: "no ) after range right value"})
		return nil
	}
	if r != ')' {
		l.tokens = append(l.tokens, Item{Typ: ItemError,
			Val: fmt.Sprintf("unexpected rune after range right integer: %c", r)})
		return nil
	}

	r = l.peek()
	switch {
	case unicode.IsSpace(r):
		return lexSpace
	case r == '-':
		l.pos += 1
		return lexStringIntegerOrRange
	case r == ')':
		return lexRightParen
	case r == '(':
		return lexLeftParen
	case r == eof:
		return nil
	}
	l.tokens = append(l.tokens, Item{Typ: ItemError,
		Val: fmt.Sprintf("unexpected rune after integer range: %c", r)})
	return nil
}
