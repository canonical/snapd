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
	"strings"
)

type tokenType int

const (
	tokEOF tokenType = iota
	tokText
	tokBraceOpen
	tokBraceClose
	tokComma
)

// String is used for debugging purposes only.
func (t tokenType) String() string {
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

type token struct {
	tType tokenType
	text  string
}

func scan(text string) (tokens []token, err error) {
	if len(text) == 0 {
		return nil, errors.New("pattern has length 0")
	}
	if text[0] != '/' {
		return nil, errors.New("pattern must start with '/'")
	}

	var runes []rune

	consumeText := func() {
		if len(runes) > 0 {
			tokens = append(tokens, token{text: string(runes), tType: tokText})
			runes = nil
		}
	}

	rr := strings.NewReader(text)
loop:
	for {
		r, _, err := rr.ReadRune()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break loop
			}
			// Should not occur, err is only set if no rune available to read
			return nil, fmt.Errorf("internal error: failed to read rune while scanning path pattern: %w", err)
		}

		switch r {
		case '{':
			consumeText()
			tokens = append(tokens, token{text: string(r), tType: tokBraceOpen})
		case '}':
			consumeText()
			tokens = append(tokens, token{text: string(r), tType: tokBraceClose})
		case ',':
			consumeText()
			tokens = append(tokens, token{text: string(r), tType: tokComma})
		case '\\':
			r2, _, err := rr.ReadRune()
			if err != nil {
				return nil, errors.New(`trailing unescaped '\' character`)
			}

			runes = append(runes, r, r2)
		case '[', ']':
			return nil, errors.New("cannot contain unescaped '[' or ']' character")
		default:
			runes = append(runes, r)
		}
	}

	consumeText()

	return tokens, nil
}
