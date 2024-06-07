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

import "bytes"

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
