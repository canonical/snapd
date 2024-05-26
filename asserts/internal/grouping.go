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
	"fmt"
	"sort"

	"github.com/ddkwork/golibrary/mylog"
)

// Groupings maintain labels to identify membership to one or more groups.
// Labels are implemented as subsets of integers from 0
// up to an excluded maximum, where the integers represent the groups.
// Assumptions:
//   - most labels are for one group or very few
//   - a few labels are sparse with more groups in them
//   - very few comprise the universe of all groups
type Groupings struct {
	n               uint
	maxGroup        uint16
	bitsetThreshold uint16
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
	return &Groupings{n: uint(n), bitsetThreshold: uint16(n / 16)}, nil
}

// N returns up to how many groups are supported.
// That is the value that was passed to NewGroupings.
func (gr *Groupings) N() int {
	return int(gr.n)
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

// search locates group among the sorted Grouping elements, it returns:
//   - true if found
//   - false if not found
//   - the index at which group should be inserted to keep the
//     elements sorted if not found and the bit-set representation is not in use
func (gr *Groupings) search(g *Grouping, group uint16) (found bool, j uint16) {
	if g.size > gr.bitsetThreshold {
		return bitsetContains(g, group), 0
	}
	j = uint16(sort.Search(int(g.size), func(i int) bool { return g.elems[i] >= group }))
	if j < g.size && g.elems[j] == group {
		return true, 0
	}
	return false, j
}

func bitsetContains(g *Grouping, group uint16) bool {
	return (g.elems[group/16] & (1 << (group % 16))) != 0
}

// AddTo adds the given group to the grouping.
func (gr *Groupings) AddTo(g *Grouping, group uint16) error {
	mylog.Check(gr.WithinRange(group))

	if group > gr.maxGroup {
		gr.maxGroup = group
	}
	if g.size == 0 {
		g.size = 1
		g.elems = []uint16{group}
		return nil
	}
	found, j := gr.search(g, group)
	if found {
		return nil
	}
	newsize := g.size + 1
	if newsize > gr.bitsetThreshold {
		// switching to a bit-set representation after the size point
		// where the space cost is the same, the representation uses
		// bitsetThreshold-many 16-bits words stored in elems.
		// We don't always use the bit-set representation because
		// * we expect small groupings and iteration to be common,
		//   iteration is more costly over the bit-set representation
		// * serialization matches more or less what we do in memory,
		//   so again is more efficient for small groupings in the
		//   extensive representation.
		if g.size == gr.bitsetThreshold {
			prevelems := g.elems
			g.elems = make([]uint16, gr.bitsetThreshold)
			for _, e := range prevelems {
				bitsetAdd(g, e)
			}
		}
		g.size = newsize
		bitsetAdd(g, group)
		return nil
	}
	var newelems []uint16
	if int(g.size) == cap(g.elems) {
		newelems = make([]uint16, newsize, cap(g.elems)*2)
		copy(newelems, g.elems[:j])
	} else {
		newelems = g.elems[:newsize]
	}
	if j < g.size {
		copy(newelems[j+1:], g.elems[j:])
	}
	// inserting new group at j index keeping the elements sorted
	newelems[j] = group
	g.size = newsize
	g.elems = newelems
	return nil
}

func bitsetAdd(g *Grouping, group uint16) {
	g.elems[group/16] |= 1 << (group % 16)
}

// Contains returns whether the given group is a member of the grouping.
func (gr *Groupings) Contains(g *Grouping, group uint16) bool {
	found, _ := gr.search(g, group)
	return found
}

// Serialize produces a string encoding the given integers.
func Serialize(elems []uint16) string {
	b := bytes.NewBuffer(make([]byte, 0, len(elems)*2))
	binary.Write(b, binary.LittleEndian, elems)
	return base64.RawURLEncoding.EncodeToString(b.Bytes())
}

// Serialize produces a string representing the grouping label.
func (gr *Groupings) Serialize(g *Grouping) string {
	// groupings are serialized as:
	//  * the actual element groups if there are up to
	//    bitsetThreshold elements: elems[0], elems[1], ...
	//  * otherwise the number of elements, followed by the bitset
	//    representation comprised of bitsetThreshold-many 16-bits words
	//    (stored using elems as well)
	if g.size > gr.bitsetThreshold {
		return gr.bitsetSerialize(g)
	}
	return Serialize(g.elems)
}

func (gr *Groupings) bitsetSerialize(g *Grouping) string {
	b := bytes.NewBuffer(make([]byte, 0, (gr.bitsetThreshold+1)*2))
	binary.Write(b, binary.LittleEndian, g.size)
	binary.Write(b, binary.LittleEndian, g.elems)
	return base64.RawURLEncoding.EncodeToString(b.Bytes())
}

const errSerializedLabelFmt = "invalid serialized grouping label: %v"

// Deserialize reconstructs a grouping out of the serialized label.
func (gr *Groupings) Deserialize(label string) (*Grouping, error) {
	b := mylog.Check2(base64.RawURLEncoding.DecodeString(label))

	if len(b)%2 != 0 {
		return nil, fmt.Errorf(errSerializedLabelFmt, "not divisible into 16-bits words")
	}
	m := len(b) / 2
	var g Grouping
	if m == int(gr.bitsetThreshold+1) {
		// deserialize number of elements + bitset representation
		// comprising bitsetThreshold-many 16-bits words
		return gr.bitsetDeserialize(&g, b)
	}
	if m > int(gr.bitsetThreshold) {
		return nil, fmt.Errorf(errSerializedLabelFmt, "too large")
	}
	g.size = uint16(m)
	esz := uint16(1)
	for esz < g.size {
		esz *= 2
	}
	g.elems = make([]uint16, g.size, esz)
	binary.Read(bytes.NewBuffer(b), binary.LittleEndian, g.elems)
	for i, e := range g.elems {
		if e > gr.maxGroup {
			return nil, fmt.Errorf(errSerializedLabelFmt, "element larger than maximum group")
		}
		if i > 0 && g.elems[i-1] >= e {
			return nil, fmt.Errorf(errSerializedLabelFmt, "not sorted")
		}
	}
	return &g, nil
}

func (gr *Groupings) bitsetDeserialize(g *Grouping, b []byte) (*Grouping, error) {
	buf := bytes.NewBuffer(b)
	binary.Read(buf, binary.LittleEndian, &g.size)
	if g.size > gr.maxGroup+1 {
		return nil, fmt.Errorf(errSerializedLabelFmt, "bitset size cannot be possibly larger than maximum group plus 1")
	}
	if g.size <= gr.bitsetThreshold {
		// should not have used a bitset repr for so few elements
		return nil, fmt.Errorf(errSerializedLabelFmt, "bitset for too few elements")
	}
	g.elems = make([]uint16, gr.bitsetThreshold)
	binary.Read(buf, binary.LittleEndian, g.elems)
	return g, nil
}

// Iter iterates over the groups in the grouping and calls f with each of
// them. If f returns an error Iter immediately returns with it.
func (gr *Groupings) Iter(g *Grouping, f func(group uint16) error) error {
	if g.size > gr.bitsetThreshold {
		return gr.bitsetIter(g, f)
	}
	for _, e := range g.elems {
		mylog.Check(f(e))
	}
	return nil
}

func (gr *Groupings) bitsetIter(g *Grouping, f func(group uint16) error) error {
	c := g.size
	for i := uint16(0); i <= gr.maxGroup/16; i++ {
		w := g.elems[i]
		if w == 0 {
			continue
		}
		for j := uint16(0); w != 0; j++ {
			if w&1 != 0 {
				mylog.Check(f(i*16 + j))

				c--
				if c == 0 {
					// found all elements
					return nil
				}
			}
			w >>= 1
		}
	}
	return nil
}
