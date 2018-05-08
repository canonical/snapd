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

package release_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/release"
)

type seccompSuite struct{}

var _ = Suite(&seccompSuite{})

func (s *seccompSuite) TestInterfaceSystemKey(c *C) {
	reset := release.MockSecCompActions([]string{})
	defer reset()
	c.Check(release.SecCompActions(), DeepEquals, []string{})

	reset = release.MockSecCompActions([]string{"allow", "errno", "kill", "log", "trace", "trap"})
	defer reset()
	c.Check(release.SecCompActions(), DeepEquals, []string{"allow", "errno", "kill", "log", "trace", "trap"})
}

func (s *seccompSuite) TestSecCompSupportsAction(c *C) {
	reset := release.MockSecCompActions([]string{})
	defer reset()
	c.Check(release.SecCompSupportsAction("log"), Equals, false)

	reset = release.MockSecCompActions([]string{"allow", "errno", "kill", "log", "trace", "trap"})
	defer reset()
	c.Check(release.SecCompSupportsAction("log"), Equals, true)
}
