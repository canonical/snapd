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
	"syscall"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/testutil"
)

type utilsSuite struct {
	testutil.BaseTest
	sys *update.SyscallRecorder
}

var _ = Suite(&utilsSuite{})

func (s *utilsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.sys = &update.SyscallRecorder{}
	s.BaseTest.AddCleanup(update.MockSystemCalls(s.sys))
}

// Ensure that we refuse to create a directory with an relative path.
func (s *utilsSuite) TestSecureMkdirAllRelative(c *C) {
	err := update.SecureMkdirAllImpl("rel/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot create directory with relative path: "rel/path"`)
	c.Assert(s.sys.Calls(), HasLen, 0)
}

// Ensure that we can create a directory with an absolute path.
func (s *utilsSuite) TestSecureMkdirAllAbsolute(c *C) {
	c.Assert(update.SecureMkdirAllImpl("/abs/path", 0755, 123, 456), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "abs" 0755`,
		`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 3`,
		`fchown 4 123 456`,
		`mkdirat 4 "path" 0755`,
		`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 4`,
		`fchown 3 123 456`,
		`close 3`,
	})
}

// Ensure that we don't chown existing directories.
func (s *utilsSuite) TestSecureMkdirAllExistingDirsDontChown(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 4 "path" 0755`, syscall.EEXIST)
	err := update.SecureMkdirAllImpl("/abs/path", 0755, 123, 456)
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "abs" 0755`,
		`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 3`,
		`mkdirat 4 "path" 0755`,
		`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 4`,
		`close 3`,
	})
}
