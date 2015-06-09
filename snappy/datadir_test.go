// -*- Mode: Go; indent-tabs-mode: t -*-

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

package snappy

import (
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"
)

type DataDirSuite struct{}

var _ = Suite(&DataDirSuite{})

func (s *DataDirSuite) SetUpTest(c *C) {
	SetRootDir(c.MkDir())
}

func (s *DataDirSuite) TestSystemDataDirs(c *C) {
	c.Assert(os.MkdirAll(filepath.Join(snapDataDir, "foo.bar", "v1"), 0755), IsNil)
	dds := DataDirs("foo")
	c.Check(dds, DeepEquals, []SnapDataDir{{
		Base:    snapDataDir,
		Name:    "foo",
		Origin:  "bar",
		Version: "v1",
	}})
	c.Check(DataDirs("f"), HasLen, 0)
	c.Check(DataDirs("foobar"), HasLen, 0)
	c.Check(DataDirs("foo.bar"), HasLen, 1)
	c.Check(DataDirs("foo=v1"), HasLen, 1)
	c.Check(DataDirs("foo.bar=v1"), HasLen, 1)
}

func (s *DataDirSuite) TestDataDirsFramework(c *C) {
	c.Assert(os.MkdirAll(filepath.Join(snapDataDir, "foo", "v1"), 0755), IsNil)
	dds := DataDirs("foo")
	c.Check(dds, DeepEquals, []SnapDataDir{{
		Base:    snapDataDir,
		Name:    "foo",
		Origin:  "",
		Version: "v1",
	}})
	c.Check(DataDirs("foo=v1"), HasLen, 1)
}

func (s *DataDirSuite) TestHomeDataDirs(c *C) {
	home := strings.Replace(snapDataHomeGlob, "*", "user1", -1)
	c.Assert(os.MkdirAll(filepath.Join(home, "foo.bar", "v1"), 0755), IsNil)
	dds := DataDirs("foo")
	c.Check(dds, DeepEquals, []SnapDataDir{{
		Base:    snapDataHomeGlob,
		Name:    "foo",
		Origin:  "bar",
		Version: "v1",
	}})
}

func (s *DataDirSuite) TestEverywhichwhereDataDirs(c *C) {
	home := strings.Replace(snapDataHomeGlob, "*", "user1", -1)
	c.Assert(os.MkdirAll(filepath.Join(home, "foo.bar", "v0"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(home, "foo.bar", "v1"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(snapDataDir, "foo.xyzzy", "v1"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(snapDataDir, "foo", "v3"), 0755), IsNil)
	dds := DataDirs("foo")
	c.Assert(dds, HasLen, 4)
	hi := 0
	si := 2
	if dds[0].Base == snapDataDir {
		si = 0
		hi = 2
	}
	c.Check(dds[hi], DeepEquals, SnapDataDir{
		Base:    snapDataHomeGlob,
		Name:    "foo",
		Origin:  "bar",
		Version: "v0",
	})
	c.Check(dds[hi+1], DeepEquals, SnapDataDir{
		Base:    snapDataHomeGlob,
		Name:    "foo",
		Origin:  "bar",
		Version: "v1",
	})
	c.Check(dds[si], DeepEquals, SnapDataDir{
		Base:    snapDataDir,
		Name:    "foo",
		Origin:  "",
		Version: "v3",
	})
	c.Check(dds[si+1], DeepEquals, SnapDataDir{
		Base:    snapDataDir,
		Name:    "foo",
		Origin:  "xyzzy",
		Version: "v1",
	})
}

func (s *DataDirSuite) TestDataDirQualifiedName(c *C) {
	c.Check(SnapDataDir{Name: "foo", Origin: "bar"}.QualifiedName(), Equals, "foo.bar")
	c.Check(SnapDataDir{Name: "foo"}.QualifiedName(), Equals, "foo")
}
