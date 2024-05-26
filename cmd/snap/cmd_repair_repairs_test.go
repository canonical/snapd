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
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

func mockSnapRepair(c *C) *testutil.MockCmd {
	return testutil.MockCommand(c, filepath.Join(dirs.GlobalRootDir, dirs.CoreLibExecDir, "snap-repair"), "")
}

func (s *SnapSuite) TestSnapShowRepair(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockSnapRepair := mockSnapRepair(c)
	defer mockSnapRepair.Restore()

	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"repair", "canonical-1"}))

	c.Check(mockSnapRepair.Calls(), DeepEquals, [][]string{
		{"snap-repair", "show", "canonical-1"},
	})
}

func (s *SnapSuite) TestSnapShowRepairNoArgs(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"repair"}))
	c.Assert(err, ErrorMatches, "no <repair-id> given. Try 'snap repairs' to list all repairs or specify a specific repair id.")
}

func (s *SnapSuite) TestSnapListRepairs(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	mockSnapRepair := mockSnapRepair(c)
	defer mockSnapRepair.Restore()

	_ := mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"repairs"}))

	c.Check(mockSnapRepair.Calls(), DeepEquals, [][]string{
		{"snap-repair", "list"},
	})
}
