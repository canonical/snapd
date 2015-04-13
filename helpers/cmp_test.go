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

package helpers

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	. "launchpad.net/gocheck"
)

func (ts *HTestSuite) TestCmp(c *C) {
	tmpdir := c.MkDir()

	foo := filepath.Join(tmpdir, "foo")
	f, err := os.Create(foo)
	c.Assert(err, IsNil)
	defer f.Close()

	for i := 0; i < 1100; i++ {
		c.Assert(FilesAreEqual(foo, foo), Equals, true)
		_, err := f.WriteString("****************")
		c.Assert(err, IsNil)
		f.Sync()
	}
}

func (ts *HTestSuite) TestCmpEmptyNeqMissing(c *C) {
	tmpdir := c.MkDir()

	foo := filepath.Join(tmpdir, "foo")
	bar := filepath.Join(tmpdir, "bar")
	f, err := os.Create(foo)
	c.Assert(err, IsNil)
	defer f.Close()
	c.Assert(FilesAreEqual(foo, bar), Equals, false)
	c.Assert(FilesAreEqual(bar, foo), Equals, false)
}

func (ts *HTestSuite) TestCmpEmptyNeqNonEmpty(c *C) {
	tmpdir := c.MkDir()

	foo := filepath.Join(tmpdir, "foo")
	bar := filepath.Join(tmpdir, "bar")
	f, err := os.Create(foo)
	c.Assert(err, IsNil)
	defer f.Close()
	c.Assert(ioutil.WriteFile(bar, []byte("x"), 0644), IsNil)
	c.Assert(FilesAreEqual(foo, bar), Equals, false)
	c.Assert(FilesAreEqual(bar, foo), Equals, false)
}

func (ts *HTestSuite) TestCmpStreams(c *C) {
	for _, x := range []struct {
		a string
		b string
		r bool
	}{
		{"hello", "hello", true},
		{"hello", "world", false},
		{"hello", "hell", false},
	} {
		c.Assert(streamsEqual(strings.NewReader(x.a), strings.NewReader(x.b)), Equals, x.r)
	}
}

func (ts *HTestSuite) TestDirUpdated(c *C) {
	d1 := c.MkDir()
	d2 := c.MkDir()

	// 1. empty directories are not updated, and all ok
	c.Check(DirUpdated(d1, "", d2), HasLen, 0)

	// 2. a directory with an extra file in it is ignored,
	c.Assert(ioutil.WriteFile(filepath.Join(d2, "foo"), []byte("x"), 0644), IsNil)
	c.Check(DirUpdated(d1, "", d2), HasLen, 0)

	//   (on either side)
	c.Check(DirUpdated(d2, "", d1), HasLen, 0)

	// 3. if files are equal, it's not updated
	c.Assert(ioutil.WriteFile(filepath.Join(d1, "foo"), []byte("x"), 0644), IsNil)
	c.Check(DirUpdated(d1, "", d2), HasLen, 0)

	// 4. sub-directories are ignored
	c.Assert(os.Mkdir(filepath.Join(d1, "dir"), 0755), IsNil)
	c.Check(DirUpdated(d1, "", d2), HasLen, 0)

	// 5. all files that are different are returned
	c.Assert(ioutil.WriteFile(filepath.Join(d1, "foo"), []byte("y"), 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d1, "bar"), []byte("x"), 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d2, "bar"), []byte("y"), 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(d2, "baz"), []byte("x"), 0644), IsNil)
	c.Check(DirUpdated(d1, "", d2), DeepEquals, map[string]bool{"bar": true, "foo": true})

	// 6. as long as they have the prefix
	c.Assert(os.Rename(filepath.Join(d1, "foo"), filepath.Join(d1, "quux_foo")), IsNil)
	c.Check(DirUpdated(d1, "quux_", d2), DeepEquals, map[string]bool{"foo": true})
}
