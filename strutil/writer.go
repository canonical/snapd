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

type LimitedWriter struct {
	buffer   *bytes.Buffer
	maxBytes int
	written  int
}

func NewLimitedWriter(buffer *bytes.Buffer, maxBytes int) *LimitedWriter {
	return &LimitedWriter{
		buffer:   buffer,
		maxBytes: maxBytes,
	}
}

func (wr *LimitedWriter) Write(data []byte) (int, error) {
	sz, err := wr.buffer.Write(data)
	if err != nil {
		return 0, err
	}
	wr.written += sz
	if wr.written > wr.maxBytes {
		data := wr.buffer.Bytes()
		data = TruncateOutput(data, 0, wr.maxBytes)
		copy(wr.buffer.Bytes(), data)
		wr.written = len(data)
		wr.buffer.Truncate(wr.written)
	}
	return sz, err
}
