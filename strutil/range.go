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
	"sort"
	"strconv"
	"strings"
)

// RangeSpan represents a span of numbers inside a range. A span with
// equal Start and End describes a span of a single number.
type RangeSpan struct {
	Start, End uint
}

// Intersects checks if passed span intersects with this span.
func (s1 RangeSpan) Intersects(s2 RangeSpan) bool {
	return (s1.Start <= s2.End) && (s2.Start <= s1.End)
}

// Size is the length of the range span from start to end inclusive.
func (s RangeSpan) Size() int {
	return int(s.End) - int(s.Start) + 1
}

// Returns the string representation of the span.
func (s RangeSpan) String() string {
	if s.Size() == 1 {
		return strconv.FormatUint(uint64(s.Start), 10)
	}
	return strconv.FormatUint(uint64(s.Start), 10) + "-" + strconv.FormatUint(uint64(s.End), 10)
}

func parseRangeSpan(in string) (RangeSpan, error) {
	if !strings.Contains(in, "-") {
		val, err := strconv.ParseUint(in, 10, 32)
		if err != nil {
			return RangeSpan{}, err
		}
		return RangeSpan{uint(val), uint(val)}, nil
	}
	// Parse range e.g. 2-5
	tokens := strings.SplitN(in, "-", 2)
	if len(tokens) != 2 {
		return RangeSpan{}, fmt.Errorf("invalid range span %q", in)
	}
	start, err := strconv.ParseUint(tokens[0], 10, 32)
	if err != nil {
		return RangeSpan{}, fmt.Errorf("invalid range span start %q: %w", in, err)
	}
	end, err := strconv.ParseUint(tokens[1], 10, 32)
	if err != nil {
		return RangeSpan{}, fmt.Errorf("invalid range span end %q: %w", in, err)
	}
	if end <= start {
		return RangeSpan{}, fmt.Errorf("invalid range span %q: ends before it starts", in)
	}
	return RangeSpan{uint(start), uint(end)}, nil
}

// Range of discrete numbers represented as a set of non overlapping RangeSpan(s).
type Range []RangeSpan

// Intersects checks if passed span intersects with this range of spans.
func (r Range) Intersects(s RangeSpan) bool {
	for _, rangeSpan := range r {
		if rangeSpan.Intersects(s) {
			return true
		}
	}
	return false
}

// Size is the sum of sizes of all range spans in the range.
func (r Range) Size() (size int) {
	for _, s := range r {
		size += s.Size()
	}
	return size
}

// Returns the comma-separated string representation of the underlying range, e.g.: n,m,x-y.
func (r Range) String() string {
	var commaSeparated strings.Builder
	size := len(r)
	for i, span := range r {
		commaSeparated.WriteString(span.String())
		if i != size-1 {
			commaSeparated.WriteRune(',')
		}
	}
	return commaSeparated.String()
}

// ParseRange parses a range represented as a string. The entries are joining
// them with a comma: n[,m] or as a range: n-m or a combination of both, assuming
// the ranges are non-negative and do not overlap, e.g.: n,m,x-y.
func ParseRange(input string) (Range, error) {
	tokens := strings.Split(input, ",")
	r := Range{}
	for _, token := range tokens {
		s, err := parseRangeSpan(token)
		if err != nil {
			return nil, err
		}
		if r.Intersects(s) {
			return nil, fmt.Errorf("overlapping range span found %q", token)
		}
		r = append(r, s)
	}
	sort.Slice(r, func(i, j int) bool { return r[i].Start < r[j].Start })
	return r, nil
}
