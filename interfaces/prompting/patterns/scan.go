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
	"io"
	"strings"
)

type TokenType int

const (
	tokEOF TokenType = iota
	tokText
	tokBraceOpen
	tokBraceClose
	tokComma
)

func (t TokenType) String() string {
	switch t {
	case tokEOF:
		return "end-of-file"
	case tokText:
		return "text"
	case tokBraceOpen:
		return "brace-open"
	case tokBraceClose:
		return "brace-close"
	case tokComma:
		return "comma"
	default:
		return "?"
	}
}

type Token struct {
	Type TokenType
	Text string
}

func Scan(text string) (tokens []Token, err error) {
	var runes []rune

	rr := strings.NewReader(text)
loop:
	for {
		r, _, err := rr.ReadRune()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break loop
			}
			// Should not occur, err is only set if no rune available to read
			return nil, err
		}

		switch r {
		case '{':
			if len(runes) > 0 {
				tokens = append(tokens, Token{Text: string(runes), Type: tokText})
				runes = nil
			}
			tokens = append(tokens, Token{Text: string(r), Type: tokBraceOpen})
		case '}':
			if len(runes) > 0 {
				tokens = append(tokens, Token{Text: string(runes), Type: tokText})
				runes = nil
			}
			tokens = append(tokens, Token{Text: string(r), Type: tokBraceClose})
		case ',':
			if len(runes) > 0 {
				tokens = append(tokens, Token{Text: string(runes), Type: tokText})
				runes = nil
			}
			tokens = append(tokens, Token{Text: string(r), Type: tokComma})
		case '\\':
			r2, _, err := rr.ReadRune()
			if err != nil {
				// Should not occur, caller should verify that patterns do not
				// have trailing '\\'
				return nil, errors.New(`trailing unescaped '\' character`)
			}

			runes = append(runes, r, r2)
		case '[', ']':
			return nil, errors.New("cannot contain unescaped '[' or ']' character")
		default:
			runes = append(runes, r)
		}
	}

	if len(runes) > 0 {
		tokens = append(tokens, Token{Text: string(runes), Type: tokText})
		// runes = nil
	}

	return tokens, nil
}
