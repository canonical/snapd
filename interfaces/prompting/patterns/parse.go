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

func Parse(tokens []Token) (RenderNode, error) {
	tr := tokenReader{
		tokens: tokens,
	}
	return parseSeq(&tr)
}

func parseSeq(tr *tokenReader) (RenderNode, error) {
	var seq Seq
seqLoop:
	for {
		t := tr.Peek()

		switch t.Type {
		case tokEOF:
			break seqLoop
		case tokBraceOpen:
			inner, err := parseAlt(tr)
			if err != nil {
				return nil, err
			}

			seq = append(seq, inner)
		case tokBraceClose:
			if tr.depth > 0 {
				break seqLoop
			}

			tr.Token()

			return nil, errors.New("unmatched '}' character")
		case tokText:
			tr.Token()
			seq = append(seq, Literal(t.Text))
		case tokComma:
			if tr.depth > 0 {
				break seqLoop
			}

			tr.Token() // discard, we get called in a loop
			seq = append(seq, Literal(","))
		}
	}

	return seq.optimize().reduceStrength(), nil
}

func parseAlt(tr *tokenReader) (RenderNode, error) {
	var alt Alt

	if t := tr.Token(); t.Type != tokBraceOpen {
		return nil, errors.New("expected { in parseAlt")
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

		alt = append(alt, item)

		switch t := tr.Token(); t.Type {
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

	return alt.optimize().reduceStrength(), nil
}

type tokenReader struct {
	tokens []Token
	depth  int
}

func (tr tokenReader) Peek() Token {
	if len(tr.tokens) == 0 {
		return Token{Type: tokEOF}
	}

	return tr.tokens[0]
}

func (tr *tokenReader) Token() Token {
	t := tr.Peek()

	if t.Type != tokEOF {
		tr.tokens = tr.tokens[1:]
	}

	return t
}
