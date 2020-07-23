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

package osutil_test

import (
	"io"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type sizerTestSuite struct{}

var _ = Suite(&sizerTestSuite{})

func (s *sizerTestSuite) TestSizer(c *C) {
	sz := &osutil.Sizer{}
	c.Check(sz.Size(), Equals, int64(0))

	io.WriteString(sz, "12345")
	c.Check(sz.Size(), Equals, int64(5))
	io.WriteString(sz, "12345")
	c.Check(sz.Size(), Equals, int64(10))

	sz.Reset()
	c.Check(sz.Size(), Equals, int64(0))
	io.WriteString(sz, "12345")
	c.Check(sz.Size(), Equals, int64(5))
}
