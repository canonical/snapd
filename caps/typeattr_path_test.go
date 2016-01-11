// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package caps

import (
	"fmt"
	"regexp"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/testutil"
)

type pathSuite struct {
	testutil.BaseTest
	pathAttr    TypeAttr
	devPathAttr TypeAttr
	capType     *Type
	cap         *Capability
}

var _ = Suite(&pathSuite{})

func (s *pathSuite) SetUpSuite(c *C) {
	// A path attribute that accepts any path
	s.pathAttr = &pathAttr{
		errorHint:       "path cannot be empty",
		allowedPatterns: []*regexp.Regexp{regexp.MustCompile(".+")},
	}
	// A path attribute restricted to files in /dev/
	s.devPathAttr = &pathAttr{
		errorHint:       "devPath must point to something in /dev/",
		allowedPatterns: []*regexp.Regexp{regexp.MustCompile("^/dev/.+$")},
	}
	// An test capability and capability type using attributes
	s.capType = &Type{
		Name: "type",
		Attrs: map[string]TypeAttr{
			"path":    s.pathAttr,
			"devPath": s.devPathAttr,
		},
	}
}

func (s *pathSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	// For testing, mock the filepath.EvalSymlinks function to a simple
	// identity function that never fails. Individual tests can replace that
	// with anything they want.
	MockEvalSymlinks(&s.BaseTest, IgnoreSymbolicLinks)
	// Give each test a fresh capability object
	s.cap = &Capability{
		Name: "cap",
		Type: s.capType,
	}
}

func (s *pathSuite) TestSetAttr(c *C) {
	err := s.cap.SetAttr("path", "/some/path")
	c.Assert(err, IsNil)
	c.Assert(s.cap.Attrs["path"], Equals, "/some/path")
}

func (s *pathSuite) TestSetAttrEvaluatesSymlinks(c *C) {
	MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		return "real", nil
	})
	err := s.cap.SetAttr("path", "symbolic")
	c.Assert(err, IsNil)
	c.Assert(s.cap.Attrs["path"], Equals, "real")
}

func (s *pathSuite) TestSetAttrHandlesSymlinkErrors(c *C) {
	MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		return "", fmt.Errorf("broken symbolic link")
	})
	err := s.cap.SetAttr("path", "symbolic")
	c.Assert(err, ErrorMatches, "invalid path, cannot traverse symbolic links: broken symbolic link")
	value, ok := s.cap.Attrs["path"]
	c.Assert(value, Equals, nil)
	c.Assert(ok, Equals, false)
}

func (s *pathSuite) TestSetAttrChecksForValidValue(c *C) {
	err := s.cap.SetAttr("devPath", "/etc/passwd")
	c.Assert(err, ErrorMatches, "invalid path, devPath must point to something in /dev/")
	value, ok := s.cap.Attrs["devPath"]
	c.Assert(value, Equals, nil)
	c.Assert(ok, Equals, false)
}

func (s *pathSuite) TestGetAttrWhenUnset(c *C) {
	value, err := s.cap.GetAttr("path")
	c.Assert(err, ErrorMatches, "path is not set")
	c.Assert(value, IsNil)
}

func (s *pathSuite) TestGetAttrWhenSet(c *C) {
	err := s.cap.SetAttr("path", "/some/path")
	c.Assert(err, IsNil)
	value, err := s.cap.GetAttr("path")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "/some/path")
}

func (s *pathSuite) TestSmoke(c *C) {
	value, err := s.cap.GetAttr("path")
	c.Assert(value, Equals, nil)
	c.Assert(err, ErrorMatches, "path is not set")
	err = s.cap.SetAttr("path", "/some/path")
	c.Assert(err, IsNil)
	value, err = s.cap.GetAttr("path")
	c.Assert(value, Equals, "/some/path")
	c.Assert(err, IsNil)
}
