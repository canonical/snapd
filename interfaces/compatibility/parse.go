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
// For each non-terminal, we have a method (note though that methods that
// handle AND and OR are almost identical and have been fused in one to avoid
// code duplication, so (And|Or)ExprR are implemented in parseOpExprR, and
// (And|Or)ExprROpt are implemented in parseOpExprROpt). The parser starts by
// calling Expr. FIRST is used to decide the next method to take. If ε is in
// FIRST, then we have to look also at what is in FOLLOW.
//
// For more details look at https://en.wikipedia.org/wiki/LL_parser
//
// Thanks to Valentin David for suggesting this grammar.

package compatibility

import (
	"errors"
	"fmt"
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
		return nil, nil, fmt.Errorf("while parsing: %s", lastItem.ErrMsg)
	}
	p := parser{tokens: tokens}
	root, err := p.parseExpr()
	if err != nil {
		return nil, nil, err
	}
	if p.peekToken().Typ != ItemEOF {
		return nil, nil, fmt.Errorf("unexpected string at the end: %s", p.peekToken())
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
	case ItemLabel:
		itLab := p.nextToken()
		p.labels = append(p.labels, itLab.Label)
		return &Node{Exp: &itLab.Label}, nil
	case ItemLeftParen:
		// Consume '('
		p.nextToken()
		node, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		// Consume ')'
		if p.nextToken().Typ != ItemRightParen {
			return nil, fmt.Errorf("expected right parenthesis, found %s", p.peekToken())
		}
		return node, nil
	default:
		return nil, fmt.Errorf("unexpected token %s", p.peekToken())
	}
}

func (p *parser) parseExprR(left *Node) (*Node, error) {
	t := p.peekToken()
	switch t.Typ {
	case ItemOR:
		return p.parseOpExprR(ItemOR, left)
	case ItemAND:
		return p.parseOpExprR(ItemAND, left)
	case ItemEOF, ItemRightParen:
		return left, nil
	default:
		return nil, fmt.Errorf("unexpected token %s", p.peekToken())
	}
}

func (p *parser) parseOpExprR(oper ItemType, left *Node) (*Node, error) {
	operToken := p.nextToken()
	if operToken.Typ != oper {
		return nil, fmt.Errorf("expected %s, found %s", oper, p.peekToken())
	}

	right, err := p.parseAtom()
	if err != nil {
		return nil, err
	}

	opNode := &Node{Exp: &Operator{Oper: operToken}, Left: left, Right: right}
	return p.parseOpExprROpt(oper, opNode)
}

func (p *parser) parseOpExprROpt(oper ItemType, left *Node) (*Node, error) {
	t := p.peekToken()
	switch t.Typ {
	case oper:
		return p.parseOpExprR(oper, left)
	case ItemRightParen, ItemEOF:
		return left, nil
	default:
		return nil, fmt.Errorf("unexpected item after %s expression: %s", oper, t)
	}
}
