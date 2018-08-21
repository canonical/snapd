// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2018 Canonical Ltd
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

package osutil_test

import (
	"bytes"
	"errors"
	"os/exec"
	"syscall"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type unameSuite struct{}

var _ = check.Suite(unameSuite{})

func ucmd1(c *check.C, arg string) string {
	out, err := exec.Command("uname", arg).CombinedOutput()
	c.Assert(err, check.IsNil)
	return string(bytes.TrimSpace(out))
}

func (unameSuite) TestUname(c *check.C) {
	c.Check(osutil.KernelVersion(), check.Equals, ucmd1(c, "-r"))
	c.Check(osutil.MachineName(), check.Equals, ucmd1(c, "-m"))
}

func (unameSuite) TestUnameErrorMeansUnknown(c *check.C) {
	restore := osutil.MockUname(func(buf *syscall.Utsname) error {
		return errors.New("bzzzt")
	})
	defer restore()

	c.Check(osutil.KernelVersion(), check.Equals, "unknown")
	c.Check(osutil.MachineName(), check.Equals, "unknown")
}

func (unameSuite) TestKernelVersionStopsAtZeroes(c *check.C) {
	restore := osutil.MockUname(func(buf *syscall.Utsname) error {
		buf.Release[0] = 'f'
		buf.Release[1] = 'o'
		buf.Release[2] = 'o'
		buf.Release[3] = 0
		buf.Release[4] = 'u'
		buf.Release[5] = 'n'
		buf.Release[6] = 'u'
		buf.Release[7] = 's'
		buf.Release[8] = 'e'
		buf.Release[9] = 'd'
		return nil
	})
	defer restore()

	c.Check(osutil.KernelVersion(), check.Equals, "foo")
}

func (unameSuite) TestMachineNameStopsAtZeroes(c *check.C) {
	restore := osutil.MockUname(func(buf *syscall.Utsname) error {
		buf.Machine[0] = 'a'
		buf.Machine[1] = 'r'
		buf.Machine[2] = 'm'
		buf.Machine[3] = 'v'
		buf.Machine[4] = '7'
		buf.Machine[5] = 'a'
		buf.Machine[6] = 0
		buf.Machine[7] = 'u'
		buf.Machine[8] = 'n'
		buf.Machine[9] = 'u'
		buf.Machine[10] = 's'
		buf.Machine[11] = 'e'
		buf.Machine[12] = 'd'
		return nil
	})
	defer restore()
	c.Check(osutil.MachineName(), check.Equals, "armv7a")
}
