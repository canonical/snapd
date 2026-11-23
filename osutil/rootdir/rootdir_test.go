// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package rootdir_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil/rootdir"
	"github.com/snapcore/snapd/testutil"
)

type rootdirSuite struct {
	testutil.BaseTest
}

var _ = Suite(&rootdirSuite{})

func (s *rootdirSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	// callbacks cannot be unregistered, so start with an empty registry
	rootdir.ResetCallbacks()
	s.AddCleanup(func() { rootdir.SetRootDir("/") })
}

func (s *rootdirSuite) TestDefaultRootDir(c *C) {
	c.Assert(rootdir.RootDir, Equals, "/")
}

func (s *rootdirSuite) TestSetRootDirEmptyMeansSlash(c *C) {
	rootdir.SetRootDir("")
	c.Assert(rootdir.RootDir, Equals, "/")

	rootdir.SetRootDir("/tmp/foo")
	c.Assert(rootdir.RootDir, Equals, "/tmp/foo")

	rootdir.SetRootDir("")
	c.Assert(rootdir.RootDir, Equals, "/")
}

func (s *rootdirSuite) TestCallbacksRunAfterUpdateInOrder(c *C) {
	var order []string
	rootdir.AddRootDirCallback(func(rootdir string) {
		order = append(order, "first:"+rootdir)
	})
	rootdir.AddRootDirCallback(func(rootdir string) {
		order = append(order, "second:"+rootdir)
	})

	rootdir.SetRootDir("/tmp/bar")
	c.Assert(order, DeepEquals, []string{"first:/tmp/bar", "second:/tmp/bar"})
}

func (s *rootdirSuite) TestStripRootDir(c *C) {
	rootdir.SetRootDir("/tmp/foo")
	stripped, err := rootdir.StripRootDir("/tmp/foo/var/lib/snapd")
	c.Assert(err, IsNil)
	c.Assert(stripped, Equals, "/var/lib/snapd")

	// with the default root
	rootdir.SetRootDir("/")
	stripped, err = rootdir.StripRootDir("/var/lib/snapd")
	c.Assert(err, IsNil)
	c.Assert(stripped, Equals, "/var/lib/snapd")
}

func (s *rootdirSuite) TestStripRootDirErrors(c *C) {
	rootdir.SetRootDir("/tmp/foo")

	_, err := rootdir.StripRootDir("relative/path")
	c.Assert(err, ErrorMatches, `supplied path is not absolute "relative/path"`)

	_, err = rootdir.StripRootDir("/other/root/var")
	c.Assert(err, ErrorMatches, `supplied path is not related to global root "/other/root/var"`)
}

func (s *rootdirSuite) TestStripRootDirRootItself(c *C) {
	// the root directory itself strips to "/.", as on master
	rootdir.SetRootDir("/tmp/foo")
	stripped, err := rootdir.StripRootDir("/tmp/foo")
	c.Assert(err, IsNil)
	c.Assert(stripped, Equals, "/.")

	// also with the default root
	rootdir.SetRootDir("/")
	stripped, err = rootdir.StripRootDir("/")
	c.Assert(err, IsNil)
	c.Assert(stripped, Equals, "/.")
}

func (s *rootdirSuite) TestStripRootDirTrailingSlashRoot(c *C) {
	// a root dir without a trailing slash must not match a sibling
	// directory with the same prefix
	rootdir.SetRootDir("/tmp/foo")
	_, err := rootdir.StripRootDir("/tmp/foobar")
	c.Assert(err, ErrorMatches, `supplied path is not related to global root "/tmp/foobar"`)
}

func Test(t *testing.T) { TestingT(t) }
