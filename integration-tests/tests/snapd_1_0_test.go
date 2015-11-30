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

var _ = check.Suite(&snapd10TestSuite{})

type snapd10TestSuite struct {
	snapdTestSuite
}

func (s *snapd10TestSuite) TestResource(c *check.C) {
	exerciseAPI(c, s)
}

func (s *snapd10TestSuite) resource() string {
	return baseURL + "/1.0"
}

func (s *snapd10TestSuite) getInteractions() apiInteractions {
	return []apiInteraction{{
		responsePattern: `(?U){"result":{"api_compat":"\d+","default_channel":".*","flavor":".*","release":".*"},"status":"OK","status_code":200,"type":"sync"}`}}
}
