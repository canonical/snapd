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

package main_test

import (
	"strings"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
)

type bootstrapSuite struct{}

var _ = Suite(&bootstrapSuite{})

func (s *bootstrapSuite) TestReadCmdLine(c *C) {
	buf := make([]byte, 1024)
	numRead := update.ReadCmdline(buf)
	c.Assert(numRead, Not(Equals), -1)
	c.Assert(numRead, Not(Equals), 1)
	// Individual arguments are separated with NUL byte.
	argv := strings.Split(string(buf[0:numRead]), "\x00")
	// Smoke test, the actual value looks like
	// "/tmp/go-build020699516/github.com/snapcore/snapd/cmd/snap-update-ns/_test/snap-update-ns.test"
	c.Assert(strings.HasSuffix(argv[0], "snap-update-ns.test"), Equals, true, Commentf("argv[0] is %q", argv[0]))
}

// Check that if there is only one argument we return nil.
func (s *bootstrapSuite) TestFindSnapName1(c *C) {
	buf := []byte("arg0\x00")
	result := update.FindSnapName(buf)
	c.Assert(result, Equals, (*string)(nil))
}

// Check that if there are multiple arguments we return the 2nd one.
func (s *bootstrapSuite) TestFindSnapName2(c *C) {
	buf := []byte("arg0\x00arg1\x00arg2\x00")
	result := update.FindSnapName(buf)
	c.Assert(result, Not(Equals), (*string)(nil))
	c.Assert(*result, Equals, "arg1")
}

// Check that if the 1st argument in the buffer is not terminated we don't crash.
func (s *bootstrapSuite) TestFindSnapName3(c *C) {
	buf := []byte("arg0")
	result := update.FindSnapName(buf)
	c.Assert(result, Equals, (*string)(nil))
}

// Check that if the 2nd argument in the buffer is not terminated we don't crash.
func (s *bootstrapSuite) TestFindSnapName4(c *C) {
	buf := []byte("arg0\x00arg1")
	result := update.FindSnapName(buf)
	c.Assert(result, Not(Equals), (*string)(nil))
	c.Assert(*result, Equals, "arg1")
}

// Check that sanitizeSnapName rejects "/" and "..".
func (s *bootstrapSuite) TestSanitizeSnapName(c *C) {
	c.Assert(update.SanitizeSnapName("hello-world"), Equals, 0)
	c.Assert(update.SanitizeSnapName("hello/world"), Equals, -1)
	c.Assert(update.SanitizeSnapName("hello..world"), Equals, -1)
}

// Check that pre-go bootstrap code is disabled by default
func (s *bootstrapSuite) TestBootstrapDisabled(c *C) {
	c.Assert(update.BootstrapError(), ErrorMatches, "bootstrap is not enabled, set SNAPD_INTERNAL=.*")
}
