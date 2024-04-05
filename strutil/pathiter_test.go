// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package strutil_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil"
)

type pathIterSuite struct{}

var _ = Suite(&pathIterSuite{})

func (s *pathIterSuite) TestPathIteratorEmpty(c *C) {
	iter, err := strutil.NewPathIterator("")
	c.Assert(err, ErrorMatches, `cannot iterate over unclean path ""`)
	c.Assert(iter, IsNil)
}

func (s *pathIterSuite) TestPathIteratorFilename(c *C) {
	iter, err := strutil.NewPathIterator("foo")
	c.Assert(err, IsNil)
	c.Assert(iter.Path(), Equals, "foo")
	c.Assert(iter.Depth(), Equals, 0)

	c.Assert(iter.Next(), Equals, true)
	c.Assert(iter.CurrentBaseNoSlash(), Equals, "")
	c.Assert(iter.CurrentPath(), Equals, "foo")
	c.Assert(iter.CurrentPathNoSlash(), Equals, "foo")
	c.Assert(iter.CurrentName(), Equals, "foo")
	c.Assert(iter.CurrentNameNoSlash(), Equals, "foo")
	c.Assert(iter.Depth(), Equals, 1)

	c.Assert(iter.Next(), Equals, false)
	c.Assert(iter.Depth(), Equals, 1)
}

func (s *pathIterSuite) TestPathIteratorRelative(c *C) {
	iter, err := strutil.NewPathIterator("foo/bar")
	c.Assert(err, IsNil)
	c.Assert(iter.Path(), Equals, "foo/bar")
	c.Assert(iter.Depth(), Equals, 0)

	c.Assert(iter.Next(), Equals, true)
	c.Assert(iter.CurrentBaseNoSlash(), Equals, "")
	c.Assert(iter.CurrentPath(), Equals, "foo/")
	c.Assert(iter.CurrentPathNoSlash(), Equals, "foo")
	c.Assert(iter.CurrentName(), Equals, "foo/")
	c.Assert(iter.CurrentNameNoSlash(), Equals, "foo")
	c.Assert(iter.Depth(), Equals, 1)

	c.Assert(iter.Next(), Equals, true)
	c.Assert(iter.CurrentBaseNoSlash(), Equals, "foo")
	c.Assert(iter.CurrentPath(), Equals, "foo/bar")
	c.Assert(iter.CurrentPathNoSlash(), Equals, "foo/bar")
	c.Assert(iter.CurrentName(), Equals, "bar")
	c.Assert(iter.CurrentNameNoSlash(), Equals, "bar")
	c.Assert(iter.Depth(), Equals, 2)

	c.Assert(iter.Next(), Equals, false)
	c.Assert(iter.Depth(), Equals, 2)
}

func (s *pathIterSuite) TestPathIteratorAbsoluteAlmostClean(c *C) {
	iter, err := strutil.NewPathIterator("/foo/bar/")
	c.Assert(err, IsNil)
	c.Assert(iter.Path(), Equals, "/foo/bar/")
	c.Assert(iter.Depth(), Equals, 0)

	c.Assert(iter.Next(), Equals, true)
	c.Assert(iter.CurrentBaseNoSlash(), Equals, "")
	c.Assert(iter.CurrentPath(), Equals, "/")
	c.Assert(iter.CurrentPathNoSlash(), Equals, "/")
	c.Assert(iter.CurrentName(), Equals, "/")
	c.Assert(iter.CurrentNameNoSlash(), Equals, "")
	c.Assert(iter.Depth(), Equals, 1)

	c.Assert(iter.Next(), Equals, true)
	c.Assert(iter.CurrentBaseNoSlash(), Equals, "/")
	c.Assert(iter.CurrentPath(), Equals, "/foo/")
	c.Assert(iter.CurrentPathNoSlash(), Equals, "/foo")
	c.Assert(iter.CurrentName(), Equals, "foo/")
	c.Assert(iter.CurrentNameNoSlash(), Equals, "foo")
	c.Assert(iter.Depth(), Equals, 2)

	c.Assert(iter.Next(), Equals, true)
	c.Assert(iter.CurrentBaseNoSlash(), Equals, "/foo")
	c.Assert(iter.CurrentPath(), Equals, "/foo/bar/")
	c.Assert(iter.CurrentPathNoSlash(), Equals, "/foo/bar")
	c.Assert(iter.CurrentName(), Equals, "bar/")
	c.Assert(iter.CurrentNameNoSlash(), Equals, "bar")
	c.Assert(iter.Depth(), Equals, 3)

	c.Assert(iter.Next(), Equals, false)
	c.Assert(iter.Depth(), Equals, 3)
}

func (s *pathIterSuite) TestPathIteratorAbsoluteClean(c *C) {
	iter, err := strutil.NewPathIterator("/foo/bar")
	c.Assert(err, IsNil)
	c.Assert(iter.Path(), Equals, "/foo/bar")
	c.Assert(iter.Depth(), Equals, 0)

	c.Assert(iter.Next(), Equals, true)
	c.Assert(iter.CurrentBaseNoSlash(), Equals, "")
	c.Assert(iter.CurrentPath(), Equals, "/")
	c.Assert(iter.CurrentPathNoSlash(), Equals, "/")
	c.Assert(iter.CurrentName(), Equals, "/")
	c.Assert(iter.CurrentNameNoSlash(), Equals, "")
	c.Assert(iter.Depth(), Equals, 1)

	c.Assert(iter.Next(), Equals, true)
	c.Assert(iter.CurrentBaseNoSlash(), Equals, "/")
	c.Assert(iter.CurrentPath(), Equals, "/foo/")
	c.Assert(iter.CurrentPathNoSlash(), Equals, "/foo")
	c.Assert(iter.CurrentName(), Equals, "foo/")
	c.Assert(iter.CurrentNameNoSlash(), Equals, "foo")
	c.Assert(iter.Depth(), Equals, 2)

	c.Assert(iter.Next(), Equals, true)
	c.Assert(iter.CurrentBaseNoSlash(), Equals, "/foo")
	c.Assert(iter.CurrentPath(), Equals, "/foo/bar")
	c.Assert(iter.CurrentPath(), Equals, "/foo/bar")
	c.Assert(iter.CurrentName(), Equals, "bar")
	c.Assert(iter.CurrentNameNoSlash(), Equals, "bar")
	c.Assert(iter.Depth(), Equals, 3)

	c.Assert(iter.Next(), Equals, false)
	c.Assert(iter.Depth(), Equals, 3)
}

func (s *pathIterSuite) TestPathIteratorAbsoluteCleanDepth4(c *C) {
	iter, err := strutil.NewPathIterator("/foo/bar/baz")
	c.Assert(err, IsNil)
	c.Assert(iter.Path(), Equals, "/foo/bar/baz")
	c.Assert(iter.Depth(), Equals, 0)

	c.Assert(iter.Next(), Equals, true)
	c.Assert(iter.CurrentBaseNoSlash(), Equals, "")
	c.Assert(iter.CurrentPath(), Equals, "/")
	c.Assert(iter.CurrentPathNoSlash(), Equals, "/")
	c.Assert(iter.CurrentName(), Equals, "/")
	c.Assert(iter.CurrentNameNoSlash(), Equals, "")
	c.Assert(iter.Depth(), Equals, 1)

	c.Assert(iter.Next(), Equals, true)
	c.Assert(iter.CurrentBaseNoSlash(), Equals, "/")
	c.Assert(iter.CurrentPath(), Equals, "/foo/")
	c.Assert(iter.CurrentPathNoSlash(), Equals, "/foo")
	c.Assert(iter.CurrentName(), Equals, "foo/")
	c.Assert(iter.CurrentNameNoSlash(), Equals, "foo")
	c.Assert(iter.Depth(), Equals, 2)

	c.Assert(iter.Next(), Equals, true)
	c.Assert(iter.CurrentBaseNoSlash(), Equals, "/foo")
	c.Assert(iter.CurrentPath(), Equals, "/foo/bar/")
	c.Assert(iter.CurrentPathNoSlash(), Equals, "/foo/bar")
	c.Assert(iter.CurrentName(), Equals, "bar/")
	c.Assert(iter.CurrentNameNoSlash(), Equals, "bar")
	c.Assert(iter.Depth(), Equals, 3)

	c.Assert(iter.Next(), Equals, true)
	c.Assert(iter.CurrentBaseNoSlash(), Equals, "/foo/bar")
	c.Assert(iter.CurrentPath(), Equals, "/foo/bar/baz")
	c.Assert(iter.CurrentPathNoSlash(), Equals, "/foo/bar/baz")
	c.Assert(iter.CurrentName(), Equals, "baz")
	c.Assert(iter.CurrentNameNoSlash(), Equals, "baz")
	c.Assert(iter.Depth(), Equals, 4)

	c.Assert(iter.Next(), Equals, false)
	c.Assert(iter.Depth(), Equals, 4)
}

func (s *pathIterSuite) TestPathIteratorRootDir(c *C) {
	iter, err := strutil.NewPathIterator("/")
	c.Assert(err, IsNil)
	c.Assert(iter.Path(), Equals, "/")
	c.Assert(iter.Depth(), Equals, 0)

	c.Assert(iter.Next(), Equals, true)
	c.Assert(iter.CurrentBaseNoSlash(), Equals, "")
	c.Assert(iter.CurrentPath(), Equals, "/")
	c.Assert(iter.CurrentPathNoSlash(), Equals, "/")
	c.Assert(iter.CurrentName(), Equals, "/")
	c.Assert(iter.CurrentNameNoSlash(), Equals, "")
	c.Assert(iter.Depth(), Equals, 1)

	c.Assert(iter.Next(), Equals, false)
	c.Assert(iter.Depth(), Equals, 1)
}

func (s *pathIterSuite) TestPathIteratorUncleanPath(c *C) {
	iter, err := strutil.NewPathIterator("///some/../junk")
	c.Assert(err, ErrorMatches, `cannot iterate over unclean path ".*"`)
	c.Assert(iter, IsNil)
}

func (s *pathIterSuite) TestPathIteratorUnicode(c *C) {
	iter, err := strutil.NewPathIterator("/zażółć/gęślą/jaźń")
	c.Assert(err, IsNil)
	c.Assert(iter.Path(), Equals, "/zażółć/gęślą/jaźń")
	c.Assert(iter.Depth(), Equals, 0)

	c.Assert(iter.Next(), Equals, true)
	c.Assert(iter.CurrentBaseNoSlash(), Equals, "")
	c.Assert(iter.CurrentPath(), Equals, "/")
	c.Assert(iter.CurrentPathNoSlash(), Equals, "/")
	c.Assert(iter.CurrentName(), Equals, "/")
	c.Assert(iter.CurrentNameNoSlash(), Equals, "")
	c.Assert(iter.Depth(), Equals, 1)

	c.Assert(iter.Next(), Equals, true)
	c.Assert(iter.CurrentBaseNoSlash(), Equals, "/")
	c.Assert(iter.CurrentPath(), Equals, "/zażółć/")
	c.Assert(iter.CurrentPathNoSlash(), Equals, "/zażółć")
	c.Assert(iter.CurrentName(), Equals, "zażółć/")
	c.Assert(iter.CurrentNameNoSlash(), Equals, "zażółć")
	c.Assert(iter.Depth(), Equals, 2)

	c.Assert(iter.Next(), Equals, true)
	c.Assert(iter.CurrentBaseNoSlash(), Equals, "/zażółć")
	c.Assert(iter.CurrentPath(), Equals, "/zażółć/gęślą/")
	c.Assert(iter.CurrentPathNoSlash(), Equals, "/zażółć/gęślą")
	c.Assert(iter.CurrentName(), Equals, "gęślą/")
	c.Assert(iter.CurrentNameNoSlash(), Equals, "gęślą")
	c.Assert(iter.Depth(), Equals, 3)

	c.Assert(iter.Next(), Equals, true)
	c.Assert(iter.CurrentBaseNoSlash(), Equals, "/zażółć/gęślą")
	c.Assert(iter.CurrentPath(), Equals, "/zażółć/gęślą/jaźń")
	c.Assert(iter.CurrentPathNoSlash(), Equals, "/zażółć/gęślą/jaźń")
	c.Assert(iter.CurrentName(), Equals, "jaźń")
	c.Assert(iter.CurrentNameNoSlash(), Equals, "jaźń")
	c.Assert(iter.Depth(), Equals, 4)

	c.Assert(iter.Next(), Equals, false)
	c.Assert(iter.Depth(), Equals, 4)
}

func (s *pathIterSuite) TestPathIteratorRewind(c *C) {
	iter, err := strutil.NewPathIterator("/foo/bar")
	c.Assert(err, IsNil)
	for i := 0; i < 2; i++ {
		c.Assert(iter.Next(), Equals, true)
		c.Assert(iter.Depth(), Equals, 1)
		c.Assert(iter.CurrentPath(), Equals, "/")
		c.Assert(iter.CurrentPathNoSlash(), Equals, "/")
		c.Assert(iter.Next(), Equals, true)
		c.Assert(iter.Depth(), Equals, 2)
		c.Assert(iter.CurrentPath(), Equals, "/foo/")
		c.Assert(iter.CurrentPathNoSlash(), Equals, "/foo")
		iter.Rewind()
	}
}

func (s *pathIterSuite) TestPathIteratorExample(c *C) {
	iter, err := strutil.NewPathIterator("/some/path/there")
	c.Assert(err, IsNil)
	for iter.Next() {
		_ = iter.CurrentBaseNoSlash()
		_ = iter.CurrentPath()
		_ = iter.CurrentPathNoSlash()
		_ = iter.CurrentName()
		_ = iter.CurrentNameNoSlash()
		_ = iter.Depth()
	}
}
