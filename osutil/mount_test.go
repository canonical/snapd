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
	"io/ioutil"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type mountSuite struct{}

var _ = Suite(&mountSuite{})

func (s *mountSuite) TestIsMountedHappyish(c *C) {
	mockMountInfoFn := filepath.Join(c.MkDir(), "mountinfo")
	restore := osutil.MockMountInfoPath(mockMountInfoFn)
	defer restore()

	// note the different optinal fields
	content := []byte(`
44 24 7:1 / /snap/ubuntu-core/855 rw,relatime shared:27 - squashfs /dev/loop1 ro
44 24 7:1 / /snap/something/123 rw,relatime - squashfs /dev/loop2 ro
44 24 7:1 / /snap/random/456 rw,relatime opt:1 shared:27 - squashfs /dev/loop1 ro
`)
	err := ioutil.WriteFile(mockMountInfoFn, content, 0644)
	c.Assert(err, IsNil)

	mounted, err := osutil.IsMounted("/snap/ubuntu-core/855")
	c.Check(err, IsNil)
	c.Check(mounted, Equals, true)

	mounted, err = osutil.IsMounted("/snap/something/123")
	c.Check(err, IsNil)
	c.Check(mounted, Equals, true)

	mounted, err = osutil.IsMounted("/snap/random/456")
	c.Check(err, IsNil)
	c.Check(mounted, Equals, true)

	mounted, err = osutil.IsMounted("/random/made/up/name")
	c.Check(err, IsNil)
	c.Check(mounted, Equals, false)
}

func (s *mountSuite) TestIsMountedNotThereErr(c *C) {
	restore := osutil.MockMountInfoPath("/no/such/file")
	defer restore()

	_, err := osutil.IsMounted("/snap/ubuntu-core/855")
	c.Check(err, ErrorMatches, "open /no/such/file: no such file or directory")
}

func (s *mountSuite) TestIsMountedIncorrectLines(c *C) {
	mockMountInfoFn := filepath.Join(c.MkDir(), "mountinfo")
	restore := osutil.MockMountInfoPath(mockMountInfoFn)
	defer restore()

	content := []byte(`
invalid line
`)
	err := ioutil.WriteFile(mockMountInfoFn, content, 0644)
	c.Assert(err, IsNil)

	_, err = osutil.IsMounted("/snap/ubuntu-core/855")
	c.Check(err, ErrorMatches, `unexpected mountinfo line: "invalid line"`)
}
