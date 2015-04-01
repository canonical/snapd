/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package main

import (
	"os"
	"path/filepath"

	. "launchpad.net/gocheck"
)

func makeTempFile(c *C, content string) *os.File {
	tempdir := c.MkDir()
	f, err := os.Create(filepath.Join(tempdir, "foo"))
	c.Assert(err, IsNil)
	f.Write([]byte(content))
	f.Sync()

	return f
}

func (s *CmdTestSuite) TestUidReaderPasswd(c *C) {
	f := makeTempFile(c, `root:x:0:0:root:/root:/bin/bash
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
clickpkg:x:101:104::/nonexistent:/bin/false
`)

	uid, err := readUid("clickpkg", f.Name())
	c.Assert(err, IsNil)
	c.Assert(uid, Equals, 101)
}

func (s *CmdTestSuite) TestUidReaderGroups(c *C) {
	f := makeTempFile(c, `root:x:0:
daemon:x:1:
clickpkg:x:104:
`)

	gid, err := readUid("clickpkg", f.Name())
	c.Assert(err, IsNil)
	c.Assert(gid, Equals, 104)
}

func (s *CmdTestSuite) TestUidReaderSamePrefix(c *C) {
	f := makeTempFile(c, `root:x:0:0:root:/root:/bin/bash
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
clickpkg2:x:101:104::/nonexistent:/bin/false
clickpkg:x:102:105::/nonexistent:/bin/false
`)
	defer os.Remove(f.Name())

	uid, err := readUid("clickpkg", f.Name())
	c.Assert(err, IsNil)
	c.Assert(uid, Equals, 102)
}

func (s *CmdTestSuite) TestUidReaderInvalidPasswd(c *C) {
	f := makeTempFile(c, `root:
daemon:
clickpkg:x:
`)

	_, err := readUid("clickpkg", f.Name())
	c.Assert(err, NotNil)
}

func (s *CmdTestSuite) TestUidReaderInvalidPasswd2(c *C) {
	f := makeTempFile(c, `root:
daemon:
`)

	_, err := readUid("clickpkg", f.Name())
	c.Assert(err, NotNil)
}
