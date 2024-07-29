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

	err := os.MkdirAll(dirs.SnapSeqDir, 0755)
	c.Assert(err, IsNil)

	b, err := json.Marshal(&struct {
		Sequence []*snap.SideInfo `json:"sequence"`
		Current  string           `json:"current"`
	}{
		Sequence: seq,
		Current:  current.String(),
	})
	c.Assert(err, IsNil)

	err = os.WriteFile(seqPath, b, 0644)
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
[ "${SNAPD_REVERT_TO_REV}" = 100 ]
`
	// mock snapd command from 'previous' revision
	snapdCmd := testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd"), fmt.Sprintf(mockScript, dirs.SnapMountDir))
	defer snapdCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(snapdCmd.Calls(), DeepEquals, [][]string{
		{"snapd"},
	})
	c.Check(r.systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
		{"systemctl", "is-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "reset-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "restart", "snapd.socket"},
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
	snapdCmd := testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd"),
		`test "$SNAPD_REVERT_TO_REV" = "100"`)
	defer snapdCmd.Restore()

	systemctlCmd := testutil.MockCommand(c, "systemctl", `
if [ "$1" = restart ] && [ "$2" == snapd.socket ] ; then
    exit 1
fi
`)
	defer systemctlCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(snapdCmd.Calls(), DeepEquals, [][]string{
		{"snapd"},
	})
	c.Check(systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
		{"systemctl", "is-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "reset-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "restart", "snapd.socket"},
		{"systemctl", "restart", "snapd.service"},
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
	snapdCmd := testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd"),
		`test "$SNAPD_REVERT_TO_REV" = "100"`)
	defer snapdCmd.Restore()

	systemctlCmd := testutil.MockCommand(c, "systemctl", `
if [ "$1" = is-failed ] ; then
    exit 1
fi
`)
	defer systemctlCmd.Restore()

	// mock the sockets re-appearing
	err := os.MkdirAll(filepath.Dir(dirs.SnapdSocket), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(dirs.SnapdSocket, nil, 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(dirs.SnapSocket, nil, 0755)
	c.Assert(err, IsNil)

	os.Args = []string{"snap-failure", "snapd"}
	err = failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(snapdCmd.Calls(), DeepEquals, [][]string{
		{"snapd"},
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
	snapdCmd := testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd"),
		`test "$SNAPD_REVERT_TO_REV" = "100"`)
	defer snapdCmd.Restore()

	systemctlCmd := testutil.MockCommand(c, "systemctl", `
if [ "$1" = is-failed ] ; then
    exit 1
fi
`)
	defer systemctlCmd.Restore()

	// no sockets

	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(snapdCmd.Calls(), DeepEquals, [][]string{
		{"snapd"},
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
		{"systemctl", "restart", "snapd.socket"},
	})
}

func (r *failureSuite) TestCallPrevSnapdFromCore(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// only one entry in sequence
	writeSeqFile(c, "snapd", snap.R(123), []*snap.SideInfo{
		{Revision: snap.R(123)},
	})

	// mock snapd in the core snap
	snapdCmd := testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "core", "current", "/usr/lib/snapd/snapd"),
		`test "$SNAPD_REVERT_TO_REV" = "0"`)
	defer snapdCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(snapdCmd.Calls(), DeepEquals, [][]string{
		{"snapd"},
	})
	c.Check(r.systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
		{"systemctl", "is-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "reset-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "restart", "snapd.socket"},
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
	c.Assert(filepath.Join(dirs.SnapMountDir, "core", "current", "/usr/lib/snapd/snapd"), testutil.FileAbsent)
	// mock snapd in the core snap
	snapdCmd := testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "snapd", "current", "/usr/lib/snapd/snapd"),
		`test "$SNAPD_REVERT_TO_REV" = "0"`)
	defer snapdCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(snapdCmd.Calls(), DeepEquals, [][]string{
		{"snapd"},
	})
	c.Check(r.systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
		{"systemctl", "is-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "reset-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "restart", "snapd.socket"},
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

	var snippetSnapdSnapd, snippetCoreSnapd string
	if tc.callsFromSnapd {
		snippetSnapdSnapd, snippetCoreSnapd = `test "$SNAPD_REVERT_TO_REV" = "0"`, `exit 1`
	} else {
		snippetSnapdSnapd, snippetCoreSnapd = `exit 1`, `test "$SNAPD_REVERT_TO_REV" = "0"`
	}

	var snapdInCore, snapdInSnapd *testutil.MockCmd
	// mock snapd in the core snap
	if tc.hasCore {
		snapdInCore = testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "core", "current", "/usr/lib/snapd/snapd"),
			snippetCoreSnapd)
		defer snapdInCore.Restore()
	}

	snapdInSnapd = testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "snapd", "current", "/usr/lib/snapd/snapd"),
		snippetSnapdSnapd)
	defer snapdInSnapd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)

	if tc.callsFromSnapd {
		c.Check(snapdInSnapd.Calls(), DeepEquals, [][]string{
			{"snapd"},
		})
		if tc.hasCore {
			c.Check(snapdInCore.Calls(), HasLen, 0)
		}
	} else {
		c.Check(snapdInSnapd.Calls(), HasLen, 0)
		if tc.hasCore {
			c.Check(snapdInCore.Calls(), DeepEquals, [][]string{
				{"snapd"},
			})
		}
	}

	c.Check(r.systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
		{"systemctl", "is-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "reset-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "restart", "snapd.socket"},
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
	snapdCmd := testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd"),
		`exit 2`)
	defer snapdCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, ErrorMatches, "snapd failed: exit status 2")
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(snapdCmd.Calls(), DeepEquals, [][]string{
		{"snapd"},
	})
	c.Check(r.systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
	})
}

func (r *failureSuite) TestGarbageSeq(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	seqPath := filepath.Join(dirs.SnapSeqDir, "snapd.json")
	err := os.MkdirAll(dirs.SnapSeqDir, 0755)
	c.Assert(err, IsNil)

	err = os.WriteFile(seqPath, []byte("this is garbage"), 0644)
	c.Assert(err, IsNil)

	snapdCmd := testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd"),
		`exit 99`)
	defer snapdCmd.Restore()

	systemctlCmd := testutil.MockCommand(c, "systemctl", "exit 98")
	defer systemctlCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err = failure.Run()
	c.Check(err, ErrorMatches, `cannot parse "snapd.json" sequence file: invalid .*`)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(snapdCmd.Calls(), HasLen, 0)
	c.Check(systemctlCmd.Calls(), HasLen, 0)
}

func (r *failureSuite) TestBadSeq(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	writeSeqFile(c, "snapd", snap.R(123), []*snap.SideInfo{
		{Revision: snap.R(100)},
		// current not in sequence
	})

	snapdCmd := testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd"), "")
	defer snapdCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, ErrorMatches, "internal error: current 123 not found in sequence: .*Revision:100.*")
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(snapdCmd.Calls(), HasLen, 0)
	c.Check(r.systemctlCmd.Calls(), HasLen, 0)
}

func (r *failureSuite) TestSnapdOutputPassthrough(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	writeSeqFile(c, "snapd", snap.R(123), []*snap.SideInfo{
		{Revision: snap.R(100)},
		{Revision: snap.R(123)},
	})

	snapdCmd := testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd"), `
echo 'stderr: hello from snapd' >&2
echo 'stdout: hello from snapd'
exit 123
`)
	defer snapdCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, ErrorMatches, "snapd failed: exit status 123")
	c.Check(r.Stderr(), Equals, "stderr: hello from snapd\n")
	c.Check(r.Stdout(), Equals, "stdout: hello from snapd\n")

	c.Check(snapdCmd.Calls(), HasLen, 1)
	c.Check(r.systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
	})
}

func (r *failureSuite) TestStickySnapdSocket(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	writeSeqFile(c, "snapd", snap.R(123), []*snap.SideInfo{
		{Revision: snap.R(100)},
		{Revision: snap.R(123)},
	})

	err := os.MkdirAll(filepath.Dir(dirs.SnapdSocket), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(dirs.SnapdSocket, []byte{}, 0755)
	c.Assert(err, IsNil)

	// mock snapd in the core snap
	snapdCmd := testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "snapd", "100", "/usr/lib/snapd/snapd"),
		`test "$SNAPD_REVERT_TO_REV" = "100"`)
	defer snapdCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err = failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stderr(), HasLen, 0)

	c.Check(snapdCmd.Calls(), DeepEquals, [][]string{
		{"snapd"},
	})
	c.Check(r.systemctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "stop", "snapd.socket"},
		{"systemctl", "is-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "reset-failed", "snapd.socket", "snapd.service"},
		{"systemctl", "restart", "snapd.socket"},
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
	snapdCmd := testutil.MockCommand(c, filepath.Join(dirs.SnapMountDir, "snapd", "99", "/usr/lib/snapd/snapd"), "exit 1")
	defer snapdCmd.Restore()

	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, IsNil)

	c.Check(snapdCmd.Calls(), HasLen, 0)
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
