// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package shlex

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
)

type parserState int

const (
	wordCharacters string = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-,/@$*()+=><:;&^%~|!?[]{}"

	stateNone parserState = iota
	stateUnquotedWord
	stateOpeningSingleQuote
	stateClosingSingleQuote
	stateOpeningDoubleQuote
	stateClosingDoubleQuote
	stateSingleQuotedWord
	stateDoubleQuotedWord
	stateEscape
	stateEscapedCharacter
	stateSingleQuotedWordEscape
	stateDoubleQuotedWordEscape
	stateDoubleQuotedWordEscapedCharacter
	stateSingleQuotedWordEscapedCharacter
	stateEOF
)

var (
	ErrNoClosingQuotation = errors.New("no closing quotation")
	ErrNoEscapedCharacter = errors.New("no escaped character")
)

// Finish returns an error when the parser expects more input
func (p parserState) Finish() error {
	switch p {
	case stateOpeningSingleQuote, stateOpeningDoubleQuote:
		// on the opening quote, still need to see the closing quote
		fallthrough
	case stateDoubleQuotedWord, stateSingleQuotedWord:
		// inside a quoted word
		return ErrNoClosingQuotation
	case stateEscapedCharacter, stateSingleQuotedWordEscapedCharacter, stateDoubleQuotedWordEscapedCharacter:
		// expecting an escaped character
		fallthrough
	case stateEscape, stateSingleQuotedWordEscape, stateDoubleQuotedWordEscape:
		// on the escape character, still need to see the actual
		// escaped character
		return ErrNoEscapedCharacter
	case stateEOF:
		return io.EOF
	}

	return nil
}

// Accumulate returns whether currently read input should be accumulated
func (p parserState) Accumulate() bool {
	collectionStates := []parserState{
		// collect when inside a word
		stateUnquotedWord, stateSingleQuotedWord, stateDoubleQuotedWord,
		// collect escaped characters inside a word
		stateSingleQuotedWordEscapedCharacter, stateDoubleQuotedWordEscapedCharacter,
		// ...and outside too
		stateEscapedCharacter,
	}
	for _, v := range collectionStates {
		if p == v {
			return true
		}
	}
	return false
}

// Consume returns whether currently read input should be consumed
func (p parserState) Consume(previous parserState) bool {
	// consume if we had useful input only
	return (p == stateNone || p == stateEOF) && previous != stateNone
}

// Next returns the next state of the parser given its current state and a rune
// read from input and any errors that occurred while reading
func (p parserState) Next(r rune, err error) parserState {
	next := p
	if err == io.EOF {
		return stateEOF
	}

	switch p {
	case stateNone:
		switch r {
		case '"':
			next = stateOpeningDoubleQuote
		case '\'':
			next = stateOpeningSingleQuote
		case '\\':
			next = stateEscape
		default:
			if pos := strings.IndexRune(wordCharacters, r); pos != -1 {
				next = stateUnquotedWord
			}
		}

	case stateUnquotedWord:
		switch r {
		case '"':
			next = stateOpeningDoubleQuote
		case '\'':
			next = stateOpeningSingleQuote
		case '\\':
			next = stateEscape
		case ' ':
			next = stateNone
		default:
			if pos := strings.IndexRune(wordCharacters, r); pos != -1 {
				next = stateUnquotedWord
			} else {
				next = stateNone
			}
		}

	case stateOpeningSingleQuote:
		if r == '\'' {
			next = stateClosingSingleQuote
		} else {
			next = stateSingleQuotedWord
		}

	case stateClosingSingleQuote, stateClosingDoubleQuote:
		if pos := strings.IndexRune(wordCharacters, r); pos != -1 {
			next = stateUnquotedWord
		} else {
			next = stateNone
		}

	case stateSingleQuotedWord:
		switch r {
		case '\'':
			next = stateClosingSingleQuote
		case '\\':
			next = stateSingleQuotedWordEscape
		}

	case stateOpeningDoubleQuote:
		if r == '"' {
			next = stateClosingDoubleQuote
		} else {
			next = stateDoubleQuotedWord
		}

	case stateDoubleQuotedWord:
		switch r {
		case '\\':
			next = stateDoubleQuotedWordEscape
		case '"':
			next = stateClosingDoubleQuote
		}

	case stateEscape:
		next = stateEscapedCharacter

	case stateDoubleQuotedWordEscape:
		next = stateDoubleQuotedWordEscapedCharacter

	case stateSingleQuotedWordEscape:
		next = stateSingleQuotedWordEscapedCharacter

	case stateSingleQuotedWordEscapedCharacter:
		next = stateSingleQuotedWord

	case stateDoubleQuotedWordEscapedCharacter:
		next = stateDoubleQuotedWord

	case stateEscapedCharacter:
		if pos := strings.IndexRune(wordCharacters, r); pos != -1 {
			next = stateUnquotedWord
		} else {
			next = stateNone
		}
	}
	return next
}

type tokenizer struct {
	in    io.RuneReader
	state parserState
}

func newTokenizer(in io.RuneReader) tokenizer {
	return tokenizer{
		in:    in,
		state: stateNone,
	}

}
func (t *tokenizer) token() (string, error) {
	var b strings.Builder
	var retErr error

	for {
		r, _, err := t.in.ReadRune()
		next, prev := t.state.Next(r, err), t.state
		if err != nil {
			if err != io.EOF {
				return "", err
			}
			if err := t.state.Finish(); err != nil {
				return "", err
			}
		}

		t.state = next

		if next.Accumulate() {
			b.WriteRune(r)
		} else if next.Consume(prev) {
			break
		}
	}
	return b.String(), retErr
}

// SplitLine splits a string using shell splitting rules
func SplitLine(s string) ([]string, error) {
	out := []string{}

	tkn := newTokenizer(bufio.NewReader(bytes.NewBufferString(s)))

	for {
		token, err := tkn.token()
		if err != nil {
			if err != io.EOF {
				return nil, err
			} else {
				break
			}
		}
		out = append(out, token)
	}
	return out, nil
}
