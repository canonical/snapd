// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type envSuite struct{}

var _ = check.Suite(&envSuite{})

func (s *envSuite) TestGetenvBoolTrue(c *check.C) {
	key := "__XYZZY__"
	os.Unsetenv(key)

	for _, s := range []string{
		"1", "t", "TRUE",
	} {
		os.Setenv(key, s)
		c.Assert(os.Getenv(key), check.Equals, s)
		c.Check(osutil.GetenvBool(key), check.Equals, true, check.Commentf(s))
		c.Check(osutil.GetenvBool(key, false), check.Equals, true, check.Commentf(s))
		c.Check(osutil.GetenvBool(key, true), check.Equals, true, check.Commentf(s))
	}
}

func (s *envSuite) TestGetenvBoolFalse(c *check.C) {
	key := "__XYZZY__"
	os.Unsetenv(key)
	c.Assert(osutil.GetenvBool(key), check.Equals, false)

	for _, s := range []string{
		"", "0", "f", "FALSE", "potato",
	} {
		os.Setenv(key, s)
		c.Assert(os.Getenv(key), check.Equals, s)
		c.Check(osutil.GetenvBool(key), check.Equals, false, check.Commentf(s))
		c.Check(osutil.GetenvBool(key, false), check.Equals, false, check.Commentf(s))
	}
}

func (s *envSuite) TestGetenvBoolFalseDefaultTrue(c *check.C) {
	key := "__XYZZY__"
	os.Unsetenv(key)
	c.Assert(osutil.GetenvBool(key), check.Equals, false)

	for _, s := range []string{
		"0", "f", "FALSE",
	} {
		os.Setenv(key, s)
		c.Assert(os.Getenv(key), check.Equals, s)
		c.Check(osutil.GetenvBool(key, true), check.Equals, false, check.Commentf(s))
	}

	for _, s := range []string{
		"", "potato", // etc
	} {
		os.Setenv(key, s)
		c.Assert(os.Getenv(key), check.Equals, s)
		c.Check(osutil.GetenvBool(key, true), check.Equals, true, check.Commentf(s))
	}
}
