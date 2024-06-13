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
	"fmt"
	"strings"
)

// RenderConfig is a configuration of a render node.
type RenderConfig interface {
	// NextEx modifies the configuration to the next state returning true if more
	// configurations remain to be rendered. The argument is always the render
	// node that was earlier used to obtain render configuration.
	NextEx(n RenderNode) bool
}

// RenderNode is a node which may be rendered in a particular configuration.
type RenderNode interface {
	// Render renders the given configuration to the buffer.
	Render(*bytes.Buffer, RenderConfig)
	// Config returns the initial render configuration.
	Config() RenderConfig
	// NumVariants returns the number of variants this node can be rendered as (recursively).
	NumVariants() int
	// nodeEqual returns true if two nodes are recursively equal.
	nodeEqual(RenderNode) bool
}

// RenderAllVariants renders subsequent variants to a reusable buffer.
func RenderAllVariants(n RenderNode, observe func(int, *bytes.Buffer)) {
	var buf bytes.Buffer

	c := n.Config()

	for i := 0; ; i++ {
		// TODO: change how buffer is handled, so that we only need to re-render
		// the part that was invalidated by the old config.
		buf.Truncate(0)
		n.Render(&buf, c)
		observe(i, &buf)
		if !c.NextEx(n) {
			break
		}
	}
}

func VisitAllVariants(n RenderNode, observe func(i int, c RenderConfig)) {
	c := n.Config()

	for i := 0; ; i++ {
		observe(i, c)
		if !c.NextEx(n) {
			break
		}
	}
}

// Literal is a render node with a literal string.
type Literal string

func (n Literal) Render(buf *bytes.Buffer, conf RenderConfig) {
	buf.WriteString(string(n))
}

func (n Literal) NumVariants() int {
	return 1
}

func (n Literal) Config() RenderConfig {
	return literalConfig{}
}

func (n Literal) nodeEqual(other RenderNode) bool {
	if other, ok := other.(Literal); ok {
		return n == other
	}

	return false
}

type literalConfig struct{}

func (literalConfig) NextEx(_ RenderNode) bool {
	return false
}

func (literalConfig) GoString() string {
	return "_"
}

// Seq is sequence of consecutive render nodes.
type Seq []RenderNode

func (n Seq) Render(buf *bytes.Buffer, conf RenderConfig) {
	c := conf.(seqConfig)

	for i := range n {
		n[i].Render(buf, c[i])
	}
}

func (n Seq) NumVariants() int {
	num := 1

	for i := range n {
		num *= n[i].NumVariants()
	}

	return num
}

func (n Seq) Config() RenderConfig {
	if len(n) == 0 {
		return seqConfig(nil)
	}

	c := make(seqConfig, len(n))

	for i := range n {
		c[i] = n[i].Config()
	}

	return c
}

func (n Seq) nodeEqual(other RenderNode) bool {
	if other, ok := other.(Seq); ok {
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

// seqConfig is the configuration for a seqeunce of render nodes.
//
// Each render node has a corresponding configuration at the same index.
type seqConfig []RenderConfig

func (c seqConfig) NextEx(n RenderNode) bool {
	seq := n.(Seq)

	for i := len(c) - 1; i >= 0; i-- {
		if c[i].NextEx(seq[i]) {
			return true
		}

		c[i] = seq[i].Config()
	}

	return false
}

func (c seqConfig) GoString() string {
	var sb strings.Builder

	sb.WriteString("seqConfig{")

	for i := range c {
		if i > 0 {
			sb.WriteRune(',')
			sb.WriteRune(' ')
		}

		sb.WriteString(fmt.Sprintf("%#v", c[i]))
	}

	sb.WriteRune('}')

	return sb.String()
}

// Alt is a sequence of alternative render nodes.
type Alt []RenderNode

func (n Alt) Render(buf *bytes.Buffer, conf RenderConfig) {
	c := conf.(*altConfig)

	n[c.idx].Render(buf, c.cfg)
}

func (n Alt) NumVariants() int {
	num := 0

	for i := range n {
		num += n[i].NumVariants()
	}

	if num > 0 {
		return num
	}
	return 1
}

func (n Alt) Config() RenderConfig {
	if len(n) == 0 {
		return nil
	}

	return &altConfig{
		idx: 0,
		cfg: n[0].Config(),
	}
}

func (n Alt) nodeEqual(other RenderNode) bool {
	if other, ok := other.(Alt); ok {
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

func (alt Alt) reduceStrength() RenderNode {
	if len(alt) == 1 {
		return alt[0]
	}

	return alt
}

func (alt Alt) optimize() Alt {
	var seen []RenderNode
	var newAlt Alt

outer:
	for _, item := range alt {
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
	cfg RenderConfig // config corresponding to the alternative being explored.
}

func (c *altConfig) GoString() string {
	return fmt.Sprintf("altConfig{idx: %d, cfg: %#v}", c.idx, c.cfg)
}

func (c *altConfig) NextEx(n RenderNode) bool {
	if c == nil {
		return false
	}

	alt := n.(Alt)

	// Keep exploring the current alternative until all possibilities are exhausted.
	if c.cfg.NextEx(alt[c.idx]) {
		return true
	}

	// Advance to the next alternative if one exists and obtain the initial render configuration for it.
	c.idx++

	if c.idx >= len(alt) {
		return false
	}

	c.cfg = alt[c.idx].Config()

	return true
}
