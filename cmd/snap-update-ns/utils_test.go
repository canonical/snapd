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
	"bytes"
	"os"
	"syscall"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/testutil"
)

type utilsSuite struct {
	testutil.BaseTest
	sys *update.SyscallRecorder
	log *bytes.Buffer
}

var _ = Suite(&utilsSuite{})

func (s *utilsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.sys = &update.SyscallRecorder{}
	s.BaseTest.AddCleanup(update.MockSystemCalls(s.sys))
	buf, restore := logger.MockLogger()
	s.BaseTest.AddCleanup(restore)
	s.log = buf
}

func (s *utilsSuite) TearDownTest(c *C) {
	s.sys.CheckForStrayDescriptors(c)
	s.BaseTest.TearDownTest(c)
}

// Ensure that we refuse to create a directory with an relative path.
func (s *utilsSuite) TestSecureMkdirAllRelative(c *C) {
	err := update.SecureMkdirAll("rel/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot create directory with relative path: "rel/path"`)
	c.Assert(s.sys.Calls(), HasLen, 0)
}

// Ensure that we can "create the root directory.
func (s *utilsSuite) TestSecureMkdirAllLevel0(c *C) {
	c.Assert(update.SecureMkdirAll("/", 0755, 123, 456), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`close 3`,
	})
}

// Ensure that we can create a directory in the top-level directory.
func (s *utilsSuite) TestSecureMkdirAllLevel1(c *C) {
	os.Setenv("SNAPD_DEBUG", "1")
	defer os.Unsetenv("SNAPD_DEBUG")
	c.Assert(update.SecureMkdirAll("/path", 0755, 123, 456), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "path" 0755`,
		`openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`fchown 4 123 456`,
		`close 4`,
		`close 3`,
	})
	c.Assert(s.log.String(), testutil.Contains, `secure-mk-dir 3 ["path"] 0 -rwxr-xr-x 123 456 -> ...`)
	c.Assert(s.log.String(), testutil.Contains, `secure-mk-dir 3 ["path"] 0 -rwxr-xr-x 123 456 -> 4`)
}

// Ensure that we can create a directory two levels from the top-level directory.
func (s *utilsSuite) TestSecureMkdirAllLevel2(c *C) {
	c.Assert(update.SecureMkdirAll("/path/to", 0755, 123, 456), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "path" 0755`,
		`openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`fchown 4 123 456`,
		`close 3`,
		`mkdirat 4 "to" 0755`,
		`openat 4 "to" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`fchown 3 123 456`,
		`close 3`,
		`close 4`,
	})
}

// Ensure that we can create a directory three levels from the top-level directory.
func (s *utilsSuite) TestSecureMkdirAllLevel3(c *C) {
	c.Assert(update.SecureMkdirAll("/path/to/something", 0755, 123, 456), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "path" 0755`,
		`openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`fchown 4 123 456`,
		`mkdirat 4 "to" 0755`,
		`openat 4 "to" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 5
		`fchown 5 123 456`,
		`close 4`,
		`close 3`,
		`mkdirat 5 "something" 0755`,
		`openat 5 "something" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`fchown 3 123 456`,
		`close 3`,
		`close 5`,
	})
}

// Ensure that we don't chown existing directories.
func (s *utilsSuite) TestSecureMkdirAllExistingDirsDontChown(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 4 "path" 0755`, syscall.EEXIST)
	err := update.SecureMkdirAll("/abs/path", 0755, 123, 456)
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "abs" 0755`,
		`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`close 3`,
		`mkdirat 4 "path" 0755`,
		`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`close 3`,
		`close 4`,
	})
}

// Ensure that we we close everything when mkdirat fails.
func (s *utilsSuite) TestSecureMkdirAllMkdiratError(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, errTesting)
	err := update.SecureMkdirAll("/abs", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot mkdir path segment "abs": testing`)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "abs" 0755`,
		`close 3`,
	})
}

// Ensure that we we close everything when fchown fails.
func (s *utilsSuite) TestSecureMkdirAllFchownError(c *C) {
	s.sys.InsertFault(`fchown 4 123 456`, errTesting)
	err := update.SecureMkdirAll("/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot chown path segment "path" to 123.456 \(got up to "/"\): testing`)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "path" 0755`,
		`openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`fchown 4 123 456`,
		`close 4`,
		`close 3`,
	})
}

// Check error path when we cannot open root directory.
func (s *utilsSuite) TestSecureMkdirAllOpenRootError(c *C) {
	s.sys.InsertFault(`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := update.SecureMkdirAll("/abs/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, "cannot open root directory: testing")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> err
	})
}

// Check error path when we cannot open non-root directory.
func (s *utilsSuite) TestSecureMkdirAllOpenError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := update.SecureMkdirAll("/abs/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot open path segment "abs" \(got up to "/"\): testing`)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "abs" 0755`,
		`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> err
		`close 3`,
	})
}
