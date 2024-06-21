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
)

func parse(tokens []token) (renderNode, error) {
	tr := tokenReader{
		tokens: tokens,
	}
	return parseSeq(&tr)
}

func parseSeq(tr *tokenReader) (renderNode, error) {
	var s seq
seqLoop:
	for {
		t := tr.Peek()

		switch t.tType {
		case tokEOF:
			break seqLoop
		case tokBraceOpen:
			inner, err := parseAlt(tr)
			if err != nil {
				return nil, err
			}

			s = append(s, inner)
		case tokBraceClose:
			if tr.depth > 0 {
				break seqLoop
			}
			tr.token()
			return nil, errors.New("unmatched '}' character")
		case tokText:
			tr.token()
			s = append(s, literal(t.text))
		case tokComma:
			if tr.depth > 0 {
				break seqLoop
			}
			tr.token() // discard, we get called in a loop
			s = append(s, literal(","))
		}
	}

	return s.optimize(), nil
}

func parseAlt(tr *tokenReader) (renderNode, error) {
	var a alt

	if t := tr.token(); t.tType != tokBraceOpen {
		// Should not occur, caller should call parseAlt on peeking '{'
		return nil, fmt.Errorf("expected '{' at start of alt, but got %v", t)
	}

	tr.depth++
	defer func() {
		tr.depth--
	}()
	if tr.depth >= maxExpandedPatterns {
		return nil, fmt.Errorf("nested group depth exceeded maximum number of expanded path patterns (%d)", maxExpandedPatterns)
	}

altLoop:
	for {
		item, err := parseSeq(tr)
		if err != nil {
			return nil, err
		}

		a = append(a, item)

		switch t := tr.token(); t.tType {
		case tokBraceClose:
			break altLoop
		case tokComma:
			continue
		case tokEOF:
			return nil, errors.New("unmatched '{' character")
		default:
			return nil, fmt.Errorf("unexpected token %v when parsing alt", t)
		}
	}

	return a.optimize(), nil
}

type tokenReader struct {
	tokens []token
	depth  int
}

func (tr tokenReader) Peek() token {
	if len(tr.tokens) == 0 {
		return token{tType: tokEOF}
	}

	return tr.tokens[0]
}

func (tr *tokenReader) token() token {
	t := tr.Peek()

	if t.tType != tokEOF {
		tr.tokens = tr.tokens[1:]
	}

	return t
}
