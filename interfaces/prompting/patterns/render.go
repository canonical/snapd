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
	"bytes"
	"strings"
)

// variantState is the current variant of a render node.
type variantState interface {
	// Render renders the variant to the buffer if alreadyRendered equals 0 or
	// the node is not a literal. The alreadyRendered parameter is the number
	// of bytes of the variant which have already been rendered because they
	// are unchanged from the previous variant.
	//
	// If alreadyRendered is greater than 0 and the associated renderNode is
	// a literal, subtract from alreadyRendered the length in bytes of the
	// literal and return the difference.
	Render(buf *bytes.Buffer, alreadyRendered int) int
	// NextVariant modifies the variant to the next state, if any remain.
	// Returns the total length in bytes of the new variant, the number of
	// bytes which remain unchanged since the previous variant, and true
	// if more variants remain to be rendered.
	NextVariant() (length, lengthUnchanged int, moreRemain bool)
	// Length returns the total length of the rendered node in its current
	// variant state, including all sub-nodes if the node is a seq or alt.
	Length() int
}

// renderNode is a node which may be rendered in a particular variant state.
type renderNode interface {
	// InitialVariant returns the initial variant state for this node.
	InitialVariant() variantState
	// NumVariants returns the number of variants this node can be rendered as (recursively).
	NumVariants() int
	// nodeEqual returns true if two nodes are recursively equal.
	nodeEqual(renderNode) bool
}

// renderAllVariants renders subsequent variants to a reusable buffer.
//
// Each variant is a fully expanded path pattern, with one alternative chosen
// for each group in the path pattern. The given observe closure is then called
// on each variant, along with its index, and it can perform some action with
// the variant, such as adding it to a data structure.
func renderAllVariants(n renderNode, observe func(index int, variant string)) {
	var buf bytes.Buffer
	var moreRemain bool

	v := n.InitialVariant()
	length := 0
	lengthUnchanged := 0

	for i := 0; ; i++ {
		buf.Truncate(lengthUnchanged)
		v.Render(&buf, lengthUnchanged)
		observe(i, buf.String())
		length, lengthUnchanged, moreRemain = v.NextVariant()
		if !moreRemain {
			break
		}
		buf.Grow(length)
	}
}

// literal is a render node with a literal string.
type literal string

func (n literal) NumVariants() int {
	return 1
}

func (n literal) InitialVariant() variantState {
	return &literalVariant{
		literal: n,
	}
}

func (n literal) nodeEqual(other renderNode) bool {
	if other, ok := other.(literal); ok {
		return n == other
	}

	return false
}

type literalVariant struct {
	literal literal
}

func (v *literalVariant) Render(buf *bytes.Buffer, alreadyRendered int) int {
	if alreadyRendered > 0 {
		return alreadyRendered - len(v.literal)
	}
	buf.WriteString(string(v.literal))
	return 0
}

func (v *literalVariant) NextVariant() (length, lengthUnchanged int, moreRemain bool) {
	return len(v.literal), 0, false
}

func (v *literalVariant) Length() int {
	return len(v.literal)
}

// seq is sequence of consecutive render nodes.
type seq []renderNode

func (n seq) NumVariants() int {
	num := 1

	for i := range n {
		num *= n[i].NumVariants()
	}

	return num
}

func (n seq) InitialVariant() variantState {
	if len(n) == 0 {
		// nil seq, nil elements
		return &seqVariant{}
	}

	v := &seqVariant{
		seq:      n,
		elements: make([]variantState, len(n)),
	}

	for i := range n {
		v.elements[i] = n[i].InitialVariant()
	}

	return v
}

func (n seq) nodeEqual(other renderNode) bool {
	if other, ok := other.(seq); ok {
		if len(other) != len(n) {
			return false
		}

		for i := range n {
			if !n[i].nodeEqual(other[i]) {
				return false
			}
		}
	}

	return false
}

func (n seq) reduceStrength() renderNode {
	switch len(n) {
	case 0:
		return literal("")
	case 1:
		return n[0]
	default:
		return n
	}
}

func (n seq) optimize() seq {
	var b strings.Builder

	var newSeq seq

	for _, item := range n {
		if l, ok := item.(literal); ok {
			if l == "" {
				continue
			}

			b.WriteString(string(l))
		} else {
			if b.Len() > 0 {
				newSeq = append(newSeq, literal(b.String()))
				b.Reset()
			}

			newSeq = append(newSeq, item)
		}
	}

	if b.Len() > 0 {
		newSeq = append(newSeq, literal(b.String()))
		b.Reset()
	}

	return newSeq
}

// seqVariant is the variant state for a seqeunce of render nodes.
//
// Each render node in the seq has a corresponding variant at the same index.
// of the elements list.
type seqVariant struct {
	seq      seq
	elements []variantState
}

func (v *seqVariant) Render(buf *bytes.Buffer, alreadyRendered int) int {
	for _, element := range v.elements {
		alreadyRendered = element.Render(buf, alreadyRendered)
	}

	return alreadyRendered
}

func (v *seqVariant) NextVariant() (length, lengthUnchanged int, moreRemain bool) {
	length = 0
	var i int
	for i = len(v.elements) - 1; i >= 0; i-- {
		componentLength, componentLengthUnchanged, moreRemain := v.elements[i].NextVariant()
		if moreRemain {
			length += componentLength
			lengthUnchanged = componentLengthUnchanged
			break
		}
		// Reset the variant state for the node whose variants we just exhausted
		v.elements[i] = v.seq[i].InitialVariant()
		// Include the render length of the reset node in the total length
		length += v.elements[i].Length()
	}
	if i < 0 {
		// No expansions remain for any node in the sequence
		return 0, 0, false
	}

	// We already counted v.elements[i], so count j : 0 <= j < i
	for j := 0; j < i; j++ {
		componentLength := v.elements[j].Length()
		length += componentLength
		lengthUnchanged += componentLength
	}

	return length, lengthUnchanged, true
}

func (v *seqVariant) Length() int {
	totalLength := 0

	for _, element := range v.elements {
		totalLength += element.Length()
	}

	return totalLength
}

// alt is a sequence of alternative render nodes.
type alt []renderNode

func (n alt) NumVariants() int {
	num := 0

	for i := range n {
		num += n[i].NumVariants()
	}

	if num > 0 {
		return num
	}
	return 1
}

func (n alt) InitialVariant() variantState {
	if len(n) == 0 {
		return nil
	}

	return &altVariant{
		alt:     n,
		idx:     0,
		variant: n[0].InitialVariant(),
	}
}

func (n alt) nodeEqual(other renderNode) bool {
	if other, ok := other.(alt); ok {
		if len(other) != len(n) {
			return false
		}

		for i := range n {
			if !n[i].nodeEqual(other[i]) {
				return false
			}
		}
	}

	return false
}

func (n alt) reduceStrength() renderNode {
	if len(n) == 1 {
		return n[0]
	}

	return n
}

func (n alt) optimize() alt {
	var seen []renderNode
	var newAlt alt

outer:
	for _, item := range n {
		for _, seenItem := range seen {
			if seenItem.nodeEqual(item) {
				continue outer
			}
		}

		seen = append(seen, item)
		newAlt = append(newAlt, item)
	}

	return newAlt
}

// altVariant is the variant state for an set of alternative render nodes.
type altVariant struct {
	alt     alt          // Alt associated with this variant state.
	idx     int          // Index of the alternative currently being explored.
	variant variantState // Variant corresponding to the alternative currently being explored.
}

func (v *altVariant) Render(buf *bytes.Buffer, alreadyRendered int) int {
	return v.variant.Render(buf, alreadyRendered)
}

func (v *altVariant) NextVariant() (length, lengthUnchanged int, moreRemain bool) {
	// Keep exploring the current alternative until all possibilities are exhausted.
	if length, lengthUnchanged, moreRemain = v.variant.NextVariant(); moreRemain {
		return length, lengthUnchanged, true
	}

	// Advance to the next alternative if one exists and obtain the initial
	// variant state for it.

	v.idx++
	if v.idx >= len(v.alt) {
		return 0, 0, false
	}

	v.variant = v.alt[v.idx].InitialVariant()

	return v.variant.Length(), 0, true
}

func (v *altVariant) Length() int {
	return v.variant.Length()
}
