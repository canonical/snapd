// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package internal_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget/internal"
	"github.com/snapcore/snapd/testutil"
)

type udevSuite struct {
	testutil.BaseTest
}

var _ = Suite(&udevSuite{})

func (m *udevSuite) TestUdevTriggerHappy(c *C) {
	cmdUdevadm := testutil.MockCommand(c, "udevadm", "")
	defer cmdUdevadm.Restore()

	err := internal.UdevTrigger("/dev/node")
	c.Assert(err, IsNil)
	c.Assert(cmdUdevadm.Calls(), DeepEquals, [][]string{
		{"udevadm", "trigger", "--settle", "/dev/node"},
	})

}

func (m *udevSuite) TestUdevTriggerError(c *C) {
	cmdUdevadm := testutil.MockCommand(c, "udevadm", `echo "some error"; exit 1`)
	defer cmdUdevadm.Restore()

	err := internal.UdevTrigger("/dev/node")
	c.Assert(err, ErrorMatches, "some error")
	c.Assert(cmdUdevadm.Calls(), DeepEquals, [][]string{
		{"udevadm", "trigger", "--settle", "/dev/node"},
	})

}
