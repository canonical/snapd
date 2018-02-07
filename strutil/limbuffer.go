// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package strutil

import (
	"bytes"
)

type LimitedBuffer struct {
	buffer   *bytes.Buffer
	maxLines int
	maxBytes int
	written  int
}

func NewLimitedBuffer(maxLines, maxBytes int) *LimitedBuffer {
	return &LimitedBuffer{
		buffer:   &bytes.Buffer{},
		maxLines: maxLines,
		maxBytes: maxBytes,
	}
}

func (wr *LimitedBuffer) Write(data []byte) (int, error) {
	sz, err := wr.buffer.Write(data)
	if err != nil {
		return 0, err
	}
	wr.written += sz
	if wr.written > wr.maxBytes {
		data := wr.buffer.Bytes()
		// don't limit by number of lines, we will apply line limit in Bytes(),
		// also, limit by 2 times the requested number of bytes at this point.
		data = TruncateOutput(data, 0, 2*wr.maxBytes)
		copy(wr.buffer.Bytes(), data)
		wr.written = len(data)
		wr.buffer.Truncate(wr.written)
	}
	return sz, err
}

func (wr *LimitedBuffer) Bytes() []byte {
	return TruncateOutput(wr.buffer.Bytes(), wr.maxLines, wr.maxBytes)
}
