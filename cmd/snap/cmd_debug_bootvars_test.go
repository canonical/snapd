// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package main_test

import (
	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestDebugBootvars(c *check.C) {
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	bloader.BootVars = map[string]string{
		"snap_mode":       "try",
		"unrelated":       "thing",
		"snap_core":       "core18_1.snap",
		"snap_try_core":   "core18_2.snap",
		"snap_kernel":     "pc-kernel_3.snap",
		"snap_try_kernel": "pc-kernel_4.snap",
	}

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "bootvars"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Equals, `snap_mode=try
snap_core=core18_1.snap
snap_try_core=core18_2.snap
snap_kernel=pc-kernel_3.snap
snap_try_kernel=pc-kernel_4.snap
`)
	c.Check(s.Stderr(), check.Equals, "")
}
