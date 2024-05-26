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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/release"
)

func (s *SnapSuite) TestDebugBootvars(c *check.C) {
	restore := release.MockOnClassic(false)
	defer restore()
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	mylog.Check(bloader.SetBootVars(map[string]string{
		"snap_mode":       "try",
		"unrelated":       "thing",
		"snap_core":       "core18_1.snap",
		"snap_try_core":   "core18_2.snap",
		"snap_kernel":     "pc-kernel_3.snap",
		"snap_try_kernel": "pc-kernel_4.snap",
	}))
	c.Assert(err, check.IsNil)

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"debug", "boot-vars"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, `snap_mode=try
snap_core=core18_1.snap
snap_try_core=core18_2.snap
snap_kernel=pc-kernel_3.snap
snap_try_kernel=pc-kernel_4.snap
`)
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *SnapSuite) TestDebugBootvarsNotOnClassic(c *check.C) {
	restore := release.MockOnClassic(true)
	defer restore()
	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"debug", "boot-vars"}))
	c.Assert(err, check.ErrorMatches, `the "boot-vars" command is not available on classic systems`)
}

func (s *SnapSuite) TestDebugSetBootvars(c *check.C) {
	restore := release.MockOnClassic(false)
	defer restore()
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	mylog.Check(bloader.SetBootVars(map[string]string{
		"snap_mode":       "try",
		"unrelated":       "thing",
		"snap_core":       "core18_1.snap",
		"snap_try_core":   "core18_2.snap",
		"snap_kernel":     "pc-kernel_3.snap",
		"snap_try_kernel": "",
	}))
	c.Assert(err, check.IsNil)

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{
		"debug", "set-boot-vars",
		"snap_mode=trying", "try_recovery_system=1234", "unrelated=",
	}))
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(bloader.BootVars, check.DeepEquals, map[string]string{
		"snap_mode":           "trying",
		"unrelated":           "",
		"snap_core":           "core18_1.snap",
		"snap_try_core":       "core18_2.snap",
		"snap_kernel":         "pc-kernel_3.snap",
		"snap_try_kernel":     "",
		"try_recovery_system": "1234",
	})
}

func (s *SnapSuite) TestDebugGetSetBootvarsWithParams(c *check.C) {
	// the bootloader options are not intercepted by the mocks, so we can
	// only observe the effect indirectly for boot-vars

	restore := release.MockOnClassic(false)
	defer restore()
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	mylog.Check(bloader.SetBootVars(map[string]string{
		"snapd_recovery_system":       "1234",
		"snapd_recovery_mode":         "run",
		"unrelated":                   "thing",
		"snap_kernel":                 "pc-kernel_3.snap",
		"recovery_system_status":      "try",
		"try_recovery_system":         "9999",
		"snapd_good_recovery_systems": "0000",
	}))
	c.Assert(err, check.IsNil)

	rest := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"debug", "boot-vars", "--root-dir", boot.InitramfsUbuntuBootDir}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)
	c.Check(s.Stdout(), check.Equals, `snapd_recovery_mode=run
snapd_recovery_system=1234
snapd_recovery_kernel=
snap_kernel=pc-kernel_3.snap
snap_try_kernel=
kernel_status=
recovery_system_status=try
try_recovery_system=9999
snapd_good_recovery_systems=0000
snapd_extra_cmdline_args=
snapd_full_cmdline_args=
`)
	c.Check(s.Stderr(), check.Equals, "")
	s.ResetStdStreams()

	// and make sure that set does not blow up when passed a root dir
	rest = mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"debug", "set-boot-vars", "--root-dir", boot.InitramfsUbuntuBootDir, "foo=bar"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)

	v := mylog.Check2(bloader.GetBootVars("foo"))
	c.Assert(err, check.IsNil)
	c.Check(v, check.DeepEquals, map[string]string{
		"foo": "bar",
	})
	// and make sure that set does not blow up when passed recover bootloader flag
	rest = mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"debug", "set-boot-vars", "--recovery", "foo=recovery"}))
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.HasLen, 0)

	v = mylog.Check2(bloader.GetBootVars("foo"))
	c.Assert(err, check.IsNil)
	c.Check(v, check.DeepEquals, map[string]string{
		"foo": "recovery",
	})

	// but basic validity checks are still done
	_ = mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"debug", "set-boot-vars", "--recovery", "--root-dir", boot.InitramfsUbuntuBootDir, "foo=recovery"}))
	c.Assert(err, check.ErrorMatches, "cannot use run bootloader root-dir with a recovery flag")
}
