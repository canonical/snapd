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
	// Depth at which this node was processed, useful only for operators
	Depth int
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

// These parse operands
type parsePrefixFn func() (*Node, error)

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
	root, err := p.parseExpression()
	if err != nil {
		return nil, nil, err
	}
	return root, p.labels, nil
}

func (p *parser) parseExpression() (*Node, error) {
	t := p.peekToken()
	var parse parsePrefixFn
	switch t.Typ {
	case ItemString:
		parse = p.parseCompatLabel
	case ItemLeftParen:
		parse = p.parseGroupedExpression
	case ItemEOF:
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected token %s", t)
	}

	exp, err := parse()
	if err != nil {
		return nil, err
	}

	// Parse operators if present
	t = p.peekToken()
	switch t.Typ {
	case ItemAND, ItemOR:
		exp, err = p.parseOperator(exp)
		if err != nil {
			return nil, err
		}
	case ItemRightParen:
		if p.depth == 0 {
			return nil, errors.New("unexpected right parenthesis")
		}
	case ItemEOF:
	default:
		return nil, fmt.Errorf("unexpected token %s", t)
	}

	return exp, nil
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

func (p *parser) parseGroupedExpression() (*Node, error) {
	// Remove '('
	p.nextToken()
	p.depth++
	exp, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	// Remove ')'
	t := p.nextToken()
	p.depth--
	if t.Typ != ItemRightParen {
		return nil, errors.New("expected right parenthesis")
	}

	return exp, nil
}

func (p *parser) parseOperator(left *Node) (*Node, error) {
	op := p.nextToken()
	node := &Node{Exp: &Operator{Oper: op}, Depth: p.depth, Left: left}
	t := p.peekToken()
	switch t.Typ {
	// Only labels or left parenthesis after operator
	case ItemString, ItemLeftParen:
	default:
		return nil, fmt.Errorf("unexpected token after operator %s: %v", op, t)
	}

	var err error
	node.Right, err = p.parseExpression()
	if err != nil {
		return nil, err
	}

	// If our right expression is an operator different to the current one
	// and it happened at the current depth, return an error as we require
	// parenthesis to disambiguate in this case.
	oper, ok := node.Right.Exp.(*Operator)
	if ok && node.Right.Depth == p.depth && op.Typ != oper.Oper.Typ {
		return nil, errors.New("parenthesis required for disambiguation")
	}

	return node, nil
}
