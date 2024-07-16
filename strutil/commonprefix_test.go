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

type commonPrefixSuite struct{}

var _ = Suite(&commonPrefixSuite{})

func (s *commonPrefixSuite) TestCommonPrefix(c *C) {
	tt := []struct {
		patterns     []string
		commonPrefix string
		err          string
	}{
		{
			[]string{},
			"",
			"no patterns provided",
		},
		{
			[]string{
				"/one/single/pattern",
			},
			"/one/single/pattern",
			"",
		},
		{
			[]string{
				"/pattern/n/one",
				"/pattern/n/two",
			},
			"/pattern/n/",
			"",
		},
		{
			[]string{
				"/one/",
				"/one/two/",
			},
			"/one/",
			"",
		},
		{
			[]string{
				"$ONE",
				"/one/two/",
			},
			"",
			"",
		},
	}

	for _, t := range tt {
		commonPrefix, err := strutil.FindCommonPrefix(t.patterns)
		c.Assert(commonPrefix, Equals, t.commonPrefix)
		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err)
		}
	}
}
