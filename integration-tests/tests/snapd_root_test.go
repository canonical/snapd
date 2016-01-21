// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015 Canonical Ltd
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

package tests

import "gopkg.in/check.v1"

var _ = check.Suite(&snapdRootTestSuite{})

type snapdRootTestSuite struct {
	snapdTestSuite
}

func (s *snapdRootTestSuite) TestResource(c *check.C) {
	exerciseAPI(c, s)
}

func (s *snapdRootTestSuite) resource() string {
	return baseURL + "/"
}

func (s *snapdRootTestSuite) getInteractions() apiInteractions {
	return []apiInteraction{{
		responsePattern: `.*`}} // XXX: decide what to return for /
}
