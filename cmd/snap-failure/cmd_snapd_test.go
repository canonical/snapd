// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	failure "github.com/snapcore/snapd/cmd/snap-failure"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

func (r *failureSuite) TestRun(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)
	c.Check(r.systemctlCmd.Calls(), HasLen, 0)
}

func writeSeqFile(c *C, name string, current snap.Revision, seq []*snap.SideInfo) {
	seqPath := filepath.Join(dirs.SnapSeqDir, name+".json")

	err := os.MkdirAll(dirs.SnapSeqDir, 0o755)
	c.Assert(err, IsNil)

	b, err := json.Marshal(&struct {
		Sequence []*snap.SideInfo `json:"sequence"`
		Current  string           `json:"current"`
	}{
		Sequence: seq,
		Current:  current.String(),
	})
	c.Assert(err, IsNil)

	err = os.WriteFile(seqPath, b, 0o644)
	c.Assert(err, IsNil)
}

func (r *failureSuite) TestCallPrevSnapdFromSnap(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	writeSeqFile(c, "snapd", snap.R(123), []*snap.SideInfo{
		{Revision: snap.R(99)},
		{Revision: snap.R(100)},
		{Revision: snap.R(123)},
	})

	mockScript := `
set -eu

[ -L '%[1]s/snapd/current' ]
[ "$(readlink '%[1]s/snapd/current')" = 100 ]
`
	// mock snapd command from 'previous' revision
	systemdRunCmd := testutil.MockCommand(c, "systemd-run", fmt.Sprintf(mockScript, dirs.SnapMountDir))
	defer systemdRunCmd.Restore()

	err := os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd"), 0o755)
	c.Assert(err, IsNil)

	os.Args = []string{"snap-failure", "snapd"}
	err = failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(systemdRunCmd.Calls(), DeepEquals, [][]string{
		{"systemd-run", "--collect", "--wait", "--property=KeyringMode=shared", "--setenv=SNAPD_REVERT_TO_REV=100", "--setenv=SNAPD_DEBUG=1", "--", filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd")},
	})
	c.Check(r.systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
		{"systemctl", "is-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "reset-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "restart", "snapd.socket", "snapd.service"},
	})
}

func (r *failureSuite) TestCallPrevSnapdFromSnapRestartSnapdFallback(c *C) {
	defer failure.MockWaitTimes(1*time.Millisecond, 1*time.Millisecond)()

	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	writeSeqFile(c, "snapd", snap.R(123), []*snap.SideInfo{
		{Revision: snap.R(99)},
		{Revision: snap.R(100)},
		{Revision: snap.R(123)},
	})

	// mock snapd command from 'previous' revision
	systemdRunCmd := testutil.MockCommand(c, "systemd-run", `true`)
	defer systemdRunCmd.Restore()

	err := os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd"), 0o755)
	c.Assert(err, IsNil)

	mockCmdStateDir := c.MkDir()

	systemctlCmd := testutil.MockCommand(c, "systemctl", fmt.Sprintf(`
statedir="%s"
if [ "$1 $2 $3" = "restart snapd.socket snapd.service" ] && [ ! -f "$statedir/stamp" ]; then
    touch "$statedir/stamp"
    exit 1
fi
`, mockCmdStateDir))
	defer systemctlCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err = failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(systemdRunCmd.Calls(), DeepEquals, [][]string{
		{"systemd-run", "--collect", "--wait", "--property=KeyringMode=shared", "--setenv=SNAPD_REVERT_TO_REV=100", "--setenv=SNAPD_DEBUG=1", "--", filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd")},
	})
	c.Check(systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
		{"systemctl", "is-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "reset-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "restart", "snapd.socket", "snapd.service"},
		{"systemctl", "restart", "snapd.socket", "snapd.service"},
	})
}

func (r *failureSuite) TestCallPrevSnapdFromSnapBackToFullyActive(c *C) {
	defer failure.MockWaitTimes(1*time.Millisecond, 0)()

	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	writeSeqFile(c, "snapd", snap.R(123), []*snap.SideInfo{
		{Revision: snap.R(99)},
		{Revision: snap.R(100)},
		{Revision: snap.R(123)},
	})

	// mock snapd command from 'previous' revision
	systemdRunCmd := testutil.MockCommand(c, "systemd-run", `true`)
	defer systemdRunCmd.Restore()

	err := os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd"), 0o755)
	c.Assert(err, IsNil)

	systemctlCmd := testutil.MockCommand(c, "systemctl", `
if [ "$1" = is-failed ] ; then
    exit 1
fi
`)
	defer systemctlCmd.Restore()

	// mock the sockets re-appearing
	err = os.MkdirAll(filepath.Dir(dirs.SnapdSocket), 0o755)
	c.Assert(err, IsNil)
	err = os.WriteFile(dirs.SnapdSocket, nil, 0o755)
	c.Assert(err, IsNil)
	err = os.WriteFile(dirs.SnapSocket, nil, 0o755)
	c.Assert(err, IsNil)

	os.Args = []string{"snap-failure", "snapd"}
	err = failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(systemdRunCmd.Calls(), DeepEquals, [][]string{
		{"systemd-run", "--collect", "--wait", "--property=KeyringMode=shared", "--setenv=SNAPD_REVERT_TO_REV=100", "--setenv=SNAPD_DEBUG=1", "--", filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd")},
	})
	c.Check(systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
		{"systemctl", "is-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "is-active", "snapd.socket", "snapd.service"},
	})
}

func (r *failureSuite) TestCallPrevSnapdFromSnapBackActiveNoSockets(c *C) {
	defer failure.MockWaitTimes(1*time.Millisecond, 0)()

	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	writeSeqFile(c, "snapd", snap.R(123), []*snap.SideInfo{
		{Revision: snap.R(99)},
		{Revision: snap.R(100)},
		{Revision: snap.R(123)},
	})

	// mock snapd command from 'previous' revision
	systemdRunCmd := testutil.MockCommand(c, "systemd-run", `true`)
	defer systemdRunCmd.Restore()

	err := os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd"), 0o755)
	c.Assert(err, IsNil)

	systemctlCmd := testutil.MockCommand(c, "systemctl", `
if [ "$1" = is-failed ] ; then
    exit 1
fi
`)
	defer systemctlCmd.Restore()

	// no sockets

	os.Args = []string{"snap-failure", "snapd"}
	err = failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(systemdRunCmd.Calls(), DeepEquals, [][]string{
		{"systemd-run", "--collect", "--wait", "--property=KeyringMode=shared", "--setenv=SNAPD_REVERT_TO_REV=100", "--setenv=SNAPD_DEBUG=1", "--", filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd")},
	})
	c.Check(systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
		{"systemctl", "is-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "is-active", "snapd.socket", "snapd.service"},
		{"systemctl", "is-active", "snapd.socket", "snapd.service"},
		{"systemctl", "is-active", "snapd.socket", "snapd.service"},
		{"systemctl", "is-active", "snapd.socket", "snapd.service"},
		{"systemctl", "is-active", "snapd.socket", "snapd.service"},
		{"systemctl", "reset-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "restart", "snapd.socket", "snapd.service"},
	})
}

func (r *failureSuite) TestCallPrevSnapdFromCore(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// only one entry in sequence
	writeSeqFile(c, "snapd", snap.R(123), []*snap.SideInfo{
		{Revision: snap.R(123)},
	})

	systemdRunCmd := testutil.MockCommand(c, "systemd-run", `true`)
	defer systemdRunCmd.Restore()

	err := os.MkdirAll(filepath.Join(dirs.SnapMountDir, "core", "current", "/usr/lib/snapd"), 0o755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(dirs.SnapMountDir, "core", "current", "/usr/lib/snapd/snapd"), []byte{}, 0o755)
	c.Assert(err, IsNil)

	os.Args = []string{"snap-failure", "snapd"}
	err = failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(systemdRunCmd.Calls(), DeepEquals, [][]string{
		{"systemd-run", "--collect", "--wait", "--property=KeyringMode=shared", "--setenv=SNAPD_REVERT_TO_REV=0", "--setenv=SNAPD_DEBUG=1", "--", filepath.Join(dirs.SnapMountDir, "core", "current", "/usr/lib/snapd/snapd")},
	})
	c.Check(r.systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
		{"systemctl", "is-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "reset-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "restart", "snapd.socket", "snapd.service"},
	})
}

func (r *failureSuite) TestCallPrevSnapdFromSnapdWhenNoCore(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// only one entry in sequence
	writeSeqFile(c, "snapd", snap.R(123), []*snap.SideInfo{
		{Revision: snap.R(123)},
	})

	// validity
	// mock snapd in the core snap
	systemdRunCmd := testutil.MockCommand(c, "systemd-run", `true`)
	defer systemdRunCmd.Restore()

	err := os.MkdirAll(filepath.Join(dirs.SnapMountDir, "core", "current", "/usr/lib/snapd"), 0o755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(dirs.SnapMountDir, "core", "current", "/usr/lib/snapd/snapd"), []byte{}, 0o755)
	c.Assert(err, IsNil)

	os.Args = []string{"snap-failure", "snapd"}
	err = failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(systemdRunCmd.Calls(), DeepEquals, [][]string{
		{"systemd-run", "--collect", "--wait", "--property=KeyringMode=shared", "--setenv=SNAPD_REVERT_TO_REV=0", "--setenv=SNAPD_DEBUG=1", "--", filepath.Join(dirs.SnapMountDir, "core", "current", "/usr/lib/snapd/snapd")},
	})
	c.Check(r.systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
		{"systemctl", "is-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "reset-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "restart", "snapd.socket", "snapd.service"},
	})
}

type distroVersionDrivenTestCase struct {
	releaseInfo    *release.OS
	classic        bool
	callsFromSnapd bool
	hasCore        bool
}

func (r *failureSuite) testCallPrevSnapdWhenDistroInfoDriven(c *C, tc distroVersionDrivenTestCase) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	defer release.MockReleaseInfo(tc.releaseInfo)()
	// TODO release should update that internally when mocking release info
	defer release.MockOnClassic(tc.classic)()

	// only one entry in sequence
	writeSeqFile(c, "snapd", snap.R(123), []*snap.SideInfo{
		{Revision: snap.R(123)},
	})

	if tc.hasCore {
		err := os.MkdirAll(filepath.Join(dirs.SnapMountDir, "core", "current", "/usr/lib/snapd"), 0o755)
		c.Assert(err, IsNil)
		err = os.WriteFile(filepath.Join(dirs.SnapMountDir, "core", "current", "/usr/lib/snapd/snapd"), []byte{}, 0o755)
		c.Assert(err, IsNil)
	}
	err := os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd", "current", "/usr/lib/snapd"), 0o755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(dirs.SnapMountDir, "snapd", "current", "/usr/lib/snapd/snapd"), []byte{}, 0o755)
	c.Assert(err, IsNil)

	var snippetSnapdSnapd, snippetCoreSnapd string
	if tc.callsFromSnapd {
		snippetSnapdSnapd, snippetCoreSnapd = `true`, `exit 1`
	} else {
		snippetSnapdSnapd, snippetCoreSnapd = `exit 1`, `true`
	}

	snippet := fmt.Sprintf(`
case "$7" in
*/snapd/current/usr/lib/snapd/snapd)
  %[1]s
;;
*/core/current/usr/lib/snapd/snapd)
  %[2]s
;;
*)
  exit 1
;;
esac
`, snippetSnapdSnapd, snippetCoreSnapd)

	systemdRun := testutil.MockCommand(c, "systemd-run", snippet)

	os.Args = []string{"snap-failure", "snapd"}
	err = failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)

	if tc.callsFromSnapd {
		c.Check(systemdRun.Calls(), DeepEquals, [][]string{
			{"systemd-run", "--collect", "--wait", "--property=KeyringMode=shared", "--setenv=SNAPD_REVERT_TO_REV=0", "--setenv=SNAPD_DEBUG=1", "--", filepath.Join(dirs.SnapMountDir, "snapd", "current", "/usr/lib/snapd/snapd")},
		})
	} else {
		if tc.hasCore {
			c.Check(systemdRun.Calls(), DeepEquals, [][]string{
				{"systemd-run", "--collect", "--wait", "--property=KeyringMode=shared", "--setenv=SNAPD_REVERT_TO_REV=0", "--setenv=SNAPD_DEBUG=1", "--", filepath.Join(dirs.SnapMountDir, "core", "current", "/usr/lib/snapd/snapd")},
			})
		} else {
			c.Check(systemdRun.Calls(), DeepEquals, [][]string{})
		}
	}

	c.Check(r.systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
		{"systemctl", "is-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "reset-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "restart", "snapd.socket", "snapd.service"},
	})
}

func (r *failureSuite) TestCallPrevSnapdWithSnapdUC20(c *C) {
	r.testCallPrevSnapdWhenDistroInfoDriven(c, distroVersionDrivenTestCase{
		releaseInfo:    &release.OS{ID: "ubuntu-core", VersionID: "20"},
		classic:        false,
		callsFromSnapd: true,
		hasCore:        true,
	})
}

func (r *failureSuite) TestCallPrevSnapdWithSnapdUC18(c *C) {
	r.testCallPrevSnapdWhenDistroInfoDriven(c, distroVersionDrivenTestCase{
		releaseInfo:    &release.OS{ID: "ubuntu-core", VersionID: "18"},
		classic:        false,
		callsFromSnapd: true,
		hasCore:        true,
	})
}

func (r *failureSuite) TestCallPrevSnapdWithCoreUC16(c *C) {
	r.testCallPrevSnapdWhenDistroInfoDriven(c, distroVersionDrivenTestCase{
		releaseInfo:    &release.OS{ID: "ubuntu-core", VersionID: "16"},
		classic:        false,
		callsFromSnapd: false, // calls snapd from core as it's a UC16 system
		hasCore:        true,
	})
}

func (r *failureSuite) TestCallPrevSnapdWithCoreClassic(c *C) {
	r.testCallPrevSnapdWhenDistroInfoDriven(c, distroVersionDrivenTestCase{
		releaseInfo:    &release.OS{ID: "ubuntu", VersionID: "24.04"},
		classic:        true,
		hasCore:        true,
		callsFromSnapd: false, // classic is allowed to fall back to core
	})
}

func (r *failureSuite) TestCallPrevSnapdWithSnapdClassic(c *C) {
	r.testCallPrevSnapdWhenDistroInfoDriven(c, distroVersionDrivenTestCase{
		releaseInfo:    &release.OS{ID: "ubuntu", VersionID: "24.04"},
		classic:        true,
		hasCore:        false,
		callsFromSnapd: true, // no core, so classic stays with the snapd snap
	})
}

func (r *failureSuite) TestCallPrevSnapdFail(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	writeSeqFile(c, "snapd", snap.R(123), []*snap.SideInfo{
		{Revision: snap.R(100)},
		{Revision: snap.R(123)},
	})

	// mock snapd in the core snap
	systemdRunCmd := testutil.MockCommand(c, "systemd-run", `exit 2`)
	defer systemdRunCmd.Restore()

	err := os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd"), 0o755)
	c.Assert(err, IsNil)

	os.Args = []string{"snap-failure", "snapd"}
	err = failure.Run()
	c.Check(err, ErrorMatches, "snapd failed: exit status 2")
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(systemdRunCmd.Calls(), DeepEquals, [][]string{
		{"systemd-run", "--collect", "--wait", "--property=KeyringMode=shared", "--setenv=SNAPD_REVERT_TO_REV=100", "--setenv=SNAPD_DEBUG=1", "--", filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd")},
	})
	c.Check(r.systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
	})
}

func (r *failureSuite) TestGarbageSeq(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	seqPath := filepath.Join(dirs.SnapSeqDir, "snapd.json")
	err := os.MkdirAll(dirs.SnapSeqDir, 0o755)
	c.Assert(err, IsNil)

	err = os.WriteFile(seqPath, []byte("this is garbage"), 0o644)
	c.Assert(err, IsNil)

	systemdRunCmd := testutil.MockCommand(c, "systemd-run", `exit 99`)
	defer systemdRunCmd.Restore()

	systemctlCmd := testutil.MockCommand(c, "systemctl", "exit 98")
	defer systemctlCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err = failure.Run()
	c.Check(err, ErrorMatches, `cannot parse "snapd.json" sequence file: invalid .*`)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(systemdRunCmd.Calls(), HasLen, 0)
	c.Check(systemctlCmd.Calls(), HasLen, 0)
}

func (r *failureSuite) TestBadSeq(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	writeSeqFile(c, "snapd", snap.R(123), []*snap.SideInfo{
		{Revision: snap.R(100)},
		// current not in sequence
	})

	systemdRunCmd := testutil.MockCommand(c, "systemd-run", `true`)
	defer systemdRunCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, ErrorMatches, "internal error: current 123 not found in sequence: .*Revision:100.*")
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(systemdRunCmd.Calls(), HasLen, 0)
	c.Check(r.systemctlCmd.Calls(), HasLen, 0)
}

func (r *failureSuite) TestStickySnapdSocket(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	writeSeqFile(c, "snapd", snap.R(123), []*snap.SideInfo{
		{Revision: snap.R(100)},
		{Revision: snap.R(123)},
	})

	err := os.MkdirAll(filepath.Dir(dirs.SnapdSocket), 0o755)
	c.Assert(err, IsNil)
	err = os.WriteFile(dirs.SnapdSocket, []byte{}, 0o755)
	c.Assert(err, IsNil)

	// mock snapd in the core snap
	systemdRunCmd := testutil.MockCommand(c, "systemd-run", `true`)
	defer systemdRunCmd.Restore()

	err = os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd"), 0o755)
	c.Assert(err, IsNil)

	os.Args = []string{"snap-failure", "snapd"}
	err = failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(systemdRunCmd.Calls(), DeepEquals, [][]string{
		{"systemd-run", "--collect", "--wait", "--property=KeyringMode=shared", "--setenv=SNAPD_REVERT_TO_REV=100", "--setenv=SNAPD_DEBUG=1", "--", filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd")},
	})
	c.Check(r.systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
		{"systemctl", "is-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "reset-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "restart", "snapd.socket", "snapd.service"},
	})

	// make sure the socket file was deleted
	c.Assert(osutil.FileExists(dirs.SnapdSocket), Equals, false)
}

func (r *failureSuite) testNoReexec(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	writeSeqFile(c, "snapd", snap.R(100), []*snap.SideInfo{
		{Revision: snap.R(99)},
		{Revision: snap.R(100)},
	})

	// mock snapd command from 'previous' revision
	systemdRunCmd := testutil.MockCommand(c, "systemd-run", "exit 1")
	defer systemdRunCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, IsNil)

	c.Check(systemdRunCmd.Calls(), HasLen, 0)
	c.Check(r.systemctlCmd.Calls(), HasLen, 0)
	c.Check(r.log.String(), testutil.Contains, "re-exec unsupported or disabled")
}

func (r *failureSuite) TestReexecDisabled(c *C) {
	os.Setenv("SNAP_REEXEC", "0")
	defer os.Unsetenv("SNAP_REEXEC")
	r.testNoReexec(c)

}

func (r *failureSuite) TestReexecUnsupported(c *C) {
	r.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "fedora"}))
	dirs.SetRootDir(r.rootdir)
	r.testNoReexec(c)
}
