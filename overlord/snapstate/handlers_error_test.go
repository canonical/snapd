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

package snapstate_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

// taskErrorSuite is a base suite to test task errors using real backend implementations
type taskErrorSuite struct {
	testutil.BaseTest

	rootDir string
	state   *state.State
	runner  *state.TaskRunner
	se      *overlord.StateEngine
	snapmgr *snapstate.SnapManager
}

var _ = Suite(&taskErrorSuite{})

func (s *taskErrorSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })
	s.rootDir = dirs.GlobalRootDir

	s.state = state.New(nil)
	s.runner = state.NewTaskRunner(s.state)
	s.runner.AddHandler("error-trigger", func(task *state.Task, tomb *tomb.Tomb) error {
		return errors.New("mock error-trigger error")
	}, nil)

	var err error
	s.snapmgr, err = snapstate.Manager(s.state, s.runner)
	c.Assert(err, IsNil)
	snapstate.SetStoreCacheCleanNext(s.snapmgr, time.Now().Add(time.Hour))

	s.se = overlord.NewStateEngine(s.state)
	s.se.AddManager(s.snapmgr)
	s.se.AddManager(s.runner)
	c.Assert(s.se.StartUp(), IsNil)
}

// killSnapAppsErrorSuite tests the "kill-snap-apps" task using real backend
// implementations. Each test simulates a different error mode and verifies
// that on failure, both the snap state and root filesystem are restored to
// their pre-task state (or have acceptable differences).
type killSnapAppsErrorSuite struct {
	taskErrorSuite
}

var _ = Suite(&killSnapAppsErrorSuite{})

func (s *killSnapAppsErrorSuite) TestDoKillSnapAppsErrorNoTaskSnapSetup(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapName := "some-snap"
	si := &snap.SideInfo{
		RealName: snapName,
		Revision: snap.R(1),
		SnapID:   fmt.Sprintf("%s-id", snapName),
	}
	snapstBefore := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		Active:   true,
	}
	snapstate.Set(s.state, snapName, snapstBefore)

	task := s.state.NewTask("kill-snap-apps", "")
	chg := s.state.NewChange("test", "")
	chg.AddTask(task)

	fsBefore, err := testutil.CreateFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	fsAfter, err := testutil.CreateFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(task.Log(), HasLen, 1)
	c.Check(task.Log()[0], Matches, `.* ERROR no state entry for key "snap-setup-task"`)

	// Verify that the snap state is unchanged
	snapstAfter := &snapstate.SnapState{}
	snapstate.Get(s.state, snapName, snapstAfter)
	c.Check(snapstAfter, DeepEquals, snapstBefore)

	// Verify that the fs snapshot is unchanged
	c.Check(fsAfter, testutil.FsSnapshotsEqual, fsBefore, nil)
}

func (s *killSnapAppsErrorSuite) TestDoKillSnapAppsErrorSnapOpenLock(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapName := "some-snap"
	si := &snap.SideInfo{
		RealName: snapName,
		Revision: snap.R(1),
		SnapID:   fmt.Sprintf("%s-id", snapName),
	}
	snapstBefore := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		Active:   true,
	}
	snapstate.Set(s.state, snapName, snapstBefore)

	// Mock snaplock.OpenLock failure by making the lock directory unwritable
	err := os.MkdirAll(dirs.SnapRunLockDir, 0700)
	c.Assert(err, IsNil)
	os.Chmod(dirs.SnapRunLockDir, 0500)
	defer os.Chmod(dirs.SnapRunLockDir, 0700)

	task := s.state.NewTask("kill-snap-apps", "")
	task.Set("snap-setup", &snapstate.SnapSetup{SideInfo: si})
	chg := s.state.NewChange("test", "")
	chg.AddTask(task)

	fsBefore, err := testutil.CreateFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	fsAfter, err := testutil.CreateFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(task.Log(), HasLen, 1)
	c.Check(task.Log()[0], Matches, fmt.Sprintf(`.* ERROR open .*/run/snapd/lock/%s.lock: permission denied`, snapName))

	// Verify that the snap state is unchanged
	snapstAfter := &snapstate.SnapState{}
	snapstate.Get(s.state, snapName, snapstAfter)
	c.Check(snapstAfter, DeepEquals, snapstBefore)

	// Verify that the fs snapshot is unchanged
	c.Check(fsAfter, testutil.FsSnapshotsEqual, fsBefore, nil)
}

func (s *killSnapAppsErrorSuite) TestDoKillSnapAppsErrorKillReasonUnmarshal(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapName := "some-snap"
	si := &snap.SideInfo{
		RealName: snapName,
		Revision: snap.R(1),
		SnapID:   fmt.Sprintf("%s-id", snapName),
	}
	snapstBefore := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		Active:   true,
	}
	snapstate.Set(s.state, snapName, snapstBefore)

	task := s.state.NewTask("kill-snap-apps", "")
	// Mock error for task.Get("kill-reason") by setting an invalid value
	// that cannot be unmarshaled into snap.AppKillReason (which is a string type)
	task.Set("kill-reason", 42)
	task.Set("snap-setup", &snapstate.SnapSetup{SideInfo: si})
	chg := s.state.NewChange("test", "")
	chg.AddTask(task)

	fsBefore, err := testutil.CreateFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	fsAfter, err := testutil.CreateFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(task.Log(), HasLen, 1)
	c.Check(task.Log()[0], Matches, `.* ERROR .*could not unmarshal state entry "kill-reason".*`)

	// Verify that the snap state is unchanged
	snapstAfter := &snapstate.SnapState{}
	snapstate.Get(s.state, snapName, snapstAfter)
	c.Check(snapstAfter, DeepEquals, snapstBefore)

	// Verify that the fs snapshot has acceptable differences
	fsIgnore := &testutil.FsSnapshotIgnoreDiff{
		// lock file is intentionally not removed on error
		fmt.Sprintf("run/snapd/lock/%s.lock", snapName): {
			Kinds:         []testutil.FsDiffKind{testutil.PresenceDiffKind},
			IgnoreParents: true,
		},
	}
	c.Check(fsAfter, testutil.FsSnapshotsEqual, fsBefore, fsIgnore)
}

func (s *killSnapAppsErrorSuite) TestDoKillSnapAppsErrorInhibitLockWithHint(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapName := "some-snap"
	si := &snap.SideInfo{
		RealName: snapName,
		Revision: snap.R(1),
		SnapID:   fmt.Sprintf("%s-id", snapName),
	}
	snapstBefore := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		Active:   true,
	}
	snapstate.Set(s.state, snapName, snapstBefore)

	// Mock runinhibit.LockWithHint failure by making the InhibitDir unwritable
	err := os.MkdirAll(runinhibit.InhibitDir, 0755)
	c.Assert(err, IsNil)
	os.Chmod(runinhibit.InhibitDir, 0555)
	defer os.Chmod(runinhibit.InhibitDir, 0755)

	task := s.state.NewTask("kill-snap-apps", "")
	task.Set("kill-reason", snap.KillReasonRemove)
	task.Set("snap-setup", &snapstate.SnapSetup{SideInfo: si})
	chg := s.state.NewChange("test", "")
	chg.AddTask(task)

	fsBefore, err := testutil.CreateFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	fsAfter, err := testutil.CreateFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(task.Log(), HasLen, 1)
	c.Check(task.Log()[0], Matches, fmt.Sprintf(`.* ERROR open .*/var/lib/snapd/inhibit/%s.lock: permission denied`, snapName))

	// Verify that the snap state is unchanged
	snapstAfter := &snapstate.SnapState{}
	snapstate.Get(s.state, snapName, snapstAfter)
	c.Check(snapstAfter, DeepEquals, snapstBefore)

	// Verify that the fs snapshot has acceptable differences
	fsIgnore := &testutil.FsSnapshotIgnoreDiff{
		// lock file is intentionally not removed on error
		fmt.Sprintf("run/snapd/lock/%s.lock", snapName): {
			Kinds:         []testutil.FsDiffKind{testutil.PresenceDiffKind},
			IgnoreParents: true,
		},
	}
	c.Check(fsAfter, testutil.FsSnapshotsEqual, fsBefore, fsIgnore)
}

func (s *killSnapAppsErrorSuite) TestDoKillSnapAppsErrorBackendKillSnapApps(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapName := "some-snap"
	si := &snap.SideInfo{
		RealName: snapName,
		Revision: snap.R(1),
		SnapID:   fmt.Sprintf("%s-id", snapName),
	}
	snapstBefore := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		Active:   true,
	}
	snapstate.Set(s.state, snapName, snapstBefore)

	// setup cgroup
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()
	cgDir := fmt.Sprintf("sys/fs/cgroup/user.slice/user-1001.slice/user@1001.service/app.slice/snap.%s.app-1.1234.scope", snapName)
	cgKillFile := filepath.Join(s.rootDir, cgDir, "cgroup.kill")
	c.Assert(os.MkdirAll(filepath.Dir(cgKillFile), 0755), IsNil)
	c.Assert(os.WriteFile(cgKillFile, []byte{}, 0644), IsNil)

	// Mock error in snapst.CurrentInfo
	restore = snapstate.MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		c.Assert(name, Equals, snapName)
		return nil, fmt.Errorf("mock SnapReadInfo error")
	})
	defer restore()

	task := s.state.NewTask("kill-snap-apps", "")
	task.Set("kill-reason", snap.KillReasonRemove)
	task.Set("snap-setup", &snapstate.SnapSetup{SideInfo: si})
	chg := s.state.NewChange("test", "")
	chg.AddTask(task)

	fsBefore, err := testutil.CreateFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	fsAfter, err := testutil.CreateFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(task.Log(), HasLen, 1)
	c.Check(task.Log()[0], Matches, `.* ERROR mock SnapReadInfo error`)

	// Verify that the snap state is unchanged
	snapstAfter := &snapstate.SnapState{}
	snapstate.Get(s.state, snapName, snapstAfter)
	c.Check(snapstAfter, DeepEquals, snapstBefore)

	// Verify that the fs snapshot has acceptable differences
	fsIgnore := &testutil.FsSnapshotIgnoreDiff{
		// lock files are intentionally not removed on error
		fmt.Sprintf("run/snapd/lock/%s.lock", snapName): {
			Kinds:         []testutil.FsDiffKind{testutil.PresenceDiffKind},
			IgnoreParents: true,
		},
		fmt.Sprintf("var/lib/snapd/inhibit/%s.lock", snapName): {
			Kinds:         []testutil.FsDiffKind{testutil.PresenceDiffKind},
			IgnoreParents: true,
		},
		// cgroup.kill file is expected to be modified
		fmt.Sprintf("%s/cgroup.kill", cgDir): {
			Kinds: []testutil.FsDiffKind{testutil.SizeDiffKind, testutil.ContentDiffKind},
		},
	}
	c.Check(fsAfter, testutil.FsSnapshotsEqual, fsBefore, fsIgnore)
}

func (s *killSnapAppsErrorSuite) TestDoKillSnapAppsErrorBackendKillSnapServices(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapName := "some-snap"
	si := &snap.SideInfo{
		RealName: snapName,
		Revision: snap.R(1),
		SnapID:   fmt.Sprintf("%s-id", snapName),
	}
	snapstBefore := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		Active:   true,
	}
	snapstate.Set(s.state, snapName, snapstBefore)

	// Create snap.yaml on disk for snap.ReadInfo
	killSnapAppsTestYaml := fmt.Sprintf(`name: %s
version: 1.0
apps:
  svc1:
    command: bin/svc1
    daemon: simple
`, snapName)
	snaptest.MockSnap(c, killSnapAppsTestYaml, si)
	svcFile := filepath.Join(s.rootDir, fmt.Sprintf("/etc/systemd/system/snap.%s.svc1.service", snapName))
	c.Assert(os.MkdirAll(filepath.Dir(svcFile), 0755), IsNil)
	c.Assert(os.WriteFile(svcFile, []byte{}, 0644), IsNil)

	// Mock error in systemctl stop svc1
	restore := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		if len(cmd) == 2 && cmd[0] == "stop" && strings.Contains(cmd[1], fmt.Sprintf("%s.svc1", snapName)) {
			return nil, fmt.Errorf("mock systemctl error")
		}
		return []byte("ActiveState=active\n"), nil
	})
	defer restore()

	task := s.state.NewTask("kill-snap-apps", "")
	task.Set("kill-reason", snap.KillReasonRemove)
	task.Set("snap-setup", &snapstate.SnapSetup{SideInfo: si})
	chg := s.state.NewChange("test", "")
	chg.AddTask(task)

	fsBefore, err := testutil.CreateFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	fsAfter, err := testutil.CreateFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(task.Log(), HasLen, 1)
	c.Check(task.Log()[0], Matches, `.* ERROR mock systemctl error`)

	// Verify that the snap state is unchanged
	snapstAfter := &snapstate.SnapState{}
	snapstate.Get(s.state, snapName, snapstAfter)
	c.Check(snapstAfter, DeepEquals, snapstBefore)

	// Verify that the fs snapshot has acceptable differences
	fsIgnore := &testutil.FsSnapshotIgnoreDiff{
		// lock files are intentionally not removed on error
		fmt.Sprintf("run/snapd/lock/%s.lock", snapName): {
			Kinds:         []testutil.FsDiffKind{testutil.PresenceDiffKind},
			IgnoreParents: true,
		},
		fmt.Sprintf("var/lib/snapd/inhibit/%s.lock", snapName): {
			Kinds:         []testutil.FsDiffKind{testutil.PresenceDiffKind},
			IgnoreParents: true,
		},
	}
	c.Check(fsAfter, testutil.FsSnapshotsEqual, fsBefore, fsIgnore)
}

func (s *killSnapAppsErrorSuite) TestDoKillSnapAppsUndoesOnFutureTaskError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapName := "some-snap"
	si := &snap.SideInfo{
		RealName: snapName,
		Revision: snap.R(1),
		SnapID:   fmt.Sprintf("%s-id", snapName),
	}
	snapstBefore := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		Active:   true,
	}
	snapstate.Set(s.state, snapName, snapstBefore)

	// Create snap.yaml on disk for snap.ReadInfo
	killSnapAppsTestYaml := fmt.Sprintf(`name: %s
version: 1.0
apps:
  svc1:
    command: bin/svc1
    daemon: simple
`, snapName)
	snaptest.MockSnap(c, killSnapAppsTestYaml, si)
	svcFile := filepath.Join(s.rootDir, fmt.Sprintf("/etc/systemd/system/snap.%s.svc1.service", snapName))
	c.Assert(os.MkdirAll(filepath.Dir(svcFile), 0755), IsNil)
	c.Assert(os.WriteFile(svcFile, []byte{}, 0644), IsNil)

	restore := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	})
	defer restore()

	task := s.state.NewTask("kill-snap-apps", "")
	task.Set("kill-reason", snap.KillReasonRemove)
	task.Set("snap-setup", &snapstate.SnapSetup{SideInfo: si})
	chg := s.state.NewChange("test", "")
	chg.AddTask(task)

	terr := s.state.NewTask("error-trigger", "trigger kill-snap-apps undo")
	terr.WaitFor(task)
	chg.AddTask(terr)

	fsBefore, err := testutil.CreateFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	s.state.Unlock()
	for i := 0; i < 6; i++ {
		s.se.Ensure()
		s.se.Wait()
	}
	s.state.Lock()

	fsAfter, err := testutil.CreateFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	c.Check(task.Status(), Equals, state.UndoneStatus)
	c.Check(task.Log(), HasLen, 0)

	// Verify that the snap state is unchanged
	snapstAfter := &snapstate.SnapState{}
	snapstate.Get(s.state, snapName, snapstAfter)
	c.Check(snapstAfter, DeepEquals, snapstBefore)

	// Verify that the fs snapshot has acceptable differences
	fsIgnore := &testutil.FsSnapshotIgnoreDiff{
		// lock files are intentionally not removed on error
		fmt.Sprintf("run/snapd/lock/%s.lock", snapName): {
			Kinds:         []testutil.FsDiffKind{testutil.PresenceDiffKind},
			IgnoreParents: true,
		},
		fmt.Sprintf("var/lib/snapd/inhibit/%s.lock", snapName): {
			Kinds:         []testutil.FsDiffKind{testutil.PresenceDiffKind},
			IgnoreParents: true,
		},
	}
	c.Check(fsAfter, testutil.FsSnapshotsEqual, fsBefore, fsIgnore)
}
