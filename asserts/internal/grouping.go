// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package internal

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

// Groupings maintain labels to identify membership to one or more groups.
// Labels are implemented as subsets of integers from 0
// up to an excluded maximum, where the integers represent the groups.
// Assumptions:
//  - most labels are for one group or very few
//  - a few labels are sparse with more groups in them
//  - very few comprise the universe of all groups
type Groupings struct {
	n        uint
	maxGroup uint16
}

// NewGroupings creates a new Groupings supporting labels for membership
// to up n groups. n must be a positive multiple of 16 and <=65536.
func NewGroupings(n int) (*Groupings, error) {
	if n <= 0 || n > 65536 {
		return nil, fmt.Errorf("n=%d groups is outside of valid range (0, 65536]", n)
	}
	if n%16 != 0 {
		return nil, fmt.Errorf("n=%d groups is not a multiple of 16", n)
	}
	return &Groupings{n: uint(n)}, nil
}

// WithinRange checks whether group is within the admissible range for
// labeling otherwise it returns an error.
func (gr *Groupings) WithinRange(group uint16) error {
	if uint(group) >= gr.n {
		return fmt.Errorf("group exceeds admissible maximum: %d >= %d", group, gr.n)
	}
	return nil
}

type Grouping struct {
	size  uint16
	elems []uint16
}

func (g Grouping) Copy() Grouping {
	elems2 := make([]uint16, len(g.elems), cap(g.elems))
	copy(elems2[:], g.elems[:])
	g.elems = elems2
	return g
}

func (g *Grouping) search(group uint16) (found bool, j uint16) {
	j = uint16(sort.Search(int(g.size), func(i int) bool { return g.elems[i] >= group }))
	if j < g.size && g.elems[j] == group {
		return true, j
	}
	return false, j
}

// AddTo adds the given group to the grouping.
func (gr *Groupings) AddTo(g *Grouping, group uint16) error {
	if err := gr.WithinRange(group); err != nil {
		return err
	}
	if group > gr.maxGroup {
		gr.maxGroup = group
	}
	if g.size == 0 {
		g.size = 1
		g.elems = []uint16{group}
		return nil
	}
	// TODO: support using a bit-set representation after the size point
	// where the space cost is the same
	found, j := g.search(group)
	if found {
		return nil
	}
	var newelems []uint16
	if int(g.size) == cap(g.elems) {
		newelems = make([]uint16, g.size+1, cap(g.elems)*2)
		copy(newelems, g.elems[:j])
	} else {
		newelems = g.elems[:g.size+1]
	}
	if j < g.size {
		copy(newelems[j+1:], g.elems[j:])
	}
	newelems[j] = group
	g.size++
	g.elems = newelems
	return nil
}

// Contains returns whether the given group is a member of the grouping.
func (gr *Groupings) Contains(g *Grouping, group uint16) bool {
	found, _ := g.search(group)
	return found
}

// MakeLabel produces a string label encoding the given integers.
func MakeLabel(elems []uint16) string {
	b := bytes.NewBuffer(make([]byte, 0, len(elems)*2))
	binary.Write(b, binary.LittleEndian, elems)
	return base64.RawURLEncoding.EncodeToString(b.Bytes())
}

// Label produces a string label representing the grouping.
func (gr *Groupings) Label(g *Grouping) string {
	return MakeLabel(g.elems)
}

var errLabel = errors.New("invalid grouping label")

// Parse reconstructs a grouping out of the label.
func (gr *Groupings) Parse(label string) (*Grouping, error) {
	b, err := base64.RawURLEncoding.DecodeString(label)
	if err != nil {
		return nil, errLabel
	}
	if len(b)%2 != 0 {
		return nil, errLabel
	}
	var g Grouping
	g.size = uint16(len(b) / 2)
	esz := uint16(1)
	for esz < g.size {
		esz *= 2
	}
	g.elems = make([]uint16, g.size, esz)
	binary.Read(bytes.NewBuffer(b), binary.LittleEndian, g.elems)
	for i, e := range g.elems {
		if e > gr.maxGroup {
			return nil, errLabel
		}
		if i > 0 && g.elems[i-1] >= e {
			return nil, errLabel
		}
	}
	return &g, nil
}

// Iter iterates over the groups in the grouping and calls f with each of
// them. If f returns an error Iter immediately returns with it.
func (gr *Groupings) Iter(g *Grouping, f func(group uint16) error) error {
	for _, e := range g.elems {
		if err := f(e); err != nil {
			return err
		}
	}
	return nil
}
