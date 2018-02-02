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
	"io"
)

type BufferLimitError struct{}

func (e BufferLimitError) Error() string {
	return fmt.Sprintf("buffer capacity exceeded")
}

type LimitedWriter struct {
	writer             io.Writer
	limit              int
	limitReached       bool
	written            int
	BufferLimitReached chan struct{}
}

func NewLimitedWriter(writer io.Writer, limit int) *LimitedWriter {
	return &LimitedWriter{
		writer:             writer,
		limit:              limit,
		BufferLimitReached: make(chan struct{}),
	}
}

func (wr *LimitedWriter) Write(data []byte) (int, error) {
	n := len(data) + wr.written
	if n > wr.limit {
		// in case Write is called after error - closing channel again
		// would panic
		if !wr.limitReached {
			close(wr.BufferLimitReached)
		}
		wr.limitReached = true
		return 0, &BufferLimitError{}
	}

	sz, err := wr.writer.Write(data)
	wr.written += sz
	return sz, err
}
