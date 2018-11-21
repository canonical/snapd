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

package perf

func (buf *RingBuffer) Start() int {
	return buf.start
}

func (buf *RingBuffer) Count() int {
	return buf.count
}

func (buf *RingBuffer) Data() []Sample {
	return buf.data
}

func CurrentID() uint64 {
	return idCounter
}

func NextIDRange(n uint64) uint64 {
	return nextIDRange(n)
}

func ResetNextID() {
	idCounter = 0
}

func ResetRingBuffer() {
	ringBuf = makeRingBuffer()
}

func ReallocateIDs(samples []Sample) {
	reallocateIDs(samples)
}
