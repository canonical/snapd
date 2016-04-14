// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package common

import "gopkg.in/check.v1"

type infoTestSuite struct {
}

var _ = check.Suite(&infoTestSuite{})

func (s *infoTestSuite) TestRelease(c *check.C) {
	snappyCmd = "/bin/true"
	c.Assert(Release(c), check.Equals, "15.04")

	snappyCmd = "/non/existing/really"
	c.Assert(Release(c), check.Equals, "rolling")
}
