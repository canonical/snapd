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

package backend_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapshotstate/backend"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/systemd/systemdtest"
	"github.com/snapcore/snapd/testutil"
)

// makeMountUnitFile creates a persistent mount unit file under the root dir for the
// given mount point path and returns the unit name (base filename).
func makeMountUnitFile(c *C, mountPoint string) string {
	stripped := dirs.StripRootDir(mountPoint)
	escaped := systemd.EscapeUnitNamePath(stripped)
	c.Assert(os.MkdirAll(dirs.SnapServicesDir, 0755), IsNil)
	unitName := escaped + ".mount"
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapServicesDir, unitName), []byte("[Unit]\n"), 0644), IsNil)
	return unitName
}

// mountInfoLine returns a single mountinfo line for a bind mount at mountPoint.
func mountInfoLine(mountPoint string) string {
	return fmt.Sprintf("100 1 8:1 / %s rw,relatime - tmpfs tmpfs rw\n", mountPoint)
}

// mountInfoLines returns one mountinfo line per mount point, with unique IDs.
func mountInfoLines(mountPoints ...string) string {
	var sb strings.Builder
	for i, mp := range mountPoints {
		fmt.Fprintf(&sb, "%d 1 8:1 / %s rw,relatime - tmpfs tmpfs rw\n", 100+i, mp)
	}
	return sb.String()
}

// saveAndOpenSnapshot creates a snapshot of "hello-snap" rev 42 (whose data
// dirs are created by snapshotSuite.SetUpTest) and opens it for reading.
func saveAndOpenSnapshot(c *C) *backend.Reader {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root (runuser will fail)")
	}
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "hello-snap",
			Revision: snap.R(42),
			SnapID:   "hello-id",
		},
		Version: "v1.33",
	}
	shw, err := backend.Save(context.TODO(), 1, info, nil, []string{"snapuser"}, nil, nil)
	c.Assert(err, IsNil)
	shr, err := backend.Open(backend.Filename(shw), backend.ExtractFnameSetID)
	c.Assert(err, IsNil)
	return shr
}

func (s *snapshotSuite) TestIsPathAtOrUnderDir(c *C) {
	for _, tc := range []struct {
		path, dir string
		expected  bool
		comment   string
	}{
		{"/foo/bar", "/foo/bar", true, "exact match"},
		{"/foo/bar/baz", "/foo/bar", true, "direct child"},
		{"/foo/bar/baz/qux", "/foo/bar", true, "deep child"},
		{"/foo/bar/baz/", "/foo/bar", true, "trailing slash on path"},
		{"/foo/bar/baz", "/foo/bar/", true, "trailing slash on dir"},
		{"/foo/bar/baz/", "/foo/bar/", true, "trailing slash on both"},
		{"/foo/bar/", "/foo/bar", true, "exact match, trailing slash on path"},
		{"/foo/bar", "/foo/bar/", true, "exact match, trailing slash on dir"},
		{"/foo/bar-extra", "/foo/bar", false, "sibling with common prefix"},
		{"/foo", "/foo/bar", false, "parent of dir"},
		{"/other", "/foo/bar", false, "unrelated path"},
		{"/foo/bar/../../other", "/foo/bar", false, "path escapes dir via .."},
		{"/foo//bar/baz", "/foo/bar", true, "double slash in path"},
		{"/foo/bar/baz", "foo/bar", false, "relative dir"},
	} {
		c.Check(backend.IsPathAtOrUnderDir(tc.path, tc.dir), Equals, tc.expected,
			Commentf(tc.comment))
	}
}

func (s *snapshotSuite) TestRestoreNoMountsAtDst(c *C) {
	// No active mounts under the snap data dirs.
	// Restore succeeds with no Stop or Start calls.
	shr := saveAndOpenSnapshot(c)
	defer shr.Close()

	rs, err := shr.Restore(context.TODO(), snap.R(0), nil, logger.Debugf, nil)
	c.Assert(err, IsNil)
	rs.Cleanup()

	// one ListMountUnits call while processing each of {system common, system rev, user common, user rev}
	expListMountUnitsParams := systemdtest.ParamsForListMountUnits{SnapName: "hello-snap", Origin: "mount-control", Filter: systemd.LoadedMountUnits}
	c.Check(s.sysd.ListMountUnitsCalls, DeepEquals, []systemdtest.ParamsForListMountUnits{
		expListMountUnitsParams, expListMountUnitsParams, expListMountUnitsParams, expListMountUnitsParams,
	})
	c.Check(s.sysd.StopCalls, HasLen, 0)
	c.Check(s.sysd.StartCalls, HasLen, 0)
}

func (s *snapshotSuite) TestRestoreStopsAndRestartsSnapctlMounts(c *C) {
	// A snapctl (mount-control) mount lives under the snap revision data dir.
	// Restore must Stop it before renaming and Start it afterwards.
	si := snap.MinimalPlaceInfo("hello-snap", snap.R(42))
	mountPoint := filepath.Join(si.DataDir(), "target")
	unitName := makeMountUnitFile(c, mountPoint)

	s.sysd.ListMountUnitsResult = systemdtest.ResultForListMountUnits{
		MountPoints: []string{mountPoint},
	}
	defer osutil.MockMountInfo(mountInfoLine(mountPoint))()

	shr := saveAndOpenSnapshot(c)
	defer shr.Close()

	rs, err := shr.Restore(context.TODO(), snap.R(0), nil, logger.Debugf, nil)
	c.Assert(err, IsNil)
	rs.Cleanup()

	// one ListMountUnits call while processing each of {system common, system rev, user common, user rev}
	expListMountUnitsParams := systemdtest.ParamsForListMountUnits{SnapName: "hello-snap", Origin: "mount-control", Filter: systemd.LoadedMountUnits}
	c.Check(s.sysd.ListMountUnitsCalls, DeepEquals, []systemdtest.ParamsForListMountUnits{
		expListMountUnitsParams, expListMountUnitsParams, expListMountUnitsParams, expListMountUnitsParams,
	})
	c.Assert(s.sysd.StopCalls, HasLen, 1)
	c.Check(s.sysd.StopCalls[0], DeepEquals, []string{unitName})
	c.Assert(s.sysd.StartCalls, HasLen, 1)
	c.Check(s.sysd.StartCalls[0], DeepEquals, []string{unitName})
}

func (s *snapshotSuite) TestRestoreNonSnapctlMountReturnsError(c *C) {
	// A non-snapctl mount under the snap revision data dir.
	// Restore returns an error, no Stop called.
	si := snap.MinimalPlaceInfo("hello-snap", snap.R(42))
	mountPoint := filepath.Join(si.DataDir(), "user-target")

	// sysd reports no mount-control mounts (ListMountUnitsResult empty default)
	defer osutil.MockMountInfo(mountInfoLine(mountPoint))()

	shr := saveAndOpenSnapshot(c)
	defer shr.Close()

	_, err := shr.Restore(context.TODO(), snap.R(0), nil, logger.Debugf, nil)
	c.Assert(err, ErrorMatches, `.*cannot move data with unknown mount.*`)

	// ListMountUnits called at least once
	c.Assert(s.sysd.ListMountUnitsCalls, Not(HasLen), 0)
	c.Check(s.sysd.ListMountUnitsCalls[0], DeepEquals, systemdtest.ParamsForListMountUnits{SnapName: "hello-snap", Origin: "mount-control", Filter: systemd.LoadedMountUnits})
	c.Check(s.sysd.StopCalls, HasLen, 0)
	c.Check(s.sysd.StartCalls, HasLen, 0)
}

func (s *snapshotSuite) TestRestoreListMountErrorReturnsError(c *C) {
	// ListMountUnits returns an error.
	// Restore propagates it.
	s.sysd.ListMountUnitsResult = systemdtest.ResultForListMountUnits{
		Err: errors.New("mock ListMountUnits error"),
	}

	shr := saveAndOpenSnapshot(c)
	defer shr.Close()

	_, err := shr.Restore(context.TODO(), snap.R(0), nil, logger.Debugf, nil)
	c.Assert(err, ErrorMatches, `.*cannot list mounts.*mock ListMountUnits error.*`)

	c.Assert(s.sysd.ListMountUnitsCalls, HasLen, 1)
	c.Check(s.sysd.ListMountUnitsCalls[0], DeepEquals, systemdtest.ParamsForListMountUnits{SnapName: "hello-snap", Origin: "mount-control", Filter: systemd.LoadedMountUnits})
	c.Check(s.sysd.StopCalls, HasLen, 0)
	c.Check(s.sysd.StartCalls, HasLen, 0)
}

func (s *snapshotSuite) TestRestoreStopFailureReturnsError(c *C) {
	// Stop returns an error.
	// Restore propagates it and Start is not called since no
	// units were successfully stopped.
	si := snap.MinimalPlaceInfo("hello-snap", snap.R(42))
	mountPoint := filepath.Join(si.DataDir(), "target")
	makeMountUnitFile(c, mountPoint)

	s.sysd.ListMountUnitsResult = systemdtest.ResultForListMountUnits{
		MountPoints: []string{mountPoint},
	}
	defer osutil.MockMountInfo(mountInfoLine(mountPoint))()
	s.sysd.StopResult = errors.New("systemd stop failed")

	shr := saveAndOpenSnapshot(c)
	defer shr.Close()

	_, err := shr.Restore(context.TODO(), snap.R(0), nil, logger.Debugf, nil)
	c.Assert(err, ErrorMatches, `.*cannot stop mount unit.*systemd stop failed.*`)

	c.Assert(s.sysd.ListMountUnitsCalls, Not(HasLen), 0)
	c.Check(s.sysd.ListMountUnitsCalls[0], DeepEquals, systemdtest.ParamsForListMountUnits{SnapName: "hello-snap", Origin: "mount-control", Filter: systemd.LoadedMountUnits})
	c.Check(s.sysd.StopCalls, HasLen, 1)
	c.Check(s.sysd.StartCalls, HasLen, 0)
}

func (s *snapshotSuite) TestRestoreRestartFailureIsLoggedNotReturned(c *C) {
	// Start fails after successful Stop.
	// Restore still returns nil (restart failure is best-effort, only logged).
	si := snap.MinimalPlaceInfo("hello-snap", snap.R(42))
	mountPoint := filepath.Join(si.DataDir(), "target")
	unitName := makeMountUnitFile(c, mountPoint)

	s.sysd.ListMountUnitsResult = systemdtest.ResultForListMountUnits{
		MountPoints: []string{mountPoint},
	}
	s.sysd.StartResult = errors.New("systemd start failed")
	defer osutil.MockMountInfo(mountInfoLine(mountPoint))()

	logbuf, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	shr := saveAndOpenSnapshot(c)
	defer shr.Close()

	rs, err := shr.Restore(context.TODO(), snap.R(0), nil, logger.Debugf, nil)
	c.Assert(err, IsNil)
	rs.Cleanup()

	// one ListMountUnits call while processing each of {system common, system rev, user common, user rev}
	expListMountUnitsParams := systemdtest.ParamsForListMountUnits{SnapName: "hello-snap", Origin: "mount-control", Filter: systemd.LoadedMountUnits}
	c.Check(s.sysd.ListMountUnitsCalls, DeepEquals, []systemdtest.ParamsForListMountUnits{
		expListMountUnitsParams, expListMountUnitsParams, expListMountUnitsParams, expListMountUnitsParams,
	})
	c.Assert(s.sysd.StopCalls, HasLen, 1)
	c.Check(s.sysd.StopCalls[0], DeepEquals, []string{unitName})
	c.Assert(s.sysd.StartCalls, HasLen, 1)
	c.Check(s.sysd.StartCalls[0], DeepEquals, []string{unitName})
	c.Check(logbuf.String(), testutil.Contains, `cannot restart mount unit`)
	c.Check(logbuf.String(), testutil.Contains, `systemd start failed`)
}

func (s *snapshotSuite) TestRevertSkipsMountHandlingWhenSnapEmpty(c *C) {
	// rs.Snap == "" (older snapd persisted state).
	// No ListMountUnits call.
	dir := c.MkDir()
	rs := &backend.RestoreState{
		Snap:    "",
		Created: []string{dir},
	}
	rs.Revert()

	c.Check(s.sysd.ListMountUnitsCalls, HasLen, 0)
	// dir should have been removed
	_, err := os.Stat(dir)
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *snapshotSuite) TestRevertStopsAndRestartsSnapctlMounts(c *C) {
	// rs.Created contains a dir with a snapctl mount under it.
	// Stop before RemoveAll, Start after Revert returns.
	createdDir := filepath.Join(s.root, "var/snap/mysnap/x1")
	mountPoint := filepath.Join(createdDir, "target")
	c.Assert(os.MkdirAll(createdDir, 0755), IsNil)
	unitName := makeMountUnitFile(c, mountPoint)

	s.sysd.ListMountUnitsResult = systemdtest.ResultForListMountUnits{
		MountPoints: []string{mountPoint},
	}
	defer osutil.MockMountInfo(mountInfoLine(mountPoint))()

	// Also set up a Moved entry so Revert renames it back
	movedDir := createdDir + ".~aBcDeFgHi~"
	c.Assert(os.MkdirAll(movedDir, 0755), IsNil)

	rs := &backend.RestoreState{
		Snap:    "mysnap",
		Created: []string{createdDir},
		Moved:   []string{movedDir},
	}
	rs.Revert()

	expListMountUnitsParams := systemdtest.ParamsForListMountUnits{SnapName: "mysnap", Origin: "mount-control", Filter: systemd.LoadedMountUnits}
	c.Assert(s.sysd.ListMountUnitsCalls, HasLen, 1)
	c.Check(s.sysd.ListMountUnitsCalls[0], DeepEquals, expListMountUnitsParams)

	c.Assert(s.sysd.StopCalls, HasLen, 1)
	c.Check(s.sysd.StopCalls[0], DeepEquals, []string{unitName})
	c.Assert(s.sysd.StartCalls, HasLen, 1)
	c.Check(s.sysd.StartCalls[0], DeepEquals, []string{unitName})
	// createdDir was removed but the movedDir should have been renamed to createdDir
	_, err := os.Stat(createdDir)
	c.Check(err, IsNil)
	// movedDir (the backup) should be deleted
	_, err = os.Stat(movedDir)
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *snapshotSuite) TestRevertLogsAndContinuesOnNonSnapctlMounts(c *C) {
	// Non-snapctl mount under the first Created dir; second dir is clean.
	// Logged for dir1, no Stop called, Revert continues to dir2 and removes it
	// (verifies the loop is not aborted on a non-snapctl mount).
	dir1 := filepath.Join(s.root, "var/snap/mysnap/x1")
	dir2 := filepath.Join(s.root, "var/snap/mysnap/x2")
	mountPoint := filepath.Join(dir1, "user-target")
	c.Assert(os.MkdirAll(dir1, 0755), IsNil)
	c.Assert(os.MkdirAll(dir2, 0755), IsNil)

	// sysd reports no mount-control mounts (ListMountUnitsResult empty default);
	// the non-snapctl mount is only under dir1.
	defer osutil.MockMountInfo(mountInfoLine(mountPoint))()

	logbuf, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	rs := &backend.RestoreState{
		Snap:    "mysnap",
		Created: []string{dir1, dir2},
	}
	rs.Revert()

	// Both dirs must have been visited (loop continued past dir1).
	expListMountUnitsParams := systemdtest.ParamsForListMountUnits{SnapName: "mysnap", Origin: "mount-control", Filter: systemd.LoadedMountUnits}
	c.Assert(s.sysd.ListMountUnitsCalls, HasLen, 2)
	c.Check(s.sysd.ListMountUnitsCalls[0], DeepEquals, expListMountUnitsParams)
	c.Check(s.sysd.ListMountUnitsCalls[1], DeepEquals, expListMountUnitsParams)
	c.Check(s.sysd.StopCalls, HasLen, 0)
	c.Check(s.sysd.StartCalls, HasLen, 0)
	c.Check(logbuf.String(), testutil.Contains, `cannot remove data with unknown mount`)
	c.Check(logbuf.String(), testutil.Contains, dir1)
	// dir1 was skipped (non-snapctl mount present), dir2 was removed.
	_, err := os.Stat(dir1)
	c.Check(err, IsNil)
	_, err = os.Stat(dir2)
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *snapshotSuite) TestRevertLogsAndContinuesOnStopFailure(c *C) {
	// Stop fails for a snapctl mount under the first Created dir; second dir is clean.
	// Logged for dir1, no Start called, Revert continues to dir2 and removes it
	// (verifies the loop is not aborted on a stop failure).
	dir1 := filepath.Join(s.root, "var/snap/mysnap/x1")
	dir2 := filepath.Join(s.root, "var/snap/mysnap/x2")
	mountPoint := filepath.Join(dir1, "target")
	c.Assert(os.MkdirAll(dir1, 0755), IsNil)
	c.Assert(os.MkdirAll(dir2, 0755), IsNil)
	unitName := makeMountUnitFile(c, mountPoint)

	// The snapctl mount is only under dir1; dir2 has no mounts.
	s.sysd.ListMountUnitsResult = systemdtest.ResultForListMountUnits{
		MountPoints: []string{mountPoint},
	}
	s.sysd.StopResult = errors.New("systemd stop failed")
	defer osutil.MockMountInfo(mountInfoLine(mountPoint))()

	logbuf, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	rs := &backend.RestoreState{
		Snap:    "mysnap",
		Created: []string{dir1, dir2},
	}
	rs.Revert()

	// Both dirs must have been visited (loop continued past dir1).
	expListMountUnitsParams := systemdtest.ParamsForListMountUnits{SnapName: "mysnap", Origin: "mount-control", Filter: systemd.LoadedMountUnits}
	c.Assert(s.sysd.ListMountUnitsCalls, HasLen, 2)
	c.Check(s.sysd.ListMountUnitsCalls[0], DeepEquals, expListMountUnitsParams)
	c.Check(s.sysd.ListMountUnitsCalls[1], DeepEquals, expListMountUnitsParams)
	// Stop attempted only for dir1's mount; dir2 had no mounts.
	c.Check(s.sysd.StopCalls, HasLen, 1)
	c.Check(s.sysd.StopCalls[0], DeepEquals, []string{unitName})
	c.Check(s.sysd.StartCalls, HasLen, 0)
	c.Check(logbuf.String(), testutil.Contains, `cannot stop mount unit`)
	c.Check(logbuf.String(), testutil.Contains, `systemd stop failed`)
	c.Check(logbuf.String(), testutil.Contains, dir1)
	// dir1 was skipped (stop failed), dir2 was removed.
	_, err := os.Stat(dir1)
	c.Check(err, IsNil)
	_, err = os.Stat(dir2)
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *snapshotSuite) TestRevertLogsAndContinuesOnStartFailure(c *C) {
	// Stop succeeds but Start fails for a snapctl mount under a Created dir.
	// Logged, Revert continues, dir removed (best-effort restart).
	createdDir := filepath.Join(s.root, "var/snap/mysnap/x1")
	mountPoint := filepath.Join(createdDir, "target")
	c.Assert(os.MkdirAll(createdDir, 0755), IsNil)
	unitName := makeMountUnitFile(c, mountPoint)

	s.sysd.ListMountUnitsResult = systemdtest.ResultForListMountUnits{
		MountPoints: []string{mountPoint},
	}
	s.sysd.StartResult = errors.New("systemd start failed")
	defer osutil.MockMountInfo(mountInfoLine(mountPoint))()

	logbuf, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	rs := &backend.RestoreState{
		Snap:    "mysnap",
		Created: []string{createdDir},
	}
	rs.Revert()

	expListMountUnitsParams := systemdtest.ParamsForListMountUnits{SnapName: "mysnap", Origin: "mount-control", Filter: systemd.LoadedMountUnits}
	c.Assert(s.sysd.ListMountUnitsCalls, HasLen, 1)
	c.Check(s.sysd.ListMountUnitsCalls[0], DeepEquals, expListMountUnitsParams)
	c.Assert(s.sysd.StopCalls, HasLen, 1)
	c.Check(s.sysd.StopCalls[0], DeepEquals, []string{unitName})
	c.Assert(s.sysd.StartCalls, HasLen, 1)
	c.Check(s.sysd.StartCalls[0], DeepEquals, []string{unitName})
	c.Check(logbuf.String(), testutil.Contains, `cannot restart mount unit`)
	c.Check(logbuf.String(), testutil.Contains, `systemd start failed`)
	// dir was removed
	_, err := os.Stat(createdDir)
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *snapshotSuite) TestRevertLogsAndContinuesOnListMountError(c *C) {
	// ListMountUnits error on both Created dirs.
	// Logged for each, no Stop called, Revert continues to the second dir
	// after skipping the first (verifies the loop is not aborted on error).
	dir1 := filepath.Join(s.root, "var/snap/mysnap/x1")
	dir2 := filepath.Join(s.root, "var/snap/mysnap/x2")
	c.Assert(os.MkdirAll(dir1, 0755), IsNil)
	c.Assert(os.MkdirAll(dir2, 0755), IsNil)

	s.sysd.ListMountUnitsResult = systemdtest.ResultForListMountUnits{
		Err: errors.New("mock ListMountUnits error"),
	}

	logbuf, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	rs := &backend.RestoreState{
		Snap:    "mysnap",
		Created: []string{dir1, dir2},
	}
	rs.Revert()

	// Both dirs must have been visited (loop continued past the first error).
	expListMountUnitsParams := systemdtest.ParamsForListMountUnits{SnapName: "mysnap", Origin: "mount-control", Filter: systemd.LoadedMountUnits}
	c.Assert(s.sysd.ListMountUnitsCalls, HasLen, 2)
	c.Check(s.sysd.ListMountUnitsCalls[0], DeepEquals, expListMountUnitsParams)
	c.Check(s.sysd.ListMountUnitsCalls[1], DeepEquals, expListMountUnitsParams)
	c.Check(s.sysd.StopCalls, HasLen, 0)
	// Error logged for both dirs.
	c.Check(logbuf.String(), testutil.Contains, `cannot list mounts`)
	c.Check(logbuf.String(), testutil.Contains, `mock ListMountUnits error`)
	c.Check(logbuf.String(), testutil.Contains, dir1)
	c.Check(logbuf.String(), testutil.Contains, dir2)
	// Neither dir was removed because we couldn't determine their mounts.
	_, err := os.Stat(dir1)
	c.Check(err, IsNil)
	_, err = os.Stat(dir2)
	c.Check(err, IsNil)
}

func (s *snapshotSuite) TestRevertRestartsEachDirsMountsIndependently(c *C) {
	// Two Created dirs each with their own snapctl mount. Start fails for both.
	// Without the loop-variable fix each deferred closure would capture the
	// last value of `dir`, so the log would mention only dir2; with the fix
	// both dirs are mentioned.
	dir1 := filepath.Join(s.root, "var/snap/mysnap/x1")
	mp1 := filepath.Join(dir1, "target1")
	c.Assert(os.MkdirAll(dir1, 0755), IsNil)
	makeMountUnitFile(c, mp1)

	dir2 := filepath.Join(s.root, "var/snap/mysnap/x2")
	mp2 := filepath.Join(dir2, "target2")
	c.Assert(os.MkdirAll(dir2, 0755), IsNil)
	makeMountUnitFile(c, mp2)

	s.sysd.ListMountUnitsResult = systemdtest.ResultForListMountUnits{
		MountPoints: []string{mp1, mp2},
	}
	s.sysd.StartResult = errors.New("systemd start failed")
	defer osutil.MockMountInfo(mountInfoLines(mp1, mp2))()

	logbuf, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	rs := &backend.RestoreState{
		Snap:    "mysnap",
		Created: []string{dir1, dir2},
	}
	rs.Revert()

	// Each dir contributes one stop and one (attempted) start.
	c.Assert(s.sysd.StopCalls, HasLen, 2)
	c.Assert(s.sysd.StartCalls, HasLen, 2)

	// The error log must mention both dirs, not just the last one.
	c.Check(logbuf.String(), testutil.Contains, dir1)
	c.Check(logbuf.String(), testutil.Contains, dir2)
}
