// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package dmverity_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/snap/integrity/dmverity"
	"github.com/snapcore/snapd/testutil"
)

type testingGateSuite struct {
	testutil.BaseTest
}

var _ = Suite(&testingGateSuite{})

func (s *testingGateSuite) mockCmdline(c *C, cmdline string) {
	p := filepath.Join(c.MkDir(), "cmdline")
	c.Assert(os.WriteFile(p, []byte(cmdline), 0644), IsNil)
	s.AddCleanup(kcmdline.MockProcCmdline(p))
}

func (s *testingGateSuite) TestEnabledForTesting(c *C) {
	for _, tc := range []struct {
		testingBuild bool
		cmdline      string
		enabled      bool
	}{
		{false, "snapd.testing.dm-verity=1", false},
		{false, "", false},
		{true, "", false},
		{true, "panic=-1 console=ttyS0", false},
		{true, "snapd.testing.dm-verity", false},
		{true, "snapd.testing.dm-verity=0", false},
		{true, "snapd.testing.dm-verity=true", false},
		{true, "snapd.testing.dm-verity=11", false},
		{true, "snapd.testing.dm-verity.foo=1", false},
		{true, "snapd.testing.dm-verity=1", true},
		{true, "panic=-1 snapd.testing.dm-verity=1 console=ttyS0", true},
	} {
		restore := dmverity.MockTestingBuild(tc.testingBuild)
		s.mockCmdline(c, tc.cmdline)
		c.Check(dmverity.EnabledForTesting(), Equals, tc.enabled, Commentf("%+v", tc))
		restore()
	}
}

func (s *testingGateSuite) TestEnabledForTestingCannotReadCmdline(c *C) {
	logbuf, restore := logger.MockLogger()
	s.AddCleanup(restore)
	s.AddCleanup(dmverity.MockTestingBuild(true))
	s.AddCleanup(kcmdline.MockProcCmdline(filepath.Join(c.MkDir(), "missing")))

	c.Check(dmverity.EnabledForTesting(), Equals, false)
	c.Check(logbuf.String(), testutil.Contains, "cannot read kernel command line, dm-verity not enabled for testing:")
}

func (s *testingGateSuite) TestEnabledForTestingNotTestingBuildDoesNotReadCmdline(c *C) {
	logbuf, restore := logger.MockLogger()
	s.AddCleanup(restore)
	s.AddCleanup(dmverity.MockTestingBuild(false))
	s.AddCleanup(kcmdline.MockProcCmdline(filepath.Join(c.MkDir(), "missing")))

	c.Check(dmverity.EnabledForTesting(), Equals, false)
	c.Check(logbuf.String(), Equals, "")
}
