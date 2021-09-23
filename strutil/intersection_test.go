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

package strutil_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil"
)

type intersectionSuite struct{}

var _ = Suite(&intersectionSuite{})

func (s *intersectionSuite) TestIntersection(c *C) {
	tt := []struct {
		ins [][]string
		out []string
	}{
		{
			[][]string{
				{},
			},
			[]string{},
		},
		{
			[][]string{
				{"1"},
			},
			[]string{"1"},
		},
		{
			[][]string{
				{"1", "2"},
			},
			[]string{"1", "2"},
		},
		{
			[][]string{
				{"1", "2"},
				{"1"},
			},
			[]string{"1"},
		},
		{
			[][]string{
				{"1", "2"},
				{"1"},
				{"1"},
			},
			[]string{"1"},
		},
		{
			[][]string{
				{"1", "2"},
				{"1"},
				{"1", "3"},
			},
			[]string{"1"},
		},
		{
			[][]string{
				{"1", "2"},
				{"1"},
				{"1", "2", "3"},
			},
			[]string{"1"},
		},
		{
			[][]string{
				{"1", "2"},
				{"1", "2"},
				{"1", "2", "3"},
			},
			[]string{"1", "2"},
		},
		{
			[][]string{
				{"1", "1", "1", "1", "1"},
				{"1", "1", "1", "1", "1"},
				{"1", "1", "1", "1", "1"},
			},
			[]string{"1"},
		},
		{
			[][]string{
				{"1", "1", "1", "1", "1", "2"},
				{"1", "1", "1", "1", "1", "2", "2"},
				{"1", "1", "1", "1", "1", "2", "2"},
			},
			[]string{"1", "2"},
		},
	}

	for _, t := range tt {
		res := strutil.Intersection(t.ins...)
		c.Assert(res, DeepEquals, t.out)
	}
}
