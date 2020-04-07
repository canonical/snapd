// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type mountSuite struct {
	testutil.BaseTest
}

var _ = Suite(&mountSuite{})

func (s *mountSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *mountSuite) TestIsMountedHappyish(c *C) {
	// note the different optional fields
	const content = "" +
		"44 24 7:1 / /snap/ubuntu-core/855 rw,relatime shared:27 - squashfs /dev/loop1 ro\n" +
		"44 24 7:1 / /snap/something/123 rw,relatime - squashfs /dev/loop2 ro\n" +
		"44 24 7:1 / /snap/random/456 rw,relatime opt:1 shared:27 - squashfs /dev/loop1 ro\n"
	c.Assert(osutil.MockProcSelfMountInfo(dirs.ProcSelfMountInfo, content), IsNil)

	mounted, err := osutil.IsMounted(dirs.ProcSelfMountInfo, "/snap/ubuntu-core/855")
	c.Check(err, IsNil)
	c.Check(mounted, Equals, true)

	mounted, err = osutil.IsMounted(dirs.ProcSelfMountInfo, "/snap/something/123")
	c.Check(err, IsNil)
	c.Check(mounted, Equals, true)

	mounted, err = osutil.IsMounted(dirs.ProcSelfMountInfo, "/snap/random/456")
	c.Check(err, IsNil)
	c.Check(mounted, Equals, true)

	mounted, err = osutil.IsMounted(dirs.ProcSelfMountInfo, "/random/made/up/name")
	c.Check(err, IsNil)
	c.Check(mounted, Equals, false)
}

func (s *mountSuite) TestIsMountedBroken(c *C) {
	c.Assert(osutil.MockProcSelfMountInfo(dirs.ProcSelfMountInfo, "44 24 7:1 ...truncated-stuff"), IsNil)

	mounted, err := osutil.IsMounted(dirs.ProcSelfMountInfo, "/snap/ubuntu-core/855")
	c.Check(err, ErrorMatches, "incorrect number of fields, .*")
	c.Check(mounted, Equals, false)
}
