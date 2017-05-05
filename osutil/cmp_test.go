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

package osutil

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"
)

type CmpTestSuite struct{}

var _ = Suite(&CmpTestSuite{})

func (ts *CmpTestSuite) TestCmp(c *C) {
	tmpdir := c.MkDir()

	foo := filepath.Join(tmpdir, "foo")
	f, err := os.Create(foo)
	c.Assert(err, IsNil)
	defer f.Close()

	// pick a smaller bufsize so that the test can complete quicker
	defer func() {
		bufsz = defaultBufsz
	}()
	bufsz = 128

	// test FilesAreEqual for various sizes:
	// - bufsz not exceeded
	// - bufsz matches file size
	// - bufsz exceeds file size
	canary := "1234567890123456"
	for _, n := range []int{1, bufsz / len(canary), (bufsz / len(canary)) + 1} {
		for i := 0; i < n; i++ {
			c.Assert(FilesAreEqual(foo, foo), Equals, true)
			_, err := f.WriteString(canary)
			c.Assert(err, IsNil)
			f.Sync()
		}
	}
}

func (ts *CmpTestSuite) TestCmpEmptyNeqMissing(c *C) {
	tmpdir := c.MkDir()

	foo := filepath.Join(tmpdir, "foo")
	bar := filepath.Join(tmpdir, "bar")
	f, err := os.Create(foo)
	c.Assert(err, IsNil)
	defer f.Close()
	c.Assert(FilesAreEqual(foo, bar), Equals, false)
	c.Assert(FilesAreEqual(bar, foo), Equals, false)
}

func (ts *CmpTestSuite) TestCmpEmptyNeqNonEmpty(c *C) {
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

func (ts *CmpTestSuite) TestCmpStreams(c *C) {
	for _, x := range []struct {
		a string
		b string
		r bool
	}{
		{"hello", "hello", true},
		{"hello", "world", false},
		{"hello", "hell", false},
	} {
		c.Assert(StreamsEqual(strings.NewReader(x.a), strings.NewReader(x.b)), Equals, x.r)
	}
}
