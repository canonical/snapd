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

	"github.com/snapcore/snapd/osutil"

	. "gopkg.in/check.v1"
)

type lookupPathSuite struct {
	basePath string
}

var _ = Suite(&lookupPathSuite{})

func (s *lookupPathSuite) TestGivesCorrectPath(c *C) {
	osutil.LookPath = func(name string) (string, error) { return "/bin/true", nil }
	c.Assert(osutil.LookupPath("true"), Equals, "/bin/true")
}

func (s *lookupPathSuite) TestReturnsDefaultWhenNotFound(c *C) {
	osutil.LookPath = func(name string) (string, error) { return "", fmt.Errorf("Not found") }
	c.Assert(osutil.LookupPathWithDefault("bar", "/bin/bla"), Equals, "/bin/bla")
}
