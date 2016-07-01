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
	"regexp"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/integration-tests/testutils/build"
	"github.com/snapcore/snapd/integration-tests/testutils/cli"
	"github.com/snapcore/snapd/integration-tests/testutils/common"
	"github.com/snapcore/snapd/integration-tests/testutils/data"
	"github.com/snapcore/snapd/integration-tests/testutils/wait"
	"github.com/snapcore/snapd/testutil"
)

var _ = check.Suite(&snapOpSuite{})

type snapOpSuite struct {
	common.SnappySuite
}

// TestRemoveBusyRetries is a regression test for LP:#1571721
func (s *snapOpSuite) TestRemoveBusyRetries(c *check.C) {
	// install the binaries snap
	snapPath, err := build.LocalSnap(c, data.BasicBinariesSnapName)
	defer os.Remove(snapPath)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))
	installOutput := common.InstallSnap(c, snapPath)
	c.Assert(installOutput, testutil.Contains, data.BasicBinariesSnapName)

	// run a command that keeps the mount point busy in the background
	blockerBin := fmt.Sprintf("/snap/bin/%s.block", data.BasicBinariesSnapName)
	blockerSrv := "umount-blocker"
	cli.ExecCommand(c, "sudo", "systemd-run", "--unit", blockerSrv, blockerBin)
	c.Assert(err, check.IsNil, check.Commentf("cannot start %q:%s", blockerBin, err))
	wait.ForActiveService(c, blockerSrv)

	// snap remove will block (and try to retry) while the umount-block
	// service is running. so we need to do stuff in a go-routine
	ch := make(chan int)
	go func() {
		// wait until snappy to show that it is retrying
		needle := `Doing.*Remove`
		wait.ForCommand(c, needle, "snap", "changes")

		// find change id of the remove
		pattern := `(?m)([0-9]+).*Doing.*Remove.*"`
		err := wait.ForCommand(c, pattern, "snap", "changes", data.BasicBinariesSnapName)
		c.Assert(err, check.IsNil)

		output := cli.ExecCommand(c, "snap", "changes", data.BasicBinariesSnapName)
		id := regexp.MustCompile(pattern).FindStringSubmatch(output)[1]
		needle = `will retry: `
		wait.ForCommand(c, needle, "snap", "change", id)

		// now stop the service that blocks the umount
		cli.ExecCommand(c, "sudo", "systemctl", "stop", blockerSrv)
		wait.ForInactiveService(c, blockerSrv)

		// this triggers an Ensure in the overlord
		cli.ExecCommandErr("sudo", "snap", "refresh", "ubuntu-core")
		ch <- 1
	}()

	// try to remove, this will block and eventually succeed
	output, err := cli.ExecCommandErr("sudo", "snap", "remove", data.BasicBinariesSnapName)
	c.Check(err, check.IsNil)
	c.Check(output, testutil.Contains, `will retry: `)

	// wait for the goroutine to finish so that we can do the final
	// check that we have no pending changes
	select {
	case <-ch:
	case <-time.After(5 * time.Minute):
		c.Errorf("busy retry test timed out after 5 minutes")
	}

	// ensure no changes are left in Doing state
	output = cli.ExecCommand(c, "snap", "changes")
	c.Check(output, check.Not(testutil.Contains), "Doing")
}
