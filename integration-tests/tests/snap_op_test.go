// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015, 2016 Canonical Ltd
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

package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/build"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/data"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/wait"
	"github.com/ubuntu-core/snappy/testutil"
)

var _ = check.Suite(&snapOpSuite{})

type snapOpSuite struct {
	common.SnappySuite
}

func (s *snapOpSuite) testInstallRemove(c *check.C, snapName, displayName string) {
	installOutput := installSnap(c, snapName)
	expected := "(?ms)" +
		"Name +Version +Developer\n" +
		".*" +
		displayName + " +.*\n" +
		".*"
	c.Assert(installOutput, check.Matches, expected)

	removeOutput := removeSnap(c, snapName)
	c.Assert(removeOutput, check.Not(testutil.Contains), snapName)
}

func (s *snapOpSuite) TestInstallRemoveAliasWorks(c *check.C) {
	s.testInstallRemove(c, "hello-world", "hello-world")
}

func (s *snapOpSuite) TestRemoveRemovesAllRevisions(c *check.C) {
	snapPath, err := build.LocalSnap(c, data.BasicSnapName)
	defer os.Remove(snapPath)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))

	// install two revisions
	installSnap(c, snapPath)
	installOutput := installSnap(c, snapPath)
	c.Assert(installOutput, testutil.Contains, data.BasicSnapName)
	// double check, sideloaded snaps have revnos like 1000xx
	revnos, _ := filepath.Glob(filepath.Join(dirs.SnapSnapsDir, data.BasicSnapName, "1*"))
	c.Check(len(revnos) >= 2, check.Equals, true)

	removeOutput := removeSnap(c, data.BasicSnapName)
	c.Assert(removeOutput, check.Not(testutil.Contains), data.BasicSnapName)
	// gone from disk
	revnos, err = filepath.Glob(filepath.Join(dirs.SnapSnapsDir, data.BasicSnapName, "1*"))
	c.Assert(err, check.IsNil)
	c.Check(revnos, check.HasLen, 0)
}

// TestRemoveBusyRetries is a regression test for LP:#1571721
func (s *snapOpSuite) TestRemoveBusyRetries(c *check.C) {
	snapPath, err := build.LocalSnap(c, data.BasicBinariesSnapName)
	defer os.Remove(snapPath)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))

	installOutput := installSnap(c, snapPath)
	c.Assert(installOutput, testutil.Contains, data.BasicBinariesSnapName)

	// run a snap that keeps the mount point busy in the background
	blockerBin := fmt.Sprintf("/snap/bin/%s.block", data.BasicBinariesSnapName)
	blockerSrv := "umount-blocker"
	cli.ExecCommand(c, "sudo", "systemd-run", "--unit", blockerSrv, blockerBin)
	c.Assert(err, check.IsNil, check.Commentf("cannot start %q:%s", blockerBin, err))
	wait.ForActiveService(c, blockerSrv)

	// stop the blocking binary again after some milliseconds
	ch := make(chan int)
	go func() {
		// wait a wee bit so that unmount fails
		time.Sleep(500 * time.Millisecond)

		// and stop the service that blocks again
		cli.ExecCommand(c, "sudo", "systemctl", "stop", blockerSrv)
		wait.ForInactiveService(c, blockerSrv)
		// this triggers an Ensure in the overlord
		cli.ExecCommandErr("sudo", "snap", "refresh", "ubuntu-core")
		ch <- 1
	}()

	// try to remove, this will eventually succeed
	_, err = cli.ExecCommandErr("sudo", "snap", "remove", data.BasicBinariesSnapName)
	c.Check(err, check.IsNil)
	<-ch
}
