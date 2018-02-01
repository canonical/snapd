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

package osutil

import (
	"fmt"
)

type BufferLimitError struct{}

func (e BufferLimitError) Error() string {
	return fmt.Sprintf("buffer capacity exceeded")
}

type LimitedWriter struct {
	buf                []byte
	limitReached       bool
	BufferLimitReached chan struct{}
}

func NewLimitedWriter(capacity int) *LimitedWriter {
	return &LimitedWriter{
		buf:                make([]byte, 0, capacity),
		BufferLimitReached: make(chan struct{}),
	}
}

func (wr *LimitedWriter) Write(p []byte) (int, error) {
	m := len(wr.buf)
	n := len(p) + m
	if n > cap(wr.buf) {
		if !wr.limitReached {
			close(wr.BufferLimitReached)
		}
		wr.limitReached = true
		return 0, &BufferLimitError{}
	}
	wr.buf = wr.buf[0:n]
	copy(wr.buf[m:], p)
	return len(p), nil
}

func (wr *LimitedWriter) Bytes() []byte {
	return wr.buf
}
