// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package syscheck_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/dirs/dirstest"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/syscheck"
	"github.com/snapcore/snapd/testutil"
)

type dirsSuite struct {
	testutil.BaseTest
}

var _ = Suite(&dirsSuite{})

func (s *dirsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.AddCleanup(release.MockReleaseInfo(&release.OS{
		ID: "funkylunix",
	}))
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *dirsSuite) TestHappy(c *C) {
	dir := c.MkDir()

	dirstest.MustMockCanonicalSnapMountDir(dir)
	dirs.SetRootDir(dir)
	c.Check(dirs.SnapMountDirDetectionOutcome(), IsNil)

	c.Check(syscheck.CheckSnapMountDir(), IsNil)
}

func (s *dirsSuite) TestUndetermined(c *C) {
	d := c.MkDir()
	// pretend we have a relative symlink
	c.Assert(os.Symlink("foo/bar", filepath.Join(d, "/snap")), IsNil)
	dirs.SetRootDir(d)
	c.Logf("dir: %v", dirs.SnapMountDir)

	c.Check(dirs.SnapMountDirDetectionOutcome(), NotNil)

	c.Check(syscheck.CheckSnapMountDir(), ErrorMatches, "cannot resolve snap mount directory: .*")
}
