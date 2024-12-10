// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil"
)

type joinErrorsSuite struct{}

var _ = Suite(&joinErrorsSuite{})

func (s *joinErrorsSuite) TestJoin(c *C) {
	errs := []error{
		errors.New("foo"),
		errors.New("bar"),
		errors.New("baz"),
	}
	for _, testCase := range []struct {
		errors  []error
		wrapped error
		errStr  string
	}{
		{
			errs,
			errs[0],
			"foo\nbar\nbaz",
		},
		{
			[]error{nil, errs[2], nil, errs[1], nil},
			errs[2],
			"baz\nbar",
		},
		{
			[]error{nil, nil, nil},
			nil,
			"",
		},
	} {
		joined := strutil.JoinErrors(testCase.errors...)
		c.Check(errors.Is(joined, testCase.wrapped), Equals, true, Commentf("testCase: %+v", testCase))
		if testCase.errStr != "" {
			c.Check(joined, ErrorMatches, testCase.errStr)
		} else {
			c.Check(joined, IsNil)
		}
	}
}
