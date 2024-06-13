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
