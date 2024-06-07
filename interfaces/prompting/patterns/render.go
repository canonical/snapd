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
