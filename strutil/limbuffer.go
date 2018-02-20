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
}

func NewLimitedBuffer(maxLines, maxBytes int) *LimitedBuffer {
	return &LimitedBuffer{
		buffer:   &bytes.Buffer{},
		maxLines: maxLines,
		maxBytes: maxBytes,
	}
}

func (wr *LimitedBuffer) Write(data []byte) (int, error) {
	// Do an early (and inaccurate) truncate here to save memory,
	// the exact truncate happens in the .Bytes() function;
	// also, limit by 2 times the requested number of bytes at this point.
	lim := wr.maxBytes * 2
	dlen := len(data)

	// simple case, still within limits, nothing to truncate, just write
	if wr.buffer.Len()+dlen < lim {
		return wr.buffer.Write(data)
	}

	// simple case - data to write is outright too big,
	// just take its tail and overwrite internal buffer
	if dlen > lim {
		wr.buffer.Truncate(0)
		_, err := wr.buffer.Write(data[dlen-lim:])
		if err != nil {
			return 0, err
		}
		return dlen, err
	}

	// typical case - shift the buffer by (lim-dlen) bytes to make room for the data
	tmp := TruncateOutput(wr.buffer.Bytes(), 0, lim-dlen)
	copy(wr.buffer.Bytes(), tmp)
	wr.buffer.Truncate(len(tmp))
	_, err := wr.buffer.Write(data)
	if err != nil {
		return 0, err
	}
	return dlen, err
}

func (wr *LimitedBuffer) Bytes() []byte {
	return TruncateOutput(wr.buffer.Bytes(), wr.maxLines, wr.maxBytes)
}
