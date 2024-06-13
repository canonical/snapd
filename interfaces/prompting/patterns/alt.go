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
)

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
