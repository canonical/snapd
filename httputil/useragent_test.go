// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package httputil_test

import (
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/httputil"
)

type UASuite struct {
	restore func()
}

var _ = Suite(&UASuite{})

func (s *UASuite) SetUpTest(c *C) {
	s.restore = httputil.MockUserAgent("-")
}

func (s *UASuite) TearDownTest(c *C) {
	s.restore()
}

func (s *UASuite) TestUserAgent(c *C) {
	httputil.SetUserAgentFromVersion("10")
	ua := httputil.UserAgent()
	c.Check(strings.HasPrefix(ua, "snapd/10 "), Equals, true)

	httputil.SetUserAgentFromVersion("10", "extraProd")
	ua = httputil.UserAgent()
	c.Check(strings.Contains(ua, "extraProd"), Equals, true)
}
