// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"fmt"
	"strconv"
	"strings"
)

// RangeSpan represents a span of numbers inside a range. A span with
// equal Start and End describes a span of a single number.
type RangeSpan struct {
	Start, End int
}

// Intersets checks if passed span intersects with this span.
func (s1 RangeSpan) Intersets(s2 RangeSpan) bool {
	return (s1.Start <= s2.End) && (s2.Start <= s1.End)
}

func (s RangeSpan) Size() int {
	return s.End - s.Start + 1
}

// Range of discrete numbers represented as set of non overlapping spans.
type Range struct {
	// Spans within the range, ordered by Start.
	Spans []RangeSpan
}

// Intersets checks if passed span intersects with this range of spans.
func (r Range) Intersets(s RangeSpan) bool {
	for _, rangeSpan := range r.Spans {
		if rangeSpan.Intersets(s) {
			return true
		}
	}
	return false
}

func (r Range) Size() (size int) {
	for _, s := range r.Spans {
		size += s.Size()
	}
	return size
}

// ParseRange parses a range represented as a string. The entries are
// joining them with a comma: n[,m] or as a range: n-m or a combination
// of both, assuming the ranges do not overlap, e.g.: n,m,x-y.
func ParseRange(in string) (Range, error) {
	tokens := strings.Split(in, ",")
	r := Range{}
	for _, token := range tokens {
		s, err := parseRangeSpan(token)
		if err != nil {
			return Range{}, err
		}
		if r.Intersets(s) {
			return Range{}, fmt.Errorf("overlapping range span found %q", token)
		}
		r.Spans = append(r.Spans, s)
	}
	return r, nil
}

func parseRangeSpan(in string) (RangeSpan, error) {
	hasNegativeStart := strings.HasPrefix(in, "-")
	in = strings.TrimPrefix(in, "-")
	if !strings.Contains(in, "-") {
		val, err := strconv.ParseInt(in, 10, 32)
		if err != nil {
			return RangeSpan{}, err
		}
		if hasNegativeStart {
			val = -val
		}
		return RangeSpan{int(val), int(val)}, nil
	}
	// Parse range e.g. 2-5
	tokens := strings.SplitN(in, "-", 2)
	if len(tokens) != 2 {
		return RangeSpan{}, fmt.Errorf("invalid range %q", in)
	}
	start, err := strconv.ParseInt(tokens[0], 10, 32)
	if err != nil {
		return RangeSpan{}, fmt.Errorf("invalid range %q: %w", in, err)
	}
	if hasNegativeStart {
		start = -start
	}
	end, err := strconv.ParseInt(tokens[1], 10, 32)
	if err != nil {
		return RangeSpan{}, fmt.Errorf("invalid range %q: %w", in, err)
	}
	if end <= start {
		return RangeSpan{}, fmt.Errorf("invalid range %q: range end has to be larger than range start", in)
	}
	return RangeSpan{int(start), int(end)}, nil
}
