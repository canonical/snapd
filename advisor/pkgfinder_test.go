// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nobolt

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

package advisor_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/advisor"
)

func (s *cmdfinderSuite) TestFindPackageHit(c *C) {
	pkg, err := advisor.FindPackage("foo")
	c.Assert(err, IsNil)
	c.Check(pkg, DeepEquals, &advisor.Package{
		Snap: "foo", Version: "1.0", Summary: "foo summary",
	})
}

func (s *cmdfinderSuite) TestFindPackageMiss(c *C) {
	pkg, err := advisor.FindPackage("moh")
	c.Assert(err, IsNil)
	c.Check(pkg, IsNil)
}
