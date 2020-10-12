// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package testutil_test

import (
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/check.v1"

	. "github.com/snapcore/snapd/testutil"
)

type symlinkTargetCheckerSuite struct {
	d       string
	symlink string
	target  string
}

var _ = check.Suite(&symlinkTargetCheckerSuite{})

func (s *symlinkTargetCheckerSuite) SetUpTest(c *check.C) {
	s.d = c.MkDir()
	s.symlink = filepath.Join(s.d, "symlink")
	s.target = "target"
	c.Assert(os.Symlink(s.target, s.symlink), check.IsNil)
}

func (s *symlinkTargetCheckerSuite) TestSymlinkTargetEquals(c *check.C) {
	testInfo(c, SymlinkTargetEquals, "SymlinkTargetEquals", []string{"filename", "target"})
	testCheck(c, SymlinkTargetEquals, true, "", s.symlink, s.target)

	testCheck(c, SymlinkTargetEquals, false, "Failed to match with symbolic link target:\ntarget", s.symlink, "not-target")
	testCheck(c, SymlinkTargetEquals, false, `Cannot read symbolic link: readlink missing: no such file or directory`, "missing", "")
	testCheck(c, SymlinkTargetEquals, false, "Filename must be a string", 42, "")
	testCheck(c, SymlinkTargetEquals, false, "Cannot compare symbolic link target with something of type int", s.symlink, 1)
}

func (s *symlinkTargetCheckerSuite) TestSymlinkTargetContains(c *check.C) {
	testInfo(c, SymlinkTargetContains, "SymlinkTargetContains", []string{"filename", "target"})
	testCheck(c, SymlinkTargetContains, true, "", s.symlink, s.target[1:])
	testCheck(c, SymlinkTargetContains, true, "", s.symlink, regexp.MustCompile(".*"))

	testCheck(c, SymlinkTargetContains, false, "Failed to match with symbolic link target:\ntarget", s.symlink, "not-target")
	testCheck(c, SymlinkTargetContains, false, "Failed to match with symbolic link target:\ntarget", s.symlink, regexp.MustCompile("^$"))
	testCheck(c, SymlinkTargetContains, false, `Cannot read symbolic link: readlink missing: no such file or directory`, "missing", "")
	testCheck(c, SymlinkTargetContains, false, "Filename must be a string", 42, "")
	testCheck(c, SymlinkTargetContains, false, "Cannot compare symbolic link target with something of type int", s.symlink, 1)
}

func (s *symlinkTargetCheckerSuite) TestSymlinkTargetMatches(c *check.C) {
	testInfo(c, SymlinkTargetMatches, "SymlinkTargetMatches", []string{"filename", "regex"})
	testCheck(c, SymlinkTargetMatches, true, "", s.symlink, ".*")
	testCheck(c, SymlinkTargetMatches, true, "", s.symlink, "^"+regexp.QuoteMeta(s.target)+"$")

	testCheck(c, SymlinkTargetMatches, false, "Failed to match with symbolic link target:\ntarget", s.symlink, "^$")
	testCheck(c, SymlinkTargetMatches, false, "Failed to match with symbolic link target:\ntarget", s.symlink, "123"+regexp.QuoteMeta(s.target))
	testCheck(c, SymlinkTargetMatches, false, `Cannot read symbolic link: readlink missing: no such file or directory`, "missing", "")
	testCheck(c, SymlinkTargetMatches, false, "Filename must be a string", 42, ".*")
	testCheck(c, SymlinkTargetMatches, false, "Regex must be a string", s.symlink, 1)
}
