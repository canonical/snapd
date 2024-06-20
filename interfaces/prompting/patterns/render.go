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

// renderConfig is a configuration of a render node.
type renderConfig interface {
	// NextEx modifies the configuration to the next state, if any remain.
	// Returns the length in bytes of the new configuration, the number of
	// bytes which remain unchanged since the previous configuration, and true
	// if more configurations remain to be rendered. The argument is always the
	// render node that was earlier used to obtain render configuration.
	NextEx(n renderNode) (length, lengthUnchanged int, moreRemain bool)
	// Length returns the total length of the rendered node in its current
	// configuration, including all sub-nodes if the node is a seq or alt.
	// The argument is always the render node that was earlier used to obtain
	// the render configuration.
	Length(n renderNode) int
}

// renderNode is a node which may be rendered in a particular configuration.
type renderNode interface {
	// render renders the given configuration to the buffer if alreadyWritten
	// equals 0. Otherwise, subtracts from alreadyWritten the length of the
	// string which would have been written to the buffer, and returns the
	// difference.
	Render(buf *bytes.Buffer, conf renderConfig, alreadyWritten int) int
	// Config returns the initial render configuration.
	Config() renderConfig
	// NumVariants returns the number of variants this node can be rendered as (recursively).
	NumVariants() int
	// nodeEqual returns true if two nodes are recursively equal.
	nodeEqual(renderNode) bool
}

// RenderAllVariants renders subsequent variants to a reusable buffer.
func RenderAllVariants(n renderNode, observe func(int, *bytes.Buffer)) {
	var buf bytes.Buffer
	var moreRemain bool

	c := n.Config()
	length := 0
	lengthUnchanged := 0

	for i := 0; ; i++ {
		// TODO: change how buffer is handled, so that we only need to re-render
		// the part that was invalidated by the old config.
		buf.Truncate(lengthUnchanged)
		n.Render(&buf, c, lengthUnchanged)
		observe(i, &buf)
		length, lengthUnchanged, moreRemain = c.NextEx(n)
		if !moreRemain {
			break
		}
		buf.Grow(length)
	}
}

// literal is a render node with a literal string.
type literal string

func (n literal) Render(buf *bytes.Buffer, conf renderConfig, alreadyWritten int) int {
	if alreadyWritten > 0 {
		return alreadyWritten - len(n)
	}
	buf.WriteString(string(n))
	return 0
}

func (n literal) NumVariants() int {
	return 1
}

func (n literal) Config() renderConfig {
	return literalConfig{}
}

func (n literal) nodeEqual(other renderNode) bool {
	if other, ok := other.(literal); ok {
		return n == other
	}

	return false
}

type literalConfig struct{}

func (literalConfig) NextEx(n renderNode) (length, lengthUnchanged int, moreRemain bool) {
	l := n.(literal)
	return len(l), 0, false
}

func (literalConfig) Length(n renderNode) int {
	l := n.(literal)
	return len(l)
}

// seq is sequence of consecutive render nodes.
type seq []renderNode

func (n seq) Render(buf *bytes.Buffer, conf renderConfig, alreadyWritten int) int {
	c := conf.(seqConfig)

	for i := range n {
		alreadyWritten = n[i].Render(buf, c[i], alreadyWritten)
	}

	return alreadyWritten
}

func (n seq) NumVariants() int {
	num := 1

	for i := range n {
		num *= n[i].NumVariants()
	}

	return num
}

func (n seq) Config() renderConfig {
	if len(n) == 0 {
		return seqConfig(nil)
	}

	c := make(seqConfig, len(n))

	for i := range n {
		c[i] = n[i].Config()
	}

	return c
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
		if v, ok := item.(literal); ok {
			if v == "" {
				continue
			}

			b.WriteString(string(v))
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

// seqConfig is the configuration for a seqeunce of render nodes.
//
// Each render node has a corresponding configuration at the same index.
type seqConfig []renderConfig

func (c seqConfig) NextEx(n renderNode) (length, lengthUnchanged int, moreRemain bool) {
	s := n.(seq)

	length = 0
	var i int
	for i = len(c) - 1; i >= 0; i-- {
		componentLength, componentLengthUnchanged, moreRemain := c[i].NextEx(s[i])
		if moreRemain {
			length += componentLength
			lengthUnchanged = componentLengthUnchanged
			break
		}
		// Reset the configuration for the node whose configs we just exhausted
		c[i] = s[i].Config()
		// Include the render length of the reset node in the total length
		length += c[i].Length(s[i])
	}
	if i < 0 {
		// No expansions remain for any node in the sequence
		return 0, 0, false
	}

	// We already counted c[i], so count j : 0 <= j < i
	for j := 0; j < i; j++ {
		componentLength := c[j].Length(s[j])
		length += componentLength
		lengthUnchanged += componentLength
	}

	return length, lengthUnchanged, true
}

func (c seqConfig) Length(n renderNode) int {
	s := n.(seq)

	totalLength := 0

	for i := 0; i < len(c); i++ {
		totalLength += c[i].Length(s[i])
	}

	return totalLength
}

// alt is a sequence of alternative render nodes.
type alt []renderNode

func (n alt) Render(buf *bytes.Buffer, conf renderConfig, alreadyWritten int) int {
	c := conf.(*altConfig)

	return n[c.idx].Render(buf, c.cfg, alreadyWritten)
}

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

func (n alt) Config() renderConfig {
	if len(n) == 0 {
		return nil
	}

	return &altConfig{
		idx: 0,
		cfg: n[0].Config(),
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

// altConfig is the configuration for an seqeunce of alternative of render nodes.
type altConfig struct {
	idx int          // index of the alternative currently being explored
	cfg renderConfig // config corresponding to the alternative being explored.
}

func (c *altConfig) NextEx(n renderNode) (length, lengthUnchanged int, moreRemain bool) {
	if c == nil {
		return 0, 0, false
	}

	a := n.(alt)

	// Keep exploring the current alternative until all possibilities are exhausted.
	if length, lengthUnchanged, moreRemain = c.cfg.NextEx(a[c.idx]); moreRemain {
		return length, lengthUnchanged, true
	}

	// Advance to the next alternative if one exists and obtain the initial render configuration for it.
	c.idx++

	if c.idx >= len(a) {
		return 0, 0, false
	}

	c.cfg = a[c.idx].Config()

	return c.cfg.Length(a[c.idx]), 0, true
}

func (c *altConfig) Length(n renderNode) int {
	if c == nil {
		return 0
	}

	a := n.(alt)

	return c.cfg.Length(a[c.idx])
}
