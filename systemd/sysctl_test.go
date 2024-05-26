// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"errors"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type sysctlSuite struct {
	testutil.BaseTest
}

var _ = Suite(&sysctlSuite{})

func (s *sysctlSuite) TestSysctl(c *C) {
	defer systemd.MockSystemdSysctlPath("systemd-sysctl")()

	systemdSysctl := testutil.MockCommand(c, "systemd-sysctl", "")
	defer systemdSysctl.Restore()
	mylog.Check(systemd.Sysctl(nil))

	mylog.Check(systemd.Sysctl([]string{"kernel.printk", "net"}))


	c.Check(systemdSysctl.Calls(), DeepEquals, [][]string{
		{"systemd-sysctl"},
		{"systemd-sysctl", "--prefix", "kernel.printk", "--prefix", "net"},
	})
}

func (s *sysctlSuite) TestSysctlError(c *C) {
	defer systemd.MockSystemdSysctlPath("systemd-sysctl")()

	systemdSysctl := testutil.MockCommand(c, "systemd-sysctl", `
echo foo >&2
exit 1
`)
	defer systemdSysctl.Restore()
	mylog.Check(systemd.Sysctl(nil))
	c.Assert(err, ErrorMatches, `(?m)systemd-sysctl invoked with \[\] failed with exit status 1: foo`)
	mylog.Check(systemd.Sysctl([]string{"net"}))
	c.Assert(err, ErrorMatches, `(?m)systemd-sysctl invoked with \[--prefix net\] failed with exit status 1: foo`)
}

func (s *sysctlSuite) TestSysctlFailedExec(c *C) {
	defer systemd.MockSystemdSysctlPath("/i/bet/this/does/not/exist/systemd-sysctl")()
	mylog.Check(systemd.Sysctl(nil))
	c.Assert(err, ErrorMatches, `fork/exec /i/bet/this/does/not/exist/systemd-sysctl: no such file or directory`)
}

func (s *sysctlSuite) TestMockSystemdSysctl(c *C) {
	var capturedArgs []string
	var sysctlErr error
	r := systemd.MockSystemdSysctl(func(args ...string) error {
		capturedArgs = args
		return sysctlErr
	})
	defer r()
	mylog.Check(systemd.Sysctl([]string{"kernel.printk", "net"}))


	c.Check(capturedArgs, DeepEquals, []string{"--prefix", "kernel.printk", "--prefix", "net"})

	sysctlErr = errors.New("boom")
	mylog.Check(systemd.Sysctl(nil))
	c.Check(err, Equals, sysctlErr)
	c.Check(capturedArgs, HasLen, 0)
}
