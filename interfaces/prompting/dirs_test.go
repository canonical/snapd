// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package prompting_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/testutil"
)

type promptingDirsSuite struct {
	testutil.BaseTest
}

var _ = Suite(&promptingDirsSuite{})

func (s *promptingDirsSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *promptingDirsSuite) TestStateDir(c *C) {
	c.Check(prompting.StateDir(), Equals, filepath.Join(dirs.GlobalRootDir, "var/lib/snapd/interfaces-requests"))

	_, err := os.Stat(prompting.StateDir())
	c.Assert(err, ErrorMatches, "stat .*var/lib/snapd/interfaces-requests: no such file or directory")
	err = prompting.EnsureStateDir()
	c.Assert(err, IsNil)

	fi, err := os.Stat(prompting.StateDir())
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
}

func (s *promptingDirsSuite) TestEnsureStateDirError(c *C) {
	_, err := os.Stat(prompting.StateDir())
	c.Assert(err, ErrorMatches, "stat .*var/lib/snapd/interfaces-requests: no such file or directory")

	parent := filepath.Dir(prompting.StateDir())
	c.Assert(os.MkdirAll(parent, 0o755), IsNil)
	f, err := os.Create(prompting.StateDir())
	c.Assert(err, IsNil)
	f.Close()

	err = prompting.EnsureStateDir()
	c.Assert(err, ErrorMatches, "cannot create interfaces requests state directory.*")
}
