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

package hardwarestate_test

import (
	"github.com/snapcore/snapd/overlord/hardwarestate"
	"testing"

	. "gopkg.in/check.v1"
)

func TestUDevMonitor(t *testing.T) { TestingT(t) }

type udevMonitorSuite struct{}

var _ = Suite(&udevMonitorSuite{})

func (s *udevMonitorSuite) TestSmoke(c *C) {
	for i := 0; i < 3; i++ {
		mon := hardwarestate.NewUDevMonitor()
		c.Assert(mon, NotNil)
		c.Assert(mon.Run(), IsNil)
		c.Assert(mon.Stop(), IsNil)
	}
}
