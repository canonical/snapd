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
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

// fsEntry records attributes of a fs entry for comparison
type fsEntry struct {
	mode        os.FileMode
	size        int64
	contentHash string // empty for dirs
}

// maps path to fsEntry
type fsSnapshot map[string]fsEntry

func getFileHash(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// createFsSnapshot walks the root directory and collects fs entries
func createFsSnapshot(rootDir string) (fsSnapshot, error) {
	entries := make(fsSnapshot)
	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entry := fsEntry{
			mode: info.Mode(),
			size: info.Size(),
		}
		if !d.IsDir() {
			hash, err := getFileHash(path)
			if err != nil {
				return err
			}
			entry.contentHash = hash
		}
		entries[rel] = entry
		return nil
	})
	return entries, err
}

type fsDiffType string

const (
	presence fsDiffType = "presence"
	mode     fsDiffType = "mode"
	size     fsDiffType = "size"
	content  fsDiffType = "content"
)

// maps path to all fsDiffTypes for that path
type fsSnapshotDiff map[string][]fsDiffType

type fsIgnoreDiff struct {
	dtypes        []fsDiffType
	ignoreParents bool
	// can be extended to ignoreChildren, if needed
}

// TODO:GOVERSION: replace this with slices.Contains() once we're on go 1.21+
func contains[T comparable](s []T, e T) bool {
	for _, k := range s {
		if k == e {
			return true
		}
	}
	return false
}

// maps path to fsIgnoreDiff for that path
type fsSnapshotIgnoreDiff map[string]fsIgnoreDiff

func (fi *fsSnapshotIgnoreDiff) isIgnored(path string, dt fsDiffType) bool {
	if fi == nil {
		return false
	}
	for p, diff := range *fi {
		if p == path && contains(diff.dtypes, dt) {
			return true
		}
		// If IgnoreParents is set, ignore all its parents too
		if diff.ignoreParents {
			if strings.HasPrefix(p, path) && contains(diff.dtypes, dt) {
				return true
			}
		}
	}
	return false
}

// compareFsSnapshots compares two fs snapshots, returning differences that are not ignored
func compareFsSnapshots(before, after fsSnapshot, ignore *fsSnapshotIgnoreDiff) fsSnapshotDiff {
	diffs := make(fsSnapshotDiff)

	allPaths := make(map[string]struct{})
	for p := range before {
		allPaths[p] = struct{}{}
	}
	for p := range after {
		allPaths[p] = struct{}{}
	}

	for path := range allPaths {
		if ignore.isIgnored(path, presence) {
			continue
		}
		bEntry, bHas := before[path]
		aEntry, aHas := after[path]

		if (bHas && !aHas) || (!bHas && aHas) {
			diffs[path] = append(diffs[path], presence)
			continue
		}

		// Both exist - compare attributes
		if bEntry.mode != aEntry.mode && !ignore.isIgnored(path, mode) {
			diffs[path] = append(diffs[path], mode)
		}
		if bEntry.size != aEntry.size && !ignore.isIgnored(path, size) {
			diffs[path] = append(diffs[path], size)
		}
		if bEntry.contentHash != aEntry.contentHash && !ignore.isIgnored(path, content) {
			diffs[path] = append(diffs[path], content)
		}
	}
	return diffs
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

	fsBefore, err := createFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	fsAfter, err := createFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(task.Log(), HasLen, 1)
	c.Check(task.Log()[0], Matches, `.* ERROR no state entry for key "snap-setup-task"`)

	// Verify that the snap state is unchanged
	snapstAfter := &snapstate.SnapState{}
	snapstate.Get(s.state, snapName, snapstAfter)
	c.Check(snapstAfter, DeepEquals, snapstBefore)

	// Verify that the fs snapshot is unchanged
	fsDiffs := compareFsSnapshots(fsBefore, fsAfter, nil)
	c.Check(fsDiffs, HasLen, 0)
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

	fsBefore, err := createFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	fsAfter, err := createFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(task.Log(), HasLen, 1)
	c.Check(task.Log()[0], Matches, fmt.Sprintf(`.* ERROR open .*/run/snapd/lock/%s.lock: permission denied`, snapName))

	// Verify that the snap state is unchanged
	snapstAfter := &snapstate.SnapState{}
	snapstate.Get(s.state, snapName, snapstAfter)
	c.Check(snapstAfter, DeepEquals, snapstBefore)

	// Verify that the fs snapshot is unchanged
	fsDiffs := compareFsSnapshots(fsBefore, fsAfter, nil)
	c.Check(fsDiffs, HasLen, 0)
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

	fsBefore, err := createFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	fsAfter, err := createFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(task.Log(), HasLen, 1)
	c.Check(task.Log()[0], Matches, `.* ERROR .*could not unmarshal state entry "kill-reason".*`)

	// Verify that the snap state is unchanged
	snapstAfter := &snapstate.SnapState{}
	snapstate.Get(s.state, snapName, snapstAfter)
	c.Check(snapstAfter, DeepEquals, snapstBefore)

	// Verify that the fs snapshot has acceptable differences
	fsIgnoreDiff := &fsSnapshotIgnoreDiff{
		// lock file is intentionally not removed on error
		fmt.Sprintf("run/snapd/lock/%s.lock", snapName): {
			dtypes:        []fsDiffType{presence},
			ignoreParents: true,
		},
	}
	fsDiffs := compareFsSnapshots(fsBefore, fsAfter, fsIgnoreDiff)
	c.Check(fsDiffs, HasLen, 0)
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

	fsBefore, err := createFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	fsAfter, err := createFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(task.Log(), HasLen, 1)
	c.Check(task.Log()[0], Matches, fmt.Sprintf(`.* ERROR open .*/var/lib/snapd/inhibit/%s.lock: permission denied`, snapName))

	// Verify that the snap state is unchanged
	snapstAfter := &snapstate.SnapState{}
	snapstate.Get(s.state, snapName, snapstAfter)
	c.Check(snapstAfter, DeepEquals, snapstBefore)

	// Verify that the fs snapshot has acceptable differences
	fsIgnoreDiff := &fsSnapshotIgnoreDiff{
		// lock file is intentionally not removed on error
		fmt.Sprintf("run/snapd/lock/%s.lock", snapName): {
			dtypes:        []fsDiffType{presence},
			ignoreParents: true,
		},
	}
	fsDiffs := compareFsSnapshots(fsBefore, fsAfter, fsIgnoreDiff)
	c.Check(fsDiffs, HasLen, 0)
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

	fsBefore, err := createFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	fsAfter, err := createFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(task.Log(), HasLen, 1)
	c.Check(task.Log()[0], Matches, `.* ERROR mock SnapReadInfo error`)

	// Verify that the snap state is unchanged
	snapstAfter := &snapstate.SnapState{}
	snapstate.Get(s.state, snapName, snapstAfter)
	c.Check(snapstAfter, DeepEquals, snapstBefore)

	// Verify that the fs snapshot has acceptable differences
	fsIgnoreDiff := &fsSnapshotIgnoreDiff{
		// lock files are intentionally not removed on error
		fmt.Sprintf("run/snapd/lock/%s.lock", snapName): {
			dtypes:        []fsDiffType{presence},
			ignoreParents: true,
		},
		fmt.Sprintf("var/lib/snapd/inhibit/%s.lock", snapName): {
			dtypes:        []fsDiffType{presence},
			ignoreParents: true,
		},
		// cgroup.kill file is expected to be modified
		fmt.Sprintf("%s/cgroup.kill", cgDir): {
			dtypes: []fsDiffType{size, content},
		},
	}
	fsDiffs := compareFsSnapshots(fsBefore, fsAfter, fsIgnoreDiff)
	c.Check(fsDiffs, HasLen, 0)
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

	fsBefore, err := createFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	fsAfter, err := createFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(task.Log(), HasLen, 1)
	c.Check(task.Log()[0], Matches, `.* ERROR mock systemctl error`)

	// Verify that the snap state is unchanged
	snapstAfter := &snapstate.SnapState{}
	snapstate.Get(s.state, snapName, snapstAfter)
	c.Check(snapstAfter, DeepEquals, snapstBefore)

	// Verify that the fs snapshot has acceptable differences
	fsIgnoreDiff := &fsSnapshotIgnoreDiff{
		// lock files are intentionally not removed on error
		fmt.Sprintf("run/snapd/lock/%s.lock", snapName): {
			dtypes:        []fsDiffType{presence},
			ignoreParents: true,
		},
		fmt.Sprintf("var/lib/snapd/inhibit/%s.lock", snapName): {
			dtypes:        []fsDiffType{presence},
			ignoreParents: true,
		},
	}
	fsDiffs := compareFsSnapshots(fsBefore, fsAfter, fsIgnoreDiff)
	c.Check(fsDiffs, HasLen, 0)
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

	fsBefore, err := createFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	s.state.Unlock()
	for i := 0; i < 6; i++ {
		s.se.Ensure()
		s.se.Wait()
	}
	s.state.Lock()

	fsAfter, err := createFsSnapshot(s.rootDir)
	c.Assert(err, IsNil)

	c.Check(task.Status(), Equals, state.UndoneStatus)
	c.Check(task.Log(), HasLen, 0)

	// Verify that the snap state is unchanged
	snapstAfter := &snapstate.SnapState{}
	snapstate.Get(s.state, snapName, snapstAfter)
	c.Check(snapstAfter, DeepEquals, snapstBefore)

	// Verify that the fs snapshot has acceptable differences
	fsIgnoreDiff := &fsSnapshotIgnoreDiff{
		// lock files are intentionally not removed on error
		fmt.Sprintf("run/snapd/lock/%s.lock", snapName): {
			dtypes:        []fsDiffType{presence},
			ignoreParents: true,
		},
		fmt.Sprintf("var/lib/snapd/inhibit/%s.lock", snapName): {
			dtypes:        []fsDiffType{presence},
			ignoreParents: true,
		},
	}
	fsDiffs := compareFsSnapshots(fsBefore, fsAfter, fsIgnoreDiff)
	c.Check(fsDiffs, HasLen, 0)
}
