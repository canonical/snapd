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

package osutil_test

import (
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type testhelperSuite struct{}

var _ = Suite(&testhelperSuite{})

func mockOsArgs(args []string) (restore func()) {
	old := os.Args
	os.Args = args
	return func() {
		os.Args = old
	}
}

func (s *testhelperSuite) TestIsTestBinary(c *C) {
	// obvious case
	c.Assert(osutil.IsTestBinary(), Equals, true)

	defer mockOsArgs([]string{"foo", "bar", "baz"})()
	c.Assert(osutil.IsTestBinary(), Equals, false)
}

func (s *testhelperSuite) TestMustBeTestBinary(c *C) {
	// obvious case
	osutil.MustBeTestBinary("unexpected panic")

	defer mockOsArgs([]string{"foo", "bar", "baz"})()
	c.Assert(func() { osutil.MustBeTestBinary("panic message") }, PanicMatches, "panic message")
}

func (s *testhelperSuite) TestBinaryNoRegressionWithValidApp(c *C) {
	// a snap app named 'test' is valid, we must not be confused here
	defer mockOsArgs([]string{"/snap/bin/some-snap.test", "bar", "baz"})()
	// must not be considered a test binary
	c.Assert(osutil.IsTestBinary(), Equals, false)
	// must panic since binary is a non-test one
	c.Assert(func() { osutil.MustBeTestBinary("non test binary, expecting a panic") },
		PanicMatches, "non test binary, expecting a panic")
}
