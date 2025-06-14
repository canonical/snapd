// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package systemd_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type detectSuite struct{}

var _ = Suite(&detectSuite{})

func (*detectSuite) TestIsContainer_Yes(c *C) {
	systemdCmd := testutil.MockCommand(c, "systemd-detect-virt", `exit 0`)
	defer systemdCmd.Restore()

	c.Check(systemd.IsContainer(), Equals, true)
}

func (*detectSuite) TestIsContainer_No(c *C) {
	systemdCmd := testutil.MockCommand(c, "systemd-detect-virt", `exit 1`)
	defer systemdCmd.Restore()

	c.Check(systemd.IsContainer(), Equals, false)
}

func (*detectSuite) TestIsVirtualMachine_Yes(c *C) {
	systemdCmd := testutil.MockCommand(c, "systemd-detect-virt", `exit 0`)
	defer systemdCmd.Restore()

	c.Check(systemd.IsVirtualMachine(), Equals, true)
}

func (*detectSuite) TestIsVirtualMachine_No(c *C) {
	systemdCmd := testutil.MockCommand(c, "systemd-detect-virt", `exit 1`)
	defer systemdCmd.Restore()

	c.Check(systemd.IsVirtualMachine(), Equals, false)
}
