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

package internal_test

import (
	"encoding/base64"
	"errors"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts/internal"
)

func TestInternal(t *testing.T) { TestingT(t) }

type groupingsSuite struct{}

var _ = Suite(&groupingsSuite{})

func (s *groupingsSuite) TestNewGroupings(c *C) {
	tests := []struct {
		n   int
		err string
	}{
		{-10, `n=-10 groups is outside of valid range \(0, 65536\]`},
		{0, `n=0 groups is outside of valid range \(0, 65536\]`},
		{9, "n=9 groups is not a multiple of 16"},
		{16, ""},
		{255, "n=255 groups is not a multiple of 16"},
		{256, ""},
		{1024, ""},
		{65536, ""},
		{65537, `n=65537 groups is outside of valid range \(0, 65536\]`},
	}

	for _, t := range tests {
		comm := Commentf("%d", t.n)
		gr := mylog.Check2(internal.NewGroupings(t.n))
		if t.err == "" {
			c.Check(err, IsNil, comm)
			c.Check(gr, NotNil, comm)
			c.Check(gr.N(), Equals, t.n)
		} else {
			c.Check(gr, IsNil, comm)
			c.Check(err, ErrorMatches, t.err, comm)
		}
	}
}

func (s *groupingsSuite) TestAddToAndContains(c *C) {
	var g internal.Grouping

	gr := mylog.Check2(internal.NewGroupings(16))

	mylog.Check(gr.AddTo(&g, 1))

	mylog.Check(gr.AddTo(&g, 3))

	mylog.Check(gr.AddTo(&g, 0))

	mylog.Check(gr.AddTo(&g, 4))

	mylog.Check(gr.AddTo(&g, 2))


	for i := uint16(0); i < 5; i++ {
		c.Check(gr.Contains(&g, i), Equals, true)
	}

	c.Check(gr.Contains(&g, 5), Equals, false)
}

func (s *groupingsSuite) TestOutsideRange(c *C) {
	var g internal.Grouping

	gr := mylog.Check2(internal.NewGroupings(16))

	mylog.

		// validity
		Check(gr.AddTo(&g, 15))

	mylog.Check(gr.AddTo(&g, 16))
	c.Check(err, ErrorMatches, "group exceeds admissible maximum: 16 >= 16")
	mylog.Check(gr.AddTo(&g, 99))
	c.Check(err, ErrorMatches, "group exceeds admissible maximum: 99 >= 16")
}

func (s *groupingsSuite) TestSerializeLabel(c *C) {
	var g internal.Grouping

	gr := mylog.Check2(internal.NewGroupings(128))

	mylog.Check(gr.AddTo(&g, 1))

	mylog.Check(gr.AddTo(&g, 3))

	mylog.Check(gr.AddTo(&g, 0))

	mylog.Check(gr.AddTo(&g, 4))

	mylog.Check(gr.AddTo(&g, 2))


	l := gr.Serialize(&g)

	g1 := mylog.Check2(gr.Deserialize(l))
	c.Check(err, IsNil)

	c.Check(g1, DeepEquals, &g)
}

func (s *groupingsSuite) TestDeserializeLabelErrors(c *C) {
	var g internal.Grouping

	gr := mylog.Check2(internal.NewGroupings(64))

	mylog.Check(gr.AddTo(&g, 0))

	mylog.Check(gr.AddTo(&g, 1))

	mylog.Check(gr.AddTo(&g, 2))

	mylog.Check(gr.AddTo(&g, 3))

	mylog.Check(gr.AddTo(&g, 4))


	const errPrefix = "invalid serialized grouping label: "

	invalidLabels := []struct {
		invalid, errSuffix string
	}{
		// not base64
		{"\x0a\x02\xf4", `illegal base64 data.*`},
		// wrong length
		{base64.RawURLEncoding.EncodeToString([]byte{1}), `not divisible into 16-bits words`},
		// not a known group
		{internal.Serialize([]uint16{5}), `element larger than maximum group`},
		// not in order
		{internal.Serialize([]uint16{0, 2, 1}), `not sorted`},
		// bitset: too many words
		{internal.Serialize([]uint16{0, 0, 0, 0, 0, 0}), `too large`},
		// bitset: larger than maxgroup
		{internal.Serialize([]uint16{6, 0, 0, 0, 0}), `bitset size cannot be possibly larger than maximum group plus 1`},
		// bitset: grouping size is too small
		{internal.Serialize([]uint16{0, 0, 0, 0, 0}), `bitset for too few elements`},
		{internal.Serialize([]uint16{1, 0, 0, 0, 0}), `bitset for too few elements`},
		{internal.Serialize([]uint16{4, 0, 0, 0, 0}), `bitset for too few elements`},
	}

	for _, il := range invalidLabels {
		_ := mylog.Check2(gr.Deserialize(il.invalid))
		c.Check(err, ErrorMatches, errPrefix+il.errSuffix)
	}
}

func (s *groupingsSuite) TestIter(c *C) {
	var g internal.Grouping

	gr := mylog.Check2(internal.NewGroupings(128))

	mylog.Check(gr.AddTo(&g, 1))

	mylog.Check(gr.AddTo(&g, 3))

	mylog.Check(gr.AddTo(&g, 0))

	mylog.Check(gr.AddTo(&g, 4))

	mylog.Check(gr.AddTo(&g, 2))


	elems := []uint16{}
	f := func(group uint16) error {
		elems = append(elems, group)
		return nil
	}
	mylog.Check(gr.Iter(&g, f))

	c.Check(elems, DeepEquals, []uint16{0, 1, 2, 3, 4})
}

func (s *groupingsSuite) TestIterError(c *C) {
	var g internal.Grouping

	gr := mylog.Check2(internal.NewGroupings(32))

	mylog.Check(gr.AddTo(&g, 1))

	mylog.Check(gr.AddTo(&g, 3))


	errBoom := errors.New("boom")
	n := 0
	f := func(group uint16) error {
		n++
		return errBoom
	}
	mylog.Check(gr.Iter(&g, f))
	c.Check(err, Equals, errBoom)
	c.Check(n, Equals, 1)
}

func (s *groupingsSuite) TestRepeated(c *C) {
	var g internal.Grouping

	gr := mylog.Check2(internal.NewGroupings(64))

	mylog.Check(gr.AddTo(&g, 1))

	mylog.Check(gr.AddTo(&g, 0))

	mylog.Check(gr.AddTo(&g, 2))

	mylog.Check(gr.AddTo(&g, 1))

	mylog.Check(gr.AddTo(&g, 0))


	elems := []uint16{}
	f := func(group uint16) error {
		elems = append(elems, group)
		return nil
	}
	mylog.Check(gr.Iter(&g, f))

	c.Check(elems, DeepEquals, []uint16{0, 1, 2})
}

func (s *groupingsSuite) TestCopy(c *C) {
	var g internal.Grouping

	gr := mylog.Check2(internal.NewGroupings(16))

	mylog.Check(gr.AddTo(&g, 1))

	mylog.Check(gr.AddTo(&g, 3))

	mylog.Check(gr.AddTo(&g, 0))

	mylog.Check(gr.AddTo(&g, 4))

	mylog.Check(gr.AddTo(&g, 2))


	g2 := g.Copy()
	c.Check(g2, DeepEquals, g)
	mylog.Check(gr.AddTo(&g2, 7))


	c.Check(gr.Contains(&g, 7), Equals, false)
	c.Check(gr.Contains(&g2, 7), Equals, true)

	c.Check(g2, Not(DeepEquals), g)
}

func (s *groupingsSuite) TestBitsetSerializeAndIterSimple(c *C) {
	gr := mylog.Check2(internal.NewGroupings(32))


	var elems []uint16
	f := func(group uint16) error {
		elems = append(elems, group)
		return nil
	}

	var g internal.Grouping
	mylog.Check(gr.AddTo(&g, 1))

	mylog.Check(gr.AddTo(&g, 5))

	mylog.Check(gr.AddTo(&g, 17))

	mylog.Check(gr.AddTo(&g, 24))


	l := gr.Serialize(&g)
	c.Check(l, DeepEquals,
		internal.Serialize([]uint16{
			4,
			uint16(1<<1 | 1<<5),
			uint16(1<<(17-16) | 1<<(24-16)),
		}))
	mylog.Check(gr.Iter(&g, f))

	c.Check(elems, DeepEquals, []uint16{1, 5, 17, 24})
}

func (s *groupingsSuite) TestBitSet(c *C) {
	var g internal.Grouping

	gr := mylog.Check2(internal.NewGroupings(64))


	for i := uint16(0); i < 64; i++ {
		mylog.Check(gr.AddTo(&g, i))

		c.Check(gr.Contains(&g, i), Equals, true)

		l := gr.Serialize(&g)

		switch i {
		case 4:
			c.Check(l, Equals, internal.Serialize([]uint16{5, 0x1f, 0, 0, 0}))
		case 15:
			c.Check(l, Equals, internal.Serialize([]uint16{16, 0xffff, 0, 0, 0}))
		case 16:
			c.Check(l, Equals, internal.Serialize([]uint16{17, 0xffff, 0x1, 0, 0}))
		case 63:
			c.Check(l, Equals, internal.Serialize([]uint16{64, 0xffff, 0xffff, 0xffff, 0xffff}))
		}

		g1 := mylog.Check2(gr.Deserialize(l))
		c.Check(err, IsNil)

		c.Check(g1, DeepEquals, &g)
	}

	for i := uint16(63); ; i-- {
		mylog.Check(gr.AddTo(&g, i))

		c.Check(gr.Contains(&g, i), Equals, true)
		if i == 0 {
			break
		}

		l := gr.Serialize(&g)

		g1 := mylog.Check2(gr.Deserialize(l))
		c.Check(err, IsNil)

		c.Check(g1, DeepEquals, &g)
	}
}

func (s *groupingsSuite) TestBitsetIter(c *C) {
	gr := mylog.Check2(internal.NewGroupings(32))


	var elems []uint16
	f := func(group uint16) error {
		elems = append(elems, group)
		return nil
	}

	for i := uint16(2); i < 32; i++ {
		var g internal.Grouping
		mylog.Check(gr.AddTo(&g, i-2))

		mylog.Check(gr.AddTo(&g, i-1))

		mylog.Check(gr.AddTo(&g, i))

		mylog.Check(gr.Iter(&g, f))

		c.Check(elems, DeepEquals, []uint16{i - 2, i - 1, i})

		elems = nil
	}

	var g internal.Grouping
	for i := uint16(0); i < 32; i++ {
		mylog.Check(gr.AddTo(&g, i))

	}
	mylog.Check(gr.Iter(&g, f))

	c.Check(elems, HasLen, 32)
}

func (s *groupingsSuite) TestBitsetIterError(c *C) {
	gr := mylog.Check2(internal.NewGroupings(16))


	var g internal.Grouping
	mylog.Check(gr.AddTo(&g, 0))

	mylog.Check(gr.AddTo(&g, 1))


	errBoom := errors.New("boom")
	n := 0
	f := func(group uint16) error {
		n++
		return errBoom
	}
	mylog.Check(gr.Iter(&g, f))
	c.Check(err, Equals, errBoom)
	c.Check(n, Equals, 1)
}

func BenchmarkIterBaseline(b *testing.B) {
	b.StopTimer()

	n := 0
	f := func(group uint16) error {
		n++
		return nil
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		n = 0
		for j := uint16(0); j < 64; j++ {
			f(j)
		}
		if n != 64 {
			b.FailNow()
		}
	}
}

func BenchmarkIter4Elems(b *testing.B) {
	b.StopTimer()

	gr := mylog.Check2(internal.NewGroupings(64))

	n := 0
	f := func(group uint16) error {
		n++
		return nil
	}

	var g internal.Grouping
	gr.AddTo(&g, 1)
	gr.AddTo(&g, 5)
	gr.AddTo(&g, 17)
	gr.AddTo(&g, 24)

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		n = 0
		gr.Iter(&g, f)
		if n != 4 {
			b.FailNow()
		}
	}
}

func BenchmarkIterBitset5Elems(b *testing.B) {
	b.StopTimer()

	gr := mylog.Check2(internal.NewGroupings(64))

	n := 0
	f := func(group uint16) error {
		n++
		return nil
	}

	var g internal.Grouping
	gr.AddTo(&g, 1)
	gr.AddTo(&g, 5)
	gr.AddTo(&g, 17)
	gr.AddTo(&g, 24)
	gr.AddTo(&g, 33)

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		n = 0
		gr.Iter(&g, f)
		if n != 5 {
			b.FailNow()
		}
	}
}

func BenchmarkIterBitsetEmptyStretches(b *testing.B) {
	b.StopTimer()

	gr := mylog.Check2(internal.NewGroupings(64))

	n := 0
	f := func(group uint16) error {
		n++
		return nil
	}

	var g internal.Grouping
	gr.AddTo(&g, 0)
	gr.AddTo(&g, 15)
	gr.AddTo(&g, 16)
	gr.AddTo(&g, 31)
	gr.AddTo(&g, 32)

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		n = 0
		gr.Iter(&g, f)
		if n != 5 {
			b.FailNow()
		}
	}
}

func BenchmarkIterBitsetEven(b *testing.B) {
	b.StopTimer()

	gr := mylog.Check2(internal.NewGroupings(64))

	n := 0
	f := func(group uint16) error {
		n++
		return nil
	}

	var g internal.Grouping
	for i := 0; i <= 63; i += 2 {
		gr.AddTo(&g, uint16(i))
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		n = 0
		gr.Iter(&g, f)
		if n != 32 {
			b.FailNow()
		}
	}
}

func BenchmarkIterBitsetOdd(b *testing.B) {
	b.StopTimer()

	gr := mylog.Check2(internal.NewGroupings(64))

	n := 0
	f := func(group uint16) error {
		n++
		return nil
	}

	var g internal.Grouping
	for i := 1; i <= 63; i += 2 {
		gr.AddTo(&g, uint16(i))
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		n = 0
		gr.Iter(&g, f)
		if n != 32 {
			b.FailNow()
		}
	}
}

func BenchmarkIterBitset0Inc3(b *testing.B) {
	b.StopTimer()

	gr := mylog.Check2(internal.NewGroupings(64))

	n := 0
	f := func(group uint16) error {
		n++
		return nil
	}

	var g internal.Grouping
	for i := 0; i <= 63; i += 3 {
		gr.AddTo(&g, uint16(i))
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		n = 0
		gr.Iter(&g, f)
		if n != 22 {
			b.FailNow()
		}
	}
}

func BenchmarkIterBitset1Inc3(b *testing.B) {
	b.StopTimer()

	gr := mylog.Check2(internal.NewGroupings(64))

	n := 0
	f := func(group uint16) error {
		n++
		return nil
	}

	var g internal.Grouping
	for i := 1; i <= 63; i += 3 {
		gr.AddTo(&g, uint16(i))
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		n = 0
		gr.Iter(&g, f)
		if n != 21 {
			b.FailNow()
		}
	}
}

func BenchmarkIterBitset0Inc4(b *testing.B) {
	b.StopTimer()

	gr := mylog.Check2(internal.NewGroupings(64))

	n := 0
	f := func(group uint16) error {
		n++
		return nil
	}

	var g internal.Grouping
	for i := 0; i <= 63; i += 4 {
		gr.AddTo(&g, uint16(i))
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		n = 0
		gr.Iter(&g, f)
		if n != 16 {
			b.FailNow()
		}
	}
}

func BenchmarkIterBitsetComplete(b *testing.B) {
	b.StopTimer()

	gr := mylog.Check2(internal.NewGroupings(64))

	n := 0
	f := func(group uint16) error {
		n++
		return nil
	}

	var g internal.Grouping
	for i := 0; i <= 63; i++ {
		gr.AddTo(&g, uint16(i))
	}

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		n = 0
		gr.Iter(&g, f)
		if n != 64 {
			b.FailNow()
		}
	}
}
