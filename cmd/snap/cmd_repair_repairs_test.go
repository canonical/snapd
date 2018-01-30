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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

func mockSnapRepair(c *C) *testutil.MockCmd {
	coreLibExecDir := filepath.Join(dirs.GlobalRootDir, dirs.CoreLibExecDir)
	err := os.MkdirAll(coreLibExecDir, 0755)
	c.Assert(err, IsNil)
	return testutil.MockCommand(c, filepath.Join(coreLibExecDir, "snap-repair"), "")
}

func (s *SnapSuite) TestSnapShowRepair(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockSnapRepair := mockSnapRepair(c)
	defer mockSnapRepair.Restore()

	_, err := snap.Parser().ParseArgs([]string{"repair", "canonical-1"})
	c.Assert(err, IsNil)
	c.Check(mockSnapRepair.Calls(), DeepEquals, [][]string{
		{"snap-repair", "show", "canonical-1"},
	})
}

func (s *SnapSuite) TestSnapListRepairs(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockSnapRepair := mockSnapRepair(c)
	defer mockSnapRepair.Restore()

	_, err := snap.Parser().ParseArgs([]string{"repairs"})
	c.Assert(err, IsNil)
	c.Check(mockSnapRepair.Calls(), DeepEquals, [][]string{
		{"snap-repair", "list"},
	})
}
