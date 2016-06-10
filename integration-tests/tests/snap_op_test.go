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
	"regexp"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/integration-tests/testutils/build"
	"github.com/snapcore/snapd/integration-tests/testutils/cli"
	"github.com/snapcore/snapd/integration-tests/testutils/common"
	"github.com/snapcore/snapd/integration-tests/testutils/data"
	"github.com/snapcore/snapd/integration-tests/testutils/wait"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/testutil"
)

var _ = check.Suite(&snapOpSuite{})

type snapOpSuite struct {
	common.SnappySuite
}

func (s *snapOpSuite) testInstallRemove(c *check.C, snapName, displayName string) {
	installOutput := common.InstallSnap(c, snapName)
	expected := "(?ms)" +
		"Name +Version +Rev +Developer +Notes\n" +
		".*" +
		displayName + " +.*\n" +
		".*"
	c.Assert(installOutput, check.Matches, expected)

	removeOutput := common.RemoveSnap(c, snapName)
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
	common.InstallSnap(c, snapPath)
	installOutput := common.InstallSnap(c, snapPath)
	c.Assert(installOutput, testutil.Contains, data.BasicSnapName)
	// double check, sideloaded snaps have revnos like xNN
	revnos, _ := filepath.Glob(filepath.Join(dirs.SnapSnapsDir, data.BasicSnapName, "x*"))
	c.Check(len(revnos) >= 2, check.Equals, true)

	removeOutput := common.RemoveSnap(c, data.BasicSnapName)
	c.Assert(removeOutput, check.Not(testutil.Contains), data.BasicSnapName)
	// gone from disk
	revnos, err = filepath.Glob(filepath.Join(dirs.SnapSnapsDir, data.BasicSnapName, "1*"))
	c.Assert(err, check.IsNil)
	c.Check(revnos, check.HasLen, 0)
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
		output := cli.ExecCommand(c, "snap", "changes", data.BasicBinariesSnapName)
		id := regexp.MustCompile(`(?m)([0-9]+).*Doing.*Remove.*"`).FindStringSubmatch(output)[1]
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

func (s *snapOpSuite) TestInstallFailedIsUndone(c *check.C) {
	// make snap uninstallable
	snapName := "hello-world"
	subdirPath := filepath.Join("/snap", snapName, "current", "foo")
	_, err := cli.ExecCommandErr("sudo", "mkdir", "-p", subdirPath)
	c.Assert(err, check.IsNil)
	defer cli.ExecCommand(c, "sudo", "rm", "-rf", filepath.Dir(subdirPath))

	// try to install snap and see it fail
	_, err = cli.ExecCommandErr("sudo", "snap", "install", snapName)
	c.Assert(err, check.NotNil)

	// check undone and error in tasks
	output := cli.ExecCommand(c, "snap", "changes", snapName)
	expected := fmt.Sprintf(`(?ms).*\n(\d+) +Error.*Install "%s" snap\n$`, snapName)
	id := regexp.MustCompile(expected).FindStringSubmatch(output)[1]

	output = cli.ExecCommand(c, "snap", "change", id)

	type undoneCheckerFunc func(*check.C, string, string)
	for _, fn := range []undoneCheckerFunc{
		checkDownloadUndone,
		checkMountUndone,
		checkDataCopyUndone,
		checkSecProfilesSetupUndone} {
		fn(c, snapName, output)
	}
}

func checkDownloadUndone(c *check.C, snapName, output string) {
	expected := fmt.Sprintf(`(?ms).*Undone +.*Download snap %q from channel.*`, snapName)
	c.Assert(output, check.Matches, expected)
}

func checkMountUndone(c *check.C, snapName, output string) {
	expected := fmt.Sprintf(`(?ms).*Undone +.*Mount snap %q\n.*`, snapName)
	c.Assert(output, check.Matches, expected)

	// MountDir is removed /snap/<name>/<revision>
	checkEmptyGlob(c, filepath.Join(dirs.SnapSnapsDir, snapName, "[0-9]+"))

	// MountFile is removed /var/lib/snapd/snaps/<name>_<revision>.snap
	checkEmptyGlob(c, filepath.Join(dirs.SnapBlobDir,
		fmt.Sprintf("%s_%s.snap", snapName, "[0-9]+")))
}

func checkDataCopyUndone(c *check.C, snapName, output string) {
	expected := fmt.Sprintf(`(?ms).*Undone +.*Copy snap %q data\n.*`, snapName)
	c.Assert(output, check.Matches, expected)

	// DataHomeDir is removed /home/*/snap/<name>/<revision>
	checkEmptyGlob(c, filepath.Join(dirs.SnapDataHomeGlob, snapName, "[0-9]+"))

	// DataDir is removed /var/snap/<name>/<revision>
	checkEmptyGlob(c, filepath.Join(dirs.SnapDataDir, snapName, "[0-9]+"))
}

func checkSecProfilesSetupUndone(c *check.C, snapName, output string) {
	expected := fmt.Sprintf(`(?ms).*Undone +.*Setup snap %q security profiles\n.*`, snapName)
	c.Assert(output, check.Matches, expected)

	// security artifacts are removed for each backend: apparmor, seccomp, dbus, udev
	backends := map[string]string{
		dirs.SnapAppArmorDir:  interfaces.SecurityTagGlob(snapName),
		dirs.SnapSeccompDir:   interfaces.SecurityTagGlob(snapName),
		dirs.SnapBusPolicyDir: fmt.Sprintf("%s.conf", interfaces.SecurityTagGlob(snapName)),
		dirs.SnapUdevRulesDir: fmt.Sprintf("70-%s.rules", interfaces.SecurityTagGlob(snapName)),
	}
	for dir, glob := range backends {
		checkEmptyGlob(c, filepath.Join(dir, glob))
	}
}

func checkEmptyGlob(c *check.C, pattern string) {
	items, err := filepath.Glob(pattern)
	c.Assert(err, check.IsNil)
	c.Assert(items, check.IsNil)
}
