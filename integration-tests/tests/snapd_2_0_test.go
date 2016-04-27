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

var _ = check.Suite(&snapd20TestSuite{})

type snapd20TestSuite struct {
	snapdTestSuite
}

func (s *snapd20TestSuite) TestResource(c *check.C) {
	exerciseAPI(c, s)
}

func (s *snapd20TestSuite) resource() string {
	return baseURL + "/v2/system-info"
}

func (s *snapd20TestSuite) getInteractions() apiInteractions {
	return []apiInteraction{{
		responsePattern: `(?U){"type":"sync","status-code":200,"status":"OK","result":{"flavor":"core","series":"\d+"}}`}}
}
