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
	"errors"
	"strings"

	"github.com/snapcore/snapd/logger"
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
	// InitialVariant returns the initial variant state for this node, along
	// with its length.
	InitialVariant() (variantState, int)
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
func renderAllVariants(n renderNode, observe func(index int, variant PatternVariant)) {
	var buf bytes.Buffer

	v, length := n.InitialVariant()
	lengthUnchanged := 0
	moreRemain := true

	for i := 0; moreRemain; i++ {
		buf.Truncate(lengthUnchanged)
		buf.Grow(length - lengthUnchanged)
		v.Render(&buf, lengthUnchanged)
		variant, err := ParsePatternVariant(buf.String())
		if err != nil {
			// This should never occur
			logger.Noticef("patterns: cannot parse pattern variant '%s': %v", variant, err)
			continue
		}
		observe(i, variant)
		length, lengthUnchanged, moreRemain = v.NextVariant()
	}
}

// literal is a render node with a literal string.
//
// literal implements both renderNode and variantState, since literals can only
// be rendered one way, and thus only have one variant.
type literal string

func (n literal) NumVariants() int {
	return 1
}

func (n literal) InitialVariant() (variantState, int) {
	return n, len(n)
}

func (n literal) nodeEqual(other renderNode) bool {
	if other, ok := other.(literal); ok {
		return n == other
	}

	return false
}

func (n literal) Render(buf *bytes.Buffer, alreadyRendered int) int {
	if alreadyRendered > 0 {
		return alreadyRendered - len(n)
	}
	buf.WriteString(string(n))
	return 0
}

func (n literal) NextVariant() (length, lengthUnchanged int, moreRemain bool) {
	return 0, 0, false
}

func (n literal) Length() int {
	return len(n)
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

func (n seq) InitialVariant() (variantState, int) {
	v := &seqVariant{
		seq:        n,
		elements:   make([]variantState, len(n)),
		currLength: 0,
	}

	for i := range n {
		var length int
		v.elements[i], length = n[i].InitialVariant()
		v.currLength += length
	}

	return v, v.currLength
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

		return true
	}

	return false
}

// optimize for seq joins adjacent literals into a single node, reduces an
// empty seq to a literal "", and reduces a seq with one element to that
// element.
func (n seq) optimize() (renderNode, error) {
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

	// Reduce strength
	switch len(newSeq) {
	case 0:
		return literal(""), nil
	case 1:
		return newSeq[0], nil
	}

	return newSeq, nil
}

// seqVariant is the variant state for a seqeunce of render nodes.
//
// Each render node in the seq has a corresponding variant at the same index.
// of the elements list.
type seqVariant struct {
	seq        seq
	elements   []variantState
	currLength int
}

func (v *seqVariant) Render(buf *bytes.Buffer, alreadyRendered int) int {
	for _, element := range v.elements {
		alreadyRendered = element.Render(buf, alreadyRendered)
	}

	return alreadyRendered
}

func (v *seqVariant) NextVariant() (length, lengthUnchanged int, moreRemain bool) {
	var i int
	for i = len(v.elements) - 1; i >= 0; i-- {
		componentLength, componentLengthUnchanged, componentMoreRemain := v.elements[i].NextVariant()
		if componentMoreRemain {
			length += componentLength
			lengthUnchanged = componentLengthUnchanged
			moreRemain = true
			break
		}
		// Reset the variant state for the node whose variants we just exhausted
		v.elements[i], componentLength = v.seq[i].InitialVariant()
		// Include the render length of the reset node in the total length
		length += componentLength
	}
	if !moreRemain {
		// No expansions remain for any node in the sequence
		return 0, 0, false
	}

	// We already counted v.elements[i], so count j : 0 <= j < i
	for j := 0; j < i; j++ {
		componentLength := v.elements[j].Length()
		length += componentLength
		lengthUnchanged += componentLength
	}

	v.currLength = length

	return length, lengthUnchanged, true
}

func (v *seqVariant) Length() int {
	return v.currLength
}

// alt is a sequence of alternative render nodes.
type alt []renderNode

func (n alt) NumVariants() int {
	num := 0

	for i := range n {
		num += n[i].NumVariants()
	}

	return num
}

func (n alt) InitialVariant() (variantState, int) {
	// alt can't have zero length, since even "{}" would be parsed as
	// alt{ seq{ literal("") } }, which is optimized to alt{ literal("") },
	// which is optimized to literal("").
	// An error was thrown by optimize rather than return a 0-length alt.
	currVariant, length := n[0].InitialVariant()
	return &altVariant{
		alt:     n,
		idx:     0,
		variant: currVariant,
	}, length
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

		return true
	}

	return false
}

// optimize for alt eliminates equivalent items and reduces alts with one item
// to that item.
func (n alt) optimize() (renderNode, error) {
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

	// Reduce strength
	switch len(newAlt) {
	case 0:
		// Should not occur, since even "{}" would be parsed as
		// alt{ seq{ literal("") } }, which is optimized to alt{ literal("") }.
		return nil, errors.New("internal error: attempted to optimize 0-length alt")
	case 1:
		return newAlt[0], nil
	}

	return newAlt, nil
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

	v.variant, length = v.alt[v.idx].InitialVariant()

	return length, 0, true
}

func (v *altVariant) Length() int {
	return v.variant.Length()
}
