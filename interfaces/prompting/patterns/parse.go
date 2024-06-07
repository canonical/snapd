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
	"strings"
)

func Parse(tokens []Token) (RenderNode, error) {
	tr := tokenReader(tokens)
	return parseSeq(&tr, false)
}

func parseSeq(tr *tokenReader, insideAlt bool) (RenderNode, error) {
	var seq Seq
loop:
	for {
		t := tr.Peek()

		switch t.Type {
		case tokEOF:
			break loop
		case tokError:
			return nil, errors.New("cannot scan next token")
		case tokBraceOpen:
			inner, err := parseAlt(tr)
			if err != nil {
				return nil, err
			}

			seq = append(seq, inner)
		case tokBraceClose:
			if insideAlt {
				break loop
			}

			tr.Token()

			return nil, errors.New("unexpected } (in parseSeq)")
		case tokText:
			tr.Token()
			seq = append(seq, Literal(t.Text))
		case tokComma:
			if insideAlt {
				break loop
			}

			tr.Token() // discard, we get called in a loop
			seq = append(seq, Literal(","))
		}
	}

	return seq.optimize().reduceStrength(), nil
}

func (seq Seq) reduceStrength() RenderNode {
	switch len(seq) {
	case 0:
		return Literal("")
	case 1:
		return seq[0]
	default:
		return seq
	}
}

func (seq Seq) optimize() Seq {
	var b strings.Builder

	var newSeq Seq

	for _, item := range seq {
		if v, ok := item.(Literal); ok {
			if v == "" {
				continue
			}

			b.WriteString(string(v))
		} else {
			if b.Len() > 0 {
				newSeq = append(newSeq, Literal(b.String()))
				b.Reset()
			}

			newSeq = append(newSeq, item)
		}
	}

	if b.Len() > 0 {
		newSeq = append(newSeq, Literal(b.String()))
		b.Reset()
	}

	return newSeq
}

func parseAlt(tr *tokenReader) (RenderNode, error) {
	var alt Alt

	if t := tr.Token(); t.Type != tokBraceOpen {
		return nil, errors.New("expected { in parseAlt")
	}

loop:
	for {
		item, err := parseSeq(tr, true)
		if err != nil {
			return nil, err
		}

		alt = append(alt, item)

		switch t := tr.Token(); t.Type {
		case tokBraceClose:
			break loop
		case tokComma:
			continue
		default:
			return nil, fmt.Errorf("Unexpected token %v in parseAlt", t)
		}
	}

	return alt.optimze().reduceStrength(), nil
}

type tokenReader []Token

func (tr tokenReader) Peek() Token {
	if len(tr) == 0 {
		return Token{Type: tokEOF}
	}

	return tr[0]
}

func (tr *tokenReader) Token() Token {
	t := tr.Peek()

	if t.Type != tokEOF {
		*tr = (*tr)[1:]
	}

	return t
}
