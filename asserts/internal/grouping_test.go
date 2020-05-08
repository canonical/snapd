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
		gr, err := internal.NewGroupings(t.n)
		if t.err == "" {
			c.Check(err, IsNil, comm)
			c.Check(gr, NotNil, comm)
		} else {
			c.Check(gr, IsNil, comm)
			c.Check(err, ErrorMatches, t.err, comm)
		}
	}
}

func (s *groupingsSuite) TestAddToAndContains(c *C) {
	var g internal.Grouping

	gr, err := internal.NewGroupings(16)
	c.Assert(err, IsNil)

	err = gr.AddTo(&g, 1)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 3)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 0)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 4)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 2)
	c.Assert(err, IsNil)

	for i := uint16(0); i < 5; i++ {
		c.Check(gr.Contains(&g, i), Equals, true)
	}

	c.Check(gr.Contains(&g, 5), Equals, false)
}

func (s *groupingsSuite) TestOutsideRange(c *C) {
	var g internal.Grouping

	gr, err := internal.NewGroupings(16)
	c.Assert(err, IsNil)

	// sanity
	err = gr.AddTo(&g, 15)
	c.Assert(err, IsNil)

	err = gr.AddTo(&g, 16)
	c.Check(err, ErrorMatches, "group exceeds admissible maximum: 16 >= 16")

	err = gr.AddTo(&g, 99)
	c.Check(err, ErrorMatches, "group exceeds admissible maximum: 99 >= 16")
}

func (s *groupingsSuite) TestLabel(c *C) {
	var g internal.Grouping

	gr, err := internal.NewGroupings(16)
	c.Assert(err, IsNil)

	err = gr.AddTo(&g, 1)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 3)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 0)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 4)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 2)
	c.Assert(err, IsNil)

	l := gr.Label(&g)

	g1, err := gr.Parse(l)
	c.Check(err, IsNil)

	c.Check(g1, DeepEquals, &g)
}

func (s *groupingsSuite) TestLabelParseErrors(c *C) {
	var g internal.Grouping

	gr, err := internal.NewGroupings(16)
	c.Assert(err, IsNil)

	err = gr.AddTo(&g, 0)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 1)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 2)
	c.Assert(err, IsNil)

	invalidLabels := []string{
		// not base64
		"\x0a\x02\xf4",
		// wrong length
		base64.RawURLEncoding.EncodeToString([]byte{1}),
		// not a known group
		internal.MakeLabel([]uint16{3}),
		// not in order
		internal.MakeLabel([]uint16{0, 2, 1}),
	}

	for _, il := range invalidLabels {
		_, err := gr.Parse(il)
		c.Check(err, ErrorMatches, "invalid grouping label")
	}
}

func (s *groupingsSuite) TestIter(c *C) {
	var g internal.Grouping

	gr, err := internal.NewGroupings(16)
	c.Assert(err, IsNil)

	err = gr.AddTo(&g, 1)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 3)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 0)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 4)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 2)
	c.Assert(err, IsNil)

	elems := []uint16{}
	f := func(group uint16) error {
		elems = append(elems, group)
		return nil
	}

	err = gr.Iter(&g, f)
	c.Assert(err, IsNil)
	c.Check(elems, DeepEquals, []uint16{0, 1, 2, 3, 4})
}

func (s *groupingsSuite) TestIterError(c *C) {
	var g internal.Grouping

	gr, err := internal.NewGroupings(16)
	c.Assert(err, IsNil)

	err = gr.AddTo(&g, 1)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 3)
	c.Assert(err, IsNil)

	errBoom := errors.New("boom")
	n := 0
	f := func(group uint16) error {
		n++
		return errBoom
	}

	err = gr.Iter(&g, f)
	c.Check(err, Equals, errBoom)
	c.Check(n, Equals, 1)
}

func (s *groupingsSuite) TestRepeated(c *C) {
	var g internal.Grouping

	gr, err := internal.NewGroupings(16)
	c.Assert(err, IsNil)

	err = gr.AddTo(&g, 1)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 0)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 2)
	c.Assert(err, IsNil)

	err = gr.AddTo(&g, 1)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 0)
	c.Assert(err, IsNil)

	elems := []uint16{}
	f := func(group uint16) error {
		elems = append(elems, group)
		return nil
	}

	err = gr.Iter(&g, f)
	c.Assert(err, IsNil)
	c.Check(elems, DeepEquals, []uint16{0, 1, 2})
}

func (s *groupingsSuite) TestCopy(c *C) {
	var g internal.Grouping

	gr, err := internal.NewGroupings(16)
	c.Assert(err, IsNil)

	err = gr.AddTo(&g, 1)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 3)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 0)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 4)
	c.Assert(err, IsNil)
	err = gr.AddTo(&g, 2)
	c.Assert(err, IsNil)

	g2 := g.Copy()
	c.Check(g2, DeepEquals, g)

	err = gr.AddTo(&g2, 7)
	c.Assert(err, IsNil)

	c.Check(gr.Contains(&g, 7), Equals, false)
	c.Check(gr.Contains(&g2, 7), Equals, true)

	c.Check(g2, Not(DeepEquals), g)
}
