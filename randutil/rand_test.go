// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

package randutil_test

import (
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/randutil"
)

func Test(t *testing.T) { TestingT(t) }

type randutilSuite struct{}

var _ = Suite(&randutilSuite{})

func fixedSeedOne() int64 { return 1 }

func (s *randutilSuite) TestReseed(c *C) {
	// predictable starting point
	r := randutil.NewPseudoRand(fixedSeedOne)
	s1 := r.RandomString(100)
	r.Reseed(1)
	s2 := r.RandomString(100)
	c.Check(s1, Equals, s2)
	r.Reseed(1)
	d1 := r.RandomDuration(100)
	r.Reseed(1)
	d2 := r.RandomDuration(100)
	c.Check(d1, Equals, d2)
}

func (s *randutilSuite) TestRandomString(c *C) {
	// predictable starting point
	r := randutil.NewPseudoRand(fixedSeedOne)

	for _, v := range []struct {
		length int
		result string
	}{
		{10, "pw7MpXh0JB"},
		{5, "4PQyl"},
		{0, ""},
		{-1000, ""},
	} {
		c.Assert(r.RandomString(v.length), Equals, v.result)
	}
}

func (s *randutilSuite) TestRandomDuration(c *C) {
	// predictable starting point
	r := randutil.NewPseudoRand(fixedSeedOne)

	for _, v := range []struct {
		duration time.Duration
		result   time.Duration
	}{
		{time.Hour, 1991947779410},
		{4 * time.Hour, 4423082153551},
		{0, 0},
		{-4 * time.Hour, 0},
	} {
		res := r.RandomDuration(v.duration)
		// Automatic bounds verification for positive ranges
		// because this is difficult to infer by simply looking
		// at the nano-second totals.
		if v.duration > 0 {
			c.Check(res < v.duration, Equals, true)
		}
		c.Assert(res, Equals, v.result)
	}
}

func (s *randutilSuite) TestRandomDurationWithSeedDatePidHostMac(c *C) {
	r := randutil.NewPseudoRand(randutil.SeedDatePidHostMac)
	d := r.RandomDuration(time.Hour)
	c.Check(d < time.Hour, Equals, true)
}
