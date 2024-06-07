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
	tokError
	tokText
	tokBraceOpen
	tokBraceClose
	tokComma
)

func (t TokenType) String() string {
	switch t {
	case tokEOF:
		return "end-of-file"
	case tokError:
		return "error"
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

func Scan(text string) (tokens []Token) {
	var runes []rune

	rr := strings.NewReader(text)
loop:
	for {
		r, _, err := rr.ReadRune()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break loop
			}

			if len(runes) > 0 {
				tokens = append(tokens, Token{Text: string(runes), Type: tokText})
				runes = nil
			}

			tokens = append(tokens, Token{Type: tokError, Text: string(r)})

			return tokens
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
				if len(runes) > 0 {
					tokens = append(tokens, Token{Text: string(runes), Type: tokText})
					runes = nil
				}

				tokens = append(tokens, Token{Type: tokError, Text: string(r2)})

				return tokens
			}

			runes = append(runes, r2)
		default:
			runes = append(runes, r)
		}
	}

	if len(runes) > 0 {
		tokens = append(tokens, Token{Text: string(runes), Type: tokText})
		runes = nil
	}

	return tokens
}
