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

// Package perf contains performance monitoring helpers.
package perf

import (
	"sync/atomic"
	"time"
)

// idCounter is the global counter for allocating sample IDs.
var idCounter uint64

// ringBuf is the process global ring buffer.
var ringBuf *RingBuffer = makeRingBuffer()

// Measure times the execution of a given function.
// The prototype sample is returned, amended with timing and sample ID.
func Measure(fn func(), proto *Sample) *Sample {
	start := time.Now()
	fn()
	end := time.Now()
	proto.ID = NextID()
	proto.StartTime = start
	proto.EndTime = end
	return proto
}

// NextID allocates and returns a sample identifier.
func NextID() uint64 {
	return nextIDRange(1)
}

// nextIDRange allocates a range of sample identifiers, returning the first one.
func nextIDRange(n uint64) uint64 {
	return atomic.AddUint64(&idCounter, n) - n
}

// reallocateIDs allocates a range of IDs for a set of samples.
func reallocateIDs(samples []Sample) {
	baseID := nextIDRange(uint64(len(samples)))
	for i := range samples {
		samples[i].ID = baseID + uint64(i)
	}
}

// StoreSample stores a sample in the in-memory ring buffer.
func StoreSample(sample *Sample) {
	ringBuf.Store(sample)
}

// MeasureAndStore conveniently combines Measure and StoreSample.
func MeasureAndStore(fn func(), proto *Sample) {
	Measure(fn, proto)
	StoreSample(proto)
}

// ReplaceRingBuffer replaces the in-memory performance monitoring ring buffer.
func ReplaceRingBuffer(buf *RingBuffer) {
	ringBuf = buf
}

// GetRingBuffer retrieves the in-memory performance monitoring ring buffer.
func GetRingBuffer() *RingBuffer {
	return ringBuf
}

func makeRingBuffer() *RingBuffer {
	return NewRingBuffer(1024)
}
