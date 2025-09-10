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

// This implements a parser for compatibility expressions (SD211), which is a
// combination of compatibility labels with AND and OR operators, with the
// possibility of grouping expressions using parenthesis. The grammar we have
// for this parser is:
//
// Expr ::= AndExpr | OrExpr
// AndExpr ::= AndExpr 'AND' Atom | Atom
// OrExpr ::= OrExpr 'OR' Atom | Atom
// Atom ::= '(' Expr ')' | LABEL
//
// With lexical tokens:
//
// LABEL = [a-z][a-z0-9]*(-([0-9]+|[(][0-9]+[.][.][0-9]+[)]))*([a-z][a-z0-9]*(-([0-9]+|[(][0-9]+[.][.][0-9]+[)]))*)*
//
//
// To make it recursive LL(1), we can break left recursion, and break conflicts:
//
// Expr ::= Atom ExprR
// ExprR ::= OrExprR | AndExprR | ε
// OrExprR ::= 'OR' Atom OrExprROpt
// OrExprROpt ::= OrExprR | ε
// AndExprR ::= 'AND' Atom AndExprROpt
// AndExprROpt ::= AndExprR | ε
// Atom ::= '(' Expr ')' | Label
//
// We end up with the first and follow sets (with FIRST in respective order of the rules):
//
//              First         Follow
// Expr         '(',Label     EOF,')'
// ExprR        'OR','AND',ε  EOF,')'
// OrExprR      'OR'          EOF,')'
// OrExprROpt   'OR',ε        EOF,')'
// AndExprR     'AND'         EOF,')'
// AndExprROpt  'AND',ε       EOF,')'
// Atom         '(',Label     EOF,')','OR','AND'
//
// And no conflicts.
//
// Note that it will be parsed as right associative, but as we do not mix OR and
// AND, we can just ignore it and keep the AST with right priority.
//
// For each non-terminal, we have a function, and check the token from the
// lexer, FIRST and decide which branch to take. If ε is in FIRST, then compare
// to what is in FOLLOW.
//
// For more details look at https://en.wikipedia.org/wiki/LL_parser
//
// Thanks to Valentin David for suggesting this grammar.

package compatibility

import (
	"errors"
	"fmt"
	"strconv"
)

// Expression needs to be fulfilled by values of nodes in the tree. We can have
// here either a CompatField or an Operator struct.
type Expression interface {
	String() string
}

// Node for abstract syntax tree of compatibility expressions.
type Node struct {
	Left  *Node
	Right *Node
	Exp   Expression
}

// parser for compatibility expressions
type parser struct {
	tokens []Item
	pos    int
	depth  int
	labels []CompatField
}

// Operator can be AND or OR.
type Operator struct {
	// Type ItemAND or ItemOR
	Oper Item
}

func (op *Operator) String() string {
	return op.Oper.String()
}

func (p *parser) nextToken() Item {
	if p.pos >= len(p.tokens) {
		return Item{Typ: ItemEOF}
	}
	t := p.tokens[p.pos]
	p.pos++
	return t
}

func (p *parser) peekToken() Item {
	if p.pos >= len(p.tokens) {
		return Item{Typ: ItemEOF}
	}
	return p.tokens[p.pos]
}

// parse parses a compatibility expression and returns an AST and a slice with
// all compatibility labels found while building it.
func parse(input string) (*Node, []CompatField, error) {
	tokens := items(input)
	if len(tokens) == 0 {
		return nil, nil, errors.New("empty compatibility string")
	}
	lastItem := tokens[len(tokens)-1]
	if lastItem.Typ == ItemError {
		return nil, nil, fmt.Errorf("while parsing: %s", lastItem.Val)
	}
	p := parser{tokens: tokens}
	root, err := p.parseExpr()
	if err != nil {
		return nil, nil, err
	}
	return root, p.labels, nil
}

func (p *parser) parseExpr() (*Node, error) {
	left, err := p.parseAtom()
	if err != nil {
		return nil, err
	}

	return p.parseExprR(left)
}

func (p *parser) parseAtom() (*Node, error) {
	switch p.peekToken().Typ {
	case ItemString:
		return p.parseCompatLabel()
	case ItemLeftParen:
		// Consume '('
		p.nextToken()
		p.depth++
		node, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if p.peekToken().Typ != ItemRightParen {
			return nil, fmt.Errorf("expected right parenthesis, found %s", p.peekToken())
		}
		// Consume ')'
		p.nextToken()
		p.depth--
		return node, nil
	default:
		return nil, fmt.Errorf("unexpected token %s", p.peekToken())
	}
}

func (p *parser) parseExprR(left *Node) (*Node, error) {
	t := p.peekToken()
	switch t.Typ {
	case ItemOR:
		return p.parseOrExprR(left)
	case ItemAND:
		return p.parseAndExprR(left)
	case ItemRightParen:
		if p.depth == 0 {
			return nil, fmt.Errorf("unexpected right parenthesis")
		}
		return left, nil
	case ItemEOF:
		return left, nil
	default:
		return nil, fmt.Errorf("unexpected token %s", p.peekToken())
	}
}

func (p *parser) parseOrExprR(left *Node) (*Node, error) {
	if p.peekToken().Typ != ItemOR {
		return nil, fmt.Errorf("expected OR, found %s", p.peekToken())
	}
	orToken := p.nextToken()

	right, err := p.parseAtom()
	if err != nil {
		return nil, err
	}

	orNode := &Node{Exp: &Operator{Oper: orToken}, Left: left, Right: right}
	return p.parseOrExprROpt(orNode)
}

func (p *parser) parseOrExprROpt(left *Node) (*Node, error) {
	t := p.peekToken()
	switch t.Typ {
	case ItemOR:
		return p.parseOrExprR(left)
	case ItemAND:
		return nil, fmt.Errorf("unexpected AND after OR")
	default:
		return left, nil
	}
}

func (p *parser) parseAndExprR(left *Node) (*Node, error) {
	if p.peekToken().Typ != ItemAND {
		return nil, fmt.Errorf("expected AND, found %s", p.peekToken())
	}
	andToken := p.nextToken()

	right, err := p.parseAtom()
	if err != nil {
		return nil, err
	}

	andNode := &Node{Exp: &Operator{Oper: andToken}, Left: left, Right: right}
	return p.parseAndExprROpt(andNode)
}

func (p *parser) parseAndExprROpt(left *Node) (*Node, error) {
	t := p.peekToken()
	switch t.Typ {
	case ItemAND:
		return p.parseAndExprR(left)
	case ItemOR:
		return nil, fmt.Errorf("unexpected OR after AND")
	default:
		return left, nil
	}
}

func appendDimensionToLabel(label *CompatField, dim *CompatDimension) {
	if dim != nil {
		if len(dim.Values) == 0 {
			dim.Values = append(dim.Values, CompatRange{Min: 0, Max: 0})
		}
		label.Dimensions = append(label.Dimensions, *dim)
	}
}

func (p *parser) parseCompatLabel() (*Node, error) {
	compatLabel := &CompatField{}
	node := &Node{Exp: compatLabel}

	var err error
	var currDim *CompatDimension
	var singleInt, leftInt, rightInt uint64
	tags := map[string]bool{}
	// Here the lexer makes sure that the sequence of items is correct
intLoop:
	for {
		t := p.peekToken()
		switch t.Typ {
		case ItemString:
			appendDimensionToLabel(compatLabel, currDim)

			if len(t.Val) > 32 {
				return nil, fmt.Errorf("string is longer than 32 characters: %s", t.Val)
			}
			if _, ok := tags[t.Val]; ok {
				return nil, fmt.Errorf("repeated string in label: %s", t.Val)
			}
			currDim = &CompatDimension{Tag: t.Val}
			tags[t.Val] = true
		case ItemInteger:
			singleInt, err = strconv.ParseUint(t.Val, 10, 0)
			if err != nil {
				return nil, fmt.Errorf("cannot convert to an integer: %v", err)
			}
			currDim.Values = append(currDim.Values,
				CompatRange{Min: uint(singleInt), Max: uint(singleInt)})
		case ItemRangeLeftInt:
			leftInt, err = strconv.ParseUint(t.Val, 10, 0)
			if err != nil {
				return nil, fmt.Errorf("cannot convert to an integer: %v", err)
			}
		case ItemRangeRightInt:
			rightInt, err = strconv.ParseUint(t.Val, 10, 0)
			if err != nil {
				return nil, fmt.Errorf("cannot convert to an integer: %v", err)
			}
			if leftInt > rightInt {
				return nil, fmt.Errorf("negative range specified: (%d..%d)",
					leftInt, rightInt)
			}
			currDim.Values = append(currDim.Values,
				CompatRange{Min: uint(leftInt), Max: uint(rightInt)})
		case ItemEOF, ItemRightParen, ItemAND, ItemOR:
			break intLoop
		case ItemLeftParen:
			return nil, fmt.Errorf("open parenthesis after label")
		}
		p.nextToken()
	}
	appendDimensionToLabel(compatLabel, currDim)
	p.labels = append(p.labels, *compatLabel)

	return node, nil
}
