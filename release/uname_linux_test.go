// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/release"
)

func (s *ReleaseTestSuite) TestKernelVersion(c *C) {
	ver := release.KernelVersion()
	// Ensure that we got something.
	c.Check(ver, Not(Equals), "")
}

func (s *ReleaseTestSuite) TestGetKernelRelease(c *C) {
	var buf syscall.Utsname
	c.Check(release.GetKernelRelease(&buf), Equals, "")

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

	c.Check(release.GetKernelRelease(&buf), Equals, "foo")
}

func (s *ReleaseTestSuite) TestGetKernelMachine(c *C) {
	var buf syscall.Utsname
	c.Check(release.GetMachineName(&buf), Equals, "")

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

	c.Check(release.GetMachineName(&buf), Equals, "armv7a")
}
