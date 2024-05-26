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

package osutil_test

import (
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
)

type unlinkSuite struct {
	d string
}

var _ = check.Suite(&unlinkSuite{})

func (s *unlinkSuite) checkDirnames(c *check.C, names []string) {
	dir := mylog.Check2(os.Open(s.d))
	c.Assert(err, check.IsNil)
	defer dir.Close()
	found := mylog.Check2(dir.Readdirnames(-1))
	c.Assert(err, check.IsNil)
	sort.Strings(names)
	sort.Strings(found)
	c.Check(found, check.DeepEquals, names)
}

func (s *unlinkSuite) SetUpTest(c *check.C) {
	s.d = c.MkDir()
	s.mkFixture(c)
}

func (s *unlinkSuite) mkFixture(c *check.C) {
	for _, fname := range []string{"foo", "bar", "baz", "quux"} {
		f := mylog.Check2(os.Create(filepath.Join(s.d, fname)))
		if err == nil {
			f.Close()
		} else if !os.IsExist(err) {
			c.Fatal(err)
		}
	}
	if mylog.Check(os.Mkdir(filepath.Join(s.d, "dir"), 0700)); err != nil && !os.IsExist(err) {
		c.Fatal(err)
	}
}

func (s *unlinkSuite) TestUnlinkMany(c *check.C) {
	c.Assert(osutil.UnlinkMany(s.d, []string{"bar", "does-not-exist", "baz"}), check.IsNil)

	s.checkDirnames(c, []string{"foo", "quux", "dir"})
}

func (s *unlinkSuite) TestUnlinkManyAt(c *check.C) {
	d := mylog.Check2(os.Open(s.d))
	c.Assert(err, check.IsNil)
	c.Assert(osutil.UnlinkManyAt(d, []string{"bar", "does-not-exist", "baz"}), check.IsNil)

	s.checkDirnames(c, []string{"foo", "quux", "dir"})
}

func (s *unlinkSuite) TestUnlinkManyFails(c *check.C) {
	type T struct {
		dirname   string
		filenames []string
		expected  string
	}
	tests := []T{
		{
			dirname:   filepath.Join(s.d, "does-not-exist"),
			filenames: []string{"bar", "baz"},
			expected:  `open /tmp/.*/does-not-exist: no such file or directory`,
		}, {
			dirname:   filepath.Join(s.d, "foo"),
			filenames: []string{"bar", "baz"},
			expected:  `open /tmp/.*/foo: not a directory`,
		}, {
			dirname:   s.d,
			filenames: []string{"bar", "dir", "baz"},
			expected:  `remove dir: is a directory`,
		},
	}

	for i, test := range tests {
		c.Check(osutil.UnlinkMany(test.dirname, test.filenames), check.ErrorMatches, test.expected, check.Commentf("%d", i))
		s.mkFixture(c)
	}
}
