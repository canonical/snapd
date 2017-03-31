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

package osutil_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/osutil"

	. "gopkg.in/check.v1"
)

type findInPathSuite struct {
	basePath string
}

var _ = Suite(&findInPathSuite{})

func (s *findInPathSuite) relocatePath(path string) string {
	return filepath.Join(s.basePath, path)
}

func (s *findInPathSuite) SetUpSuite(c *C) {
	p, err := ioutil.TempDir("", "find-in-path")
	c.Assert(err, Equals, nil)
	s.basePath = p
	osutil.Getenv = func(key string) string {
		if key != "PATH" {
			return ""
		}
		return fmt.Sprintf("%s/bin:%s/usr/bin", s.basePath, s.basePath)
	}

	for _, d := range []string{"/bin", "/usr/bin"} {
		os.MkdirAll(s.relocatePath(d), 0755)
	}

	for _, fname := range []string{"/bin/true", "/bin/foo", "/usr/bin/false", "/usr/bin/foo"} {
		f, err := os.Create(s.relocatePath(fname))
		c.Assert(err, Equals, nil)
		f.Close()
	}
}

func (s *findInPathSuite) TestGivesCorrectPath(c *C) {
	c.Assert(osutil.FindInPath("true"), Equals, s.relocatePath("/bin/true"))
	c.Assert(osutil.FindInPath("false"), Equals, s.relocatePath("/usr/bin/false"))
}

func (s *findInPathSuite) TestRespectsPriorityOrder(c *C) {
	c.Assert(osutil.FindInPath("foo"), Equals, s.relocatePath("/bin/foo"))
}

func (s *findInPathSuite) TestReturnsDefaultWhenNotFound(c *C) {
	c.Assert(osutil.FindInPathOrDefault("bar", "/bin/bla"), Equals, "/bin/bla")
}
