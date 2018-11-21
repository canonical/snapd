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

import (
	"sync"
)

// RingBuffer describes a fixed size ring buffer for storing performance samples.
type RingBuffer struct {
	mutex sync.RWMutex
	// start is the index of the first valid sample.
	start int
	// count is the number of valid samples.
	count int
	// data is a fixed-size slice of samples.
	// The range [start:start+count % cap] (which may wrap) is valid.
	data []Sample
}

// NewRingBuffer returns a ring buffer that can hold size samples.
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{data: make([]Sample, size)}
}

// Store appends a sample, possibly overwriting oldest one.
func (buf *RingBuffer) Store(sample *Sample) {
	if buf == nil {
		return
	}

	buf.mutex.Lock()
	defer buf.mutex.Unlock()

	idx := (buf.start + buf.count) % cap(buf.data)
	buf.data[idx] = *sample
	if buf.count < cap(buf.data) {
		buf.count++
	} else {
		buf.start = (buf.start + 1) % cap(buf.data)
	}
}

// StoreMany stores multiple samples quickly.
func (buf *RingBuffer) StoreMany(samples []Sample) {
	if buf == nil {
		return
	}

	buf.mutex.Lock()
	defer buf.mutex.Unlock()

	for _, sample := range samples {
		idx := (buf.start + buf.count) % cap(buf.data)
		buf.data[idx] = sample
		if buf.count < cap(buf.data) {
			buf.count++
		} else {
			buf.start = (buf.start + 1) % cap(buf.data)
		}
	}
}

// Samples returns a copy of all the samples.
func (buf *RingBuffer) Samples() []Sample {
	if buf == nil {
		return nil
	}

	buf.mutex.RLock()
	defer buf.mutex.RUnlock()

	result := make([]Sample, buf.count)
	left := copy(result, buf.data[buf.start:])
	copy(result[left:], buf.data[:buf.start])
	return result
}

// Filter returns a subset of samples that match given predicate.
func (buf *RingBuffer) Filter(pred func(*Sample) bool) []Sample {
	if buf == nil {
		return nil
	}

	buf.mutex.RLock()
	defer buf.mutex.RUnlock()

	var result []Sample
	for n := 0; n < buf.count; n++ {
		idx := (buf.start + n) % cap(buf.data)
		if pred(&buf.data[idx]) {
			result = append(result, buf.data[idx])
		}
	}
	return result
}
