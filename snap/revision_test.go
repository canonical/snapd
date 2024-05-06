// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snap_test

import (
	"encoding/json"
	"strconv"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/snap"
)

// Keep this in sync between snap and client packages.

type revisionSuite struct{}

var _ = Suite(&revisionSuite{})

func (s revisionSuite) TestString(c *C) {
	c.Assert(snap.R(0).String(), Equals, "unset")
	c.Assert(snap.R(10).String(), Equals, "10")
	c.Assert(snap.R(-9).String(), Equals, "x9")
}

func (s revisionSuite) TestUnset(c *C) {
	c.Assert(snap.R(0).Unset(), Equals, true)
	c.Assert(snap.R(10).Unset(), Equals, false)
	c.Assert(snap.R(-9).Unset(), Equals, false)
}

func (s revisionSuite) TestSet(c *C) {
	c.Assert(snap.R(0).Set(), Equals, false)
	c.Assert(snap.R(10).Set(), Equals, true)
	c.Assert(snap.R(-9).Set(), Equals, true)
}

func (s revisionSuite) TestLocal(c *C) {
	c.Assert(snap.R(0).Local(), Equals, false)
	c.Assert(snap.R(10).Local(), Equals, false)
	c.Assert(snap.R(-9).Local(), Equals, true)
}

func (s revisionSuite) TestStore(c *C) {
	c.Assert(snap.R(0).Store(), Equals, false)
	c.Assert(snap.R(10).Store(), Equals, true)
	c.Assert(snap.R(-9).Store(), Equals, false)
}

func (s revisionSuite) TestJSON(c *C) {
	for _, n := range []int{0, 10, -9} {
		r := snap.R(n)
		data, err := json.Marshal(snap.R(n))
		c.Assert(err, IsNil)
		c.Assert(string(data), Equals, `"`+r.String()+`"`)

		var got snap.Revision
		err = json.Unmarshal(data, &got)
		c.Assert(err, IsNil)
		c.Assert(got, Equals, r)

		got = snap.Revision{}
		err = json.Unmarshal([]byte(strconv.Itoa(r.N)), &got)
		c.Assert(err, IsNil)
		c.Assert(got, Equals, r)
	}
}

func (s revisionSuite) TestYAML(c *C) {
	for _, v := range []struct {
		n int
		s string
	}{
		{0, "unset"},
		{10, `"10"`},
		{-9, "x9"},
	} {
		r := snap.R(v.n)
		data, err := yaml.Marshal(snap.R(v.n))
		c.Assert(err, IsNil)
		c.Assert(string(data), Equals, v.s+"\n")

		var got snap.Revision
		err = yaml.Unmarshal(data, &got)
		c.Assert(err, IsNil)
		c.Assert(got, Equals, r)

		got = snap.Revision{}
		err = json.Unmarshal([]byte(strconv.Itoa(r.N)), &got)
		c.Assert(err, IsNil)
		c.Assert(got, Equals, r)
	}
}

func (s revisionSuite) ParseRevision(c *C) {
	type testItem struct {
		s string
		n int
		e string
	}

	var tests = []testItem{{
		s: "unset",
		n: 0,
	}, {
		s: "x1",
		n: -1,
	}, {
		s: "1",
		n: 1,
	}, {
		s: "x-1",
		e: `invalid snap revision: "x-1"`,
	}, {
		s: "x0",
		e: `invalid snap revision: "x0"`,
	}, {
		s: "-1",
		e: `invalid snap revision: "-1"`,
	}, {
		s: "0",
		e: `invalid snap revision: "0"`,
	}}

	for _, test := range tests {
		r, err := snap.ParseRevision(test.s)
		if test.e != "" {
			c.Assert(err.Error(), Equals, test.e)
			continue
		}
		c.Assert(r, Equals, snap.R(test.n))
	}
}

func (s *revisionSuite) TestR(c *C) {
	type testItem struct {
		v interface{}
		n int
		e string
	}

	var tests = []testItem{{
		v: 0,
		n: 0,
	}, {
		v: -1,
		n: -1,
	}, {
		v: 1,
		n: 1,
	}, {
		v: "unset",
		n: 0,
	}, {
		v: "x1",
		n: -1,
	}, {
		v: "1",
		n: 1,
	}, {
		v: "x-1",
		e: `invalid snap revision: "x-1"`,
	}, {
		v: "x0",
		e: `invalid snap revision: "x0"`,
	}, {
		v: "-1",
		e: `invalid snap revision: "-1"`,
	}, {
		v: "0",
		e: `invalid snap revision: "0"`,
	}, {
		v: int64(1),
		e: `cannot use 1 \(int64\) as a snap revision`,
	}}

	for _, test := range tests {
		if test.e != "" {
			f := func() { snap.R(test.v) }
			c.Assert(f, PanicMatches, test.e)
			continue
		}

		c.Assert(snap.R(test.v), Equals, snap.R(test.n))
	}
}
