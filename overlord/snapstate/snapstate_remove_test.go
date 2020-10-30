// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

func (s *snapmgrTestSuite) TestRemoveTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(11)},
		},
		Current:  snap.R(11),
		SnapType: "app",
	})

	ts, err := snapstate.Remove(s.state, "foo", snap.R(0), nil)
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	verifyRemoveTasks(c, ts)
}

func (s *snapmgrTestSuite) TestRemoveTasksAutoSnapshotDisabled(c *C) {
	snapstate.AutomaticSnapshot = func(st *state.State, instanceName string) (ts *state.TaskSet, err error) {
		return nil, snapstate.ErrNothingToDo
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(11)},
		},
		Current:  snap.R(11),
		SnapType: "app",
	})

	ts, err := snapstate.Remove(s.state, "foo", snap.R(0), nil)
	c.Assert(err, IsNil)

	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"stop-snap-services",
		"run-hook[remove]",
		"auto-disconnect",
		"remove-aliases",
		"unlink-snap",
		"unexport-content",
		"remove-profiles",
		"clear-snap",
		"discard-snap",
	})
}

func (s *snapmgrTestSuite) TestRemoveTasksAutoSnapshotDisabledByPurgeFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(11)},
		},
		Current:  snap.R(11),
		SnapType: "app",
	})

	ts, err := snapstate.Remove(s.state, "foo", snap.R(0), &snapstate.RemoveFlags{Purge: true})
	c.Assert(err, IsNil)

	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"stop-snap-services",
		"run-hook[remove]",
		"auto-disconnect",
		"remove-aliases",
		"unlink-snap",
		"unexport-content",
		"remove-profiles",
		"clear-snap",
		"discard-snap",
	})
}

func (s *snapmgrTestSuite) TestRemoveHookNotExecutedIfNotLastRevison(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(11)},
			{RealName: "foo", Revision: snap.R(12)},
		},
		Current: snap.R(12),
	})

	ts, err := snapstate.Remove(s.state, "foo", snap.R(11), nil)
	c.Assert(err, IsNil)

	runHooks := tasksWithKind(ts, "run-hook")
	// no 'remove' hook task
	c.Assert(runHooks, HasLen, 0)
}

func (s *snapmgrTestSuite) TestRemoveConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", Revision: snap.R(11)}},
		Current:  snap.R(11),
	})

	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(0), nil)
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("remove", "...").AddAll(ts)

	_, err = snapstate.Remove(s.state, "some-snap", snap.R(0), nil)
	c.Assert(err, ErrorMatches, `snap "some-snap" has "remove" change in progress`)
}

func (s *snapmgrTestSuite) testRemoveDiskSpaceCheck(c *C, featureFlag, automaticSnapshot bool) error {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstate.MockOsutilCheckFreeSpace(func(string, uint64) error {
		// osutil.CheckFreeSpace shouldn't be hit if either featureFlag
		// or automaticSnapshot is false. If both are true then we return disk
		// space error which should result in snapstate.InsufficientSpaceError
		// on remove().
		return &osutil.NotEnoughDiskSpaceError{}
	})
	defer restore()

	var automaticSnapshotCalled bool
	snapstate.AutomaticSnapshot = func(st *state.State, instanceName string) (ts *state.TaskSet, err error) {
		automaticSnapshotCalled = true
		if automaticSnapshot {
			t := s.state.NewTask("foo", "")
			ts = state.NewTaskSet(t)
			return ts, nil
		}
		// ErrNothingToDo is returned if automatic snapshots are disabled
		return nil, snapstate.ErrNothingToDo
	}

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.check-disk-space-remove", featureFlag)
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", Revision: snap.R(11)}},
		Current:  snap.R(11),
		SnapType: "app",
	})

	_, err := snapstate.Remove(s.state, "some-snap", snap.R(0), nil)
	c.Assert(automaticSnapshotCalled, Equals, true)
	return err
}

func (s *snapmgrTestSuite) TestRemoveDiskSpaceCheckDoesNothingWhenNoSnapshot(c *C) {
	featureFlag := true
	snapshot := false
	err := s.testRemoveDiskSpaceCheck(c, featureFlag, snapshot)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestRemoveDiskSpaceCheckDisabledByFeatureFlag(c *C) {
	featureFlag := false
	snapshot := true
	err := s.testRemoveDiskSpaceCheck(c, featureFlag, snapshot)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestRemoveDiskSpaceForSnapshotError(c *C) {
	featureFlag := true
	snapshot := true
	// both the snapshot and disk check feature are enabled, so we should hit
	// the disk check (which fails).
	err := s.testRemoveDiskSpaceCheck(c, featureFlag, snapshot)
	c.Assert(err, NotNil)

	diskSpaceErr := err.(*snapstate.InsufficientSpaceError)
	c.Assert(diskSpaceErr, ErrorMatches, `cannot create automatic snapshot when removing last revision of the snap: insufficient space.*`)
	c.Check(diskSpaceErr.Path, Equals, filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd"))
	c.Check(diskSpaceErr.Snaps, DeepEquals, []string{"some-snap"})
	c.Check(diskSpaceErr.ChangeKind, Equals, "remove")
}

func (s *snapmgrTestSuite) TestRemoveRunThrough(c *C) {
	c.Assert(snapstate.KeepAuxStoreInfo("some-snap-id", nil), IsNil)
	c.Check(snapstate.AuxStoreInfoFilename("some-snap-id"), testutil.FilePresent)
	si := snap.SideInfo{
		SnapID:   "some-snap-id",
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(0), nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:    "auto-disconnect:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "unexport-content:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "remove-snap-common-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "remove-snap-data-dir",
			name: "some-snap",
			path: filepath.Join(dirs.SnapDataDir, "some-snap"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/7"),
			stype: "app",
		},
		{
			op:   "discard-namespace",
			name: "some-snap",
		},
		{
			op:   "remove-inhibit-lock",
			name: "some-snap",
		},
		{
			op:   "remove-snap-dir",
			name: "some-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap"),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Check(len(s.fakeBackend.ops), Equals, len(expected))
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	tasks := ts.Tasks()
	for _, t := range tasks {
		if t.Kind() == "run-hook" {
			continue
		}
		if t.Kind() == "save-snapshot" {
			continue
		}
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)

		var expSnapSetup *snapstate.SnapSetup
		switch t.Kind() {
		case "discard-conns":
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
				},
			}
		case "clear-snap", "discard-snap":
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
					SnapID:   "some-snap-id",
					Revision: snap.R(7),
				},
			}
		default:
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
					SnapID:   "some-snap-id",
					Revision: snap.R(7),
				},
				Type:      snap.TypeApp,
				PlugsOnly: true,
			}

		}

		c.Check(snapsup, DeepEquals, expSnapSetup, Commentf(t.Kind()))
	}

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, Equals, state.ErrNoState)
	c.Check(snapstate.AuxStoreInfoFilename("some-snap-id"), testutil.FileAbsent)

}

func (s *snapmgrTestSuite) TestParallelInstanceRemoveRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	// pretend we have both a regular snap and a parallel instance
	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Active:      true,
		Sequence:    []*snap.SideInfo{&si},
		Current:     si.Revision,
		SnapType:    "app",
		InstanceKey: "instance",
	})
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap_instance", snap.R(0), nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:    "auto-disconnect:Doing",
			name:  "some-snap_instance",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap_instance",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
		},
		{
			op:    "unexport-content:Doing",
			name:  "some-snap_instance",
			revno: snap.R(7),
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap_instance",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
		},
		{
			op:   "remove-snap-common-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
		},
		{
			op:             "remove-snap-data-dir",
			name:           "some-snap_instance",
			path:           filepath.Join(dirs.SnapDataDir, "some-snap"),
			otherInstances: true,
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
			stype: "app",
		},
		{
			op:   "discard-namespace",
			name: "some-snap_instance",
		},
		{
			op:   "remove-inhibit-lock",
			name: "some-snap_instance",
		},
		{
			op:             "remove-snap-dir",
			name:           "some-snap_instance",
			path:           filepath.Join(dirs.SnapMountDir, "some-snap"),
			otherInstances: true,
		},
	}
	// start with an easier-to-read error if this fails:
	c.Check(len(s.fakeBackend.ops), Equals, len(expected))
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	tasks := ts.Tasks()
	for _, t := range tasks {
		if t.Kind() == "run-hook" {
			continue
		}
		if t.Kind() == "save-snapshot" {
			continue
		}
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)

		var expSnapSetup *snapstate.SnapSetup
		switch t.Kind() {
		case "discard-conns":
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
				},
				InstanceKey: "instance",
			}
		case "clear-snap", "discard-snap":
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
					Revision: snap.R(7),
				},
				InstanceKey: "instance",
			}
		default:
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
					Revision: snap.R(7),
				},
				Type:        snap.TypeApp,
				PlugsOnly:   true,
				InstanceKey: "instance",
			}

		}

		c.Check(snapsup, DeepEquals, expSnapSetup, Commentf(t.Kind()))
	}

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap_instance", &snapst)
	c.Assert(err, Equals, state.ErrNoState)

	// the non-instance snap is still there
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestParallelInstanceRemoveRunThroughOtherInstances(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	// pretend we have both a regular snap and a parallel instance
	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Active:      true,
		Sequence:    []*snap.SideInfo{&si},
		Current:     si.Revision,
		SnapType:    "app",
		InstanceKey: "instance",
	})
	snapstate.Set(s.state, "some-snap_other", &snapstate.SnapState{
		Active:      true,
		Sequence:    []*snap.SideInfo{&si},
		Current:     si.Revision,
		SnapType:    "app",
		InstanceKey: "other",
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap_instance", snap.R(0), nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:    "auto-disconnect:Doing",
			name:  "some-snap_instance",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap_instance",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
		},
		{
			op:    "unexport-content:Doing",
			name:  "some-snap_instance",
			revno: snap.R(7),
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap_instance",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
		},
		{
			op:   "remove-snap-common-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
		},
		{
			op:             "remove-snap-data-dir",
			name:           "some-snap_instance",
			path:           filepath.Join(dirs.SnapDataDir, "some-snap"),
			otherInstances: true,
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap_instance/7"),
			stype: "app",
		},
		{
			op:   "discard-namespace",
			name: "some-snap_instance",
		},
		{
			op:   "remove-inhibit-lock",
			name: "some-snap_instance",
		},
		{
			op:             "remove-snap-dir",
			name:           "some-snap_instance",
			path:           filepath.Join(dirs.SnapMountDir, "some-snap"),
			otherInstances: true,
		},
	}
	// start with an easier-to-read error if this fails:
	c.Check(len(s.fakeBackend.ops), Equals, len(expected))
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap_instance", &snapst)
	c.Assert(err, Equals, state.ErrNoState)

	// the other instance is still there
	err = snapstate.Get(s.state, "some-snap_other", &snapst)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestRemoveWithManyRevisionsRunThrough(c *C) {
	si3 := snap.SideInfo{
		SnapID:   "some-snap-id",
		RealName: "some-snap",
		Revision: snap.R(3),
	}

	si5 := snap.SideInfo{
		SnapID:   "some-snap-id",
		RealName: "some-snap",
		Revision: snap.R(5),
	}

	si7 := snap.SideInfo{
		SnapID:   "some-snap-id",
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si5, &si3, &si7},
		Current:  si7.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(0), nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	expected := fakeOps{
		{
			op:    "auto-disconnect:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "unexport-content:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/3"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/3"),
			stype: "app",
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/5"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/5"),
			stype: "app",
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "remove-snap-common-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "remove-snap-data-dir",
			name: "some-snap",
			path: filepath.Join(dirs.SnapDataDir, "some-snap"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/7"),
			stype: "app",
		},
		{
			op:   "discard-namespace",
			name: "some-snap",
		},
		{
			op:   "remove-inhibit-lock",
			name: "some-snap",
		},
		{
			op:   "remove-snap-dir",
			name: "some-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap"),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	tasks := ts.Tasks()
	revnos := []snap.Revision{{N: 3}, {N: 5}, {N: 7}}
	whichRevno := 0
	for _, t := range tasks {
		if t.Kind() == "run-hook" {
			continue
		}
		if t.Kind() == "save-snapshot" {
			continue
		}
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)

		var expSnapSetup *snapstate.SnapSetup
		switch t.Kind() {
		case "discard-conns":
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					SnapID:   "some-snap-id",
					RealName: "some-snap",
				},
			}
		case "clear-snap", "discard-snap":
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					SnapID:   "some-snap-id",
					RealName: "some-snap",
					Revision: revnos[whichRevno],
				},
			}
		default:
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					SnapID:   "some-snap-id",
					RealName: "some-snap",
					Revision: snap.R(7),
				},
				Type:      snap.TypeApp,
				PlugsOnly: true,
			}

		}

		c.Check(snapsup, DeepEquals, expSnapSetup, Commentf(t.Kind()))

		if t.Kind() == "discard-snap" {
			whichRevno++
		}
	}

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestRemoveOneRevisionRunThrough(c *C) {
	si3 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(3),
	}

	si5 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(5),
	}

	si7 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si5, &si3, &si7},
		Current:  si7.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(3), nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(len(s.fakeBackend.ops), Equals, 2)
	expected := fakeOps{
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/3"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/3"),
			stype: "app",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	tasks := ts.Tasks()
	for _, t := range tasks {
		if t.Kind() == "save-snapshot" {
			continue
		}
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)

		expSnapSetup := &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: "some-snap",
				Revision: snap.R(3),
			},
		}

		c.Check(snapsup, DeepEquals, expSnapSetup, Commentf(t.Kind()))
	}

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Sequence, HasLen, 2)
}

func (s *snapmgrTestSuite) TestRemoveLastRevisionRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   false,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(2), nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(len(s.fakeBackend.ops), Equals, 8)
	expected := fakeOps{
		{
			op:    "auto-disconnect:Doing",
			name:  "some-snap",
			revno: snap.R(2),
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
		},
		{
			op:   "remove-snap-common-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
		},
		{
			op:   "remove-snap-data-dir",
			name: "some-snap",
			path: filepath.Join(dirs.SnapDataDir, "some-snap"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/2"),
			stype: "app",
		},
		{
			op:   "discard-namespace",
			name: "some-snap",
		},
		{
			op:   "remove-inhibit-lock",
			name: "some-snap",
		},
		{
			op:   "remove-snap-dir",
			name: "some-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap"),
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	tasks := ts.Tasks()
	for _, t := range tasks {
		if t.Kind() == "run-hook" {
			continue
		}
		if t.Kind() == "save-snapshot" {
			continue
		}
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)

		expSnapSetup := &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: "some-snap",
			},
		}
		if t.Kind() != "discard-conns" {
			expSnapSetup.SideInfo.Revision = snap.R(2)
		}
		if t.Kind() == "auto-disconnect" {
			expSnapSetup.PlugsOnly = true
			expSnapSetup.Type = "app"
		}

		c.Check(snapsup, DeepEquals, expSnapSetup, Commentf(t.Kind()))
	}

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestRemoveCurrentActiveRevisionRefused(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	_, err := snapstate.Remove(s.state, "some-snap", snap.R(2), nil)

	c.Check(err, ErrorMatches, `cannot remove active revision 2 of snap "some-snap"`)
}

func (s *snapmgrTestSuite) TestRemoveCurrentRevisionOfSeveralRefused(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si, &si},
		Current:  si.Revision,
		SnapType: "app",
	})

	_, err := snapstate.Remove(s.state, "some-snap", snap.R(2), nil)
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `cannot remove active revision 2 of snap "some-snap" (revert first?)`)
}

func (s *snapmgrTestSuite) TestRemoveMissingRevisionRefused(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	_, err := snapstate.Remove(s.state, "some-snap", snap.R(1), nil)

	c.Check(err, ErrorMatches, `revision 1 of snap "some-snap" is not installed`)
}

func (s *snapmgrTestSuite) TestRemoveRefused(c *C) {
	si := snap.SideInfo{
		RealName: "brand-gadget",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "brand-gadget", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "gadget",
	})

	_, err := snapstate.Remove(s.state, "brand-gadget", snap.R(0), nil)

	c.Check(err, ErrorMatches, `snap "brand-gadget" is not removable: snap is used by the model`)
}

func (s *snapmgrTestSuite) TestRemoveRefusedLastRevision(c *C) {
	si := snap.SideInfo{
		RealName: "brand-gadget",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "brand-gadget", &snapstate.SnapState{
		Active:   false,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "gadget",
	})

	_, err := snapstate.Remove(s.state, "brand-gadget", snap.R(7), nil)

	c.Check(err, ErrorMatches, `snap "brand-gadget" is not removable: snap is used by the model`)
}

func (s *snapmgrTestSuite) TestRemoveDeletesConfigOnLastRevision(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	snapstate.Set(s.state, "another-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	tr := config.NewTransaction(s.state)
	tr.Set("some-snap", "foo", "bar")
	tr.Commit()

	// a config for some other snap to verify its not accidentally destroyed
	tr = config.NewTransaction(s.state)
	tr.Set("another-snap", "bar", "baz")
	tr.Commit()

	var res string
	tr = config.NewTransaction(s.state)
	c.Assert(tr.Get("some-snap", "foo", &res), IsNil)
	c.Assert(tr.Get("another-snap", "bar", &res), IsNil)

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(0), nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, Equals, state.ErrNoState)

	tr = config.NewTransaction(s.state)
	err = tr.Get("some-snap", "foo", &res)
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `snap "some-snap" has no "foo" configuration option`)

	// and another snap has its config intact
	c.Assert(tr.Get("another-snap", "bar", &res), IsNil)
	c.Assert(res, Equals, "baz")
}

func (s *snapmgrTestSuite) TestRemoveDoesntDeleteConfigIfNotLastRevision(c *C) {
	si1 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(8),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si1, &si2},
		Current:  si2.Revision,
		SnapType: "app",
	})

	tr := config.NewTransaction(s.state)
	tr.Set("some-snap", "foo", "bar")
	tr.Commit()

	var res string
	tr = config.NewTransaction(s.state)
	c.Assert(tr.Get("some-snap", "foo", &res), IsNil)

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", si1.Revision, nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	tr = config.NewTransaction(s.state)
	c.Assert(tr.Get("some-snap", "foo", &res), IsNil)
	c.Assert(res, Equals, "bar")
}

func (s *snapmgrTestSuite) TestRemoveMany(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "one", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "one", SnapID: "one-id", Revision: snap.R(1)},
		},
		Current: snap.R(1),
	})
	snapstate.Set(s.state, "two", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "two", SnapID: "two-id", Revision: snap.R(1)},
		},
		Current: snap.R(1),
	})

	removed, tts, err := snapstate.RemoveMany(s.state, []string{"one", "two"})
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 2)
	c.Check(removed, DeepEquals, []string{"one", "two"})

	c.Assert(s.state.TaskCount(), Equals, 9*2)
	for i, ts := range tts {
		c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
			"stop-snap-services",
			"run-hook[remove]",
			"auto-disconnect",
			"remove-aliases",
			"unlink-snap",
			"unexport-content",
			"remove-profiles",
			"clear-snap",
			"discard-snap",
		})
		verifyStopReason(c, ts, "remove")
		// check that tasksets are in separate lanes
		for _, t := range ts.Tasks() {
			c.Assert(t.Lanes(), DeepEquals, []int{i + 1})
		}

	}
}

func (s *snapmgrTestSuite) testRemoveManyDiskSpaceCheck(c *C, featureFlag, automaticSnapshot, freeSpaceCheckFail bool) error {
	s.state.Lock()
	defer s.state.Unlock()

	var checkFreeSpaceCall, snapshotSizeCall int

	// restored by TearDownTest
	snapstate.EstimateSnapshotSize = func(st *state.State, instanceName string, users []string) (uint64, error) {
		snapshotSizeCall++
		// expect two snapshot size estimations
		switch instanceName {
		case "one":
			return 10, nil
		case "two":
			return 20, nil
		default:
			c.Fatalf("unexpected snap: %s", instanceName)
		}
		return 1, nil
	}

	restore := snapstate.MockOsutilCheckFreeSpace(func(path string, required uint64) error {
		checkFreeSpaceCall++
		// required size is the sum of snapshot sizes of test snaps
		c.Check(required, Equals, snapstate.SafetyMarginDiskSpace(30))
		if freeSpaceCheckFail {
			return &osutil.NotEnoughDiskSpaceError{}
		}
		return nil
	})
	defer restore()

	var automaticSnapshotCalled bool
	snapstate.AutomaticSnapshot = func(st *state.State, instanceName string) (ts *state.TaskSet, err error) {
		automaticSnapshotCalled = true
		if automaticSnapshot {
			t := s.state.NewTask("foo", "")
			ts = state.NewTaskSet(t)
			return ts, nil
		}
		// ErrNothingToDo is returned if automatic snapshots are disabled
		return nil, snapstate.ErrNothingToDo
	}

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.check-disk-space-remove", featureFlag)
	tr.Commit()

	snapstate.Set(s.state, "one", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: []*snap.SideInfo{
			{RealName: "one", SnapID: "one-id", Revision: snap.R(1)},
		},
		Current: snap.R(1),
	})
	snapstate.Set(s.state, "two", &snapstate.SnapState{
		Active:   true,
		SnapType: "app",
		Sequence: []*snap.SideInfo{
			{RealName: "two", SnapID: "two-id", Revision: snap.R(1)},
		},
		Current: snap.R(1),
	})

	_, _, err := snapstate.RemoveMany(s.state, []string{"one", "two"})
	if featureFlag && automaticSnapshot {
		c.Check(snapshotSizeCall, Equals, 2)
		c.Check(checkFreeSpaceCall, Equals, 1)
	} else {
		c.Check(checkFreeSpaceCall, Equals, 0)
		c.Check(snapshotSizeCall, Equals, 0)
	}
	c.Check(automaticSnapshotCalled, Equals, true)

	return err
}

func (s *snapmgrTestSuite) TestRemoveManyDiskSpaceError(c *C) {
	featureFlag := true
	automaticSnapshot := true
	freeSpaceCheckFail := true
	err := s.testRemoveManyDiskSpaceCheck(c, featureFlag, automaticSnapshot, freeSpaceCheckFail)

	diskSpaceErr := err.(*snapstate.InsufficientSpaceError)
	c.Check(diskSpaceErr.Path, Equals, filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd"))
	c.Check(diskSpaceErr.Snaps, DeepEquals, []string{"one", "two"})
	c.Check(diskSpaceErr.ChangeKind, Equals, "remove")
}

func (s *snapmgrTestSuite) TestRemoveManyDiskSpaceCheckDisabled(c *C) {
	featureFlag := false
	automaticSnapshot := true
	freeSpaceCheckFail := true
	err := s.testRemoveManyDiskSpaceCheck(c, featureFlag, automaticSnapshot, freeSpaceCheckFail)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestRemoveManyDiskSpaceSnapshotDisabled(c *C) {
	featureFlag := true
	automaticSnapshot := false
	freeSpaceCheckFail := true
	err := s.testRemoveManyDiskSpaceCheck(c, featureFlag, automaticSnapshot, freeSpaceCheckFail)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestRemoveManyDiskSpaceCheckPasses(c *C) {
	featureFlag := true
	automaticSnapshot := true
	freeSpaceCheckFail := false
	err := s.testRemoveManyDiskSpaceCheck(c, featureFlag, automaticSnapshot, freeSpaceCheckFail)
	c.Check(err, IsNil)
}

type snapdBackend struct {
	fakeSnappyBackend
}

func (f *snapdBackend) RemoveSnapData(info *snap.Info) error {
	dir := snap.DataDir(info.SnapName(), info.Revision)
	if err := os.Remove(dir); err != nil {
		return fmt.Errorf("unexpected error: %v", err)
	}
	return f.fakeSnappyBackend.RemoveSnapData(info)
}

func (f *snapdBackend) RemoveSnapCommonData(info *snap.Info) error {
	dir := snap.CommonDataDir(info.SnapName())
	if err := os.Remove(dir); err != nil {
		return fmt.Errorf("unexpected error: %v", err)
	}
	return f.fakeSnappyBackend.RemoveSnapCommonData(info)
}

func isUndone(c *C, tasks []*state.Task, kind string, numExpected int) {
	var count int
	for _, t := range tasks {
		if t.Kind() == kind {
			c.Assert(t.Status(), Equals, state.UndoneStatus)
			count++
		}
	}
	c.Assert(count, Equals, numExpected)
}

func injectError(c *C, chg *state.Change, beforeTaskKind string, snapRev snap.Revision) {
	var found bool
	for _, t := range chg.Tasks() {
		if t.Kind() != beforeTaskKind {
			continue
		}
		sup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)
		if sup.Revision() != snapRev {
			continue
		}
		prev := t.WaitTasks()[0]
		terr := chg.State().NewTask("error-trigger", "provoking undo")
		t.WaitFor(terr)
		terr.WaitFor(prev)
		chg.AddTask(terr)
		found = true
		break
	}
	c.Assert(found, Equals, true)
}

func makeTestSnaps(c *C, st *state.State) {
	si1 := snap.SideInfo{
		SnapID:   "some-snap-id",
		RealName: "some-snap",
		Revision: snap.R(1),
	}

	si2 := snap.SideInfo{
		SnapID:   "some-snap-id",
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	snapstate.Set(st, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si1, &si2},
		Current:  si1.Revision,
		SnapType: "app",
	})

	c.Assert(os.MkdirAll(snap.DataDir("some-snap", si1.Revision), 0755), IsNil)
	c.Assert(os.MkdirAll(snap.DataDir("some-snap", si2.Revision), 0755), IsNil)
	c.Assert(os.MkdirAll(snap.CommonDataDir("some-snap"), 0755), IsNil)
}

func (s *snapmgrTestSuite) TestRemoveManyUndoRestoresCurrent(c *C) {
	b := &snapdBackend{}
	snapstate.SetSnapManagerBackend(s.snapmgr, b)
	AddForeignTaskHandlers(s.o.TaskRunner(), b)

	s.state.Lock()
	defer s.state.Unlock()
	makeTestSnaps(c, s.state)

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(0), nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	// inject an error before clear-snap of revision 1 (current), after
	// discard-snap for revision 2, that means data and snap rev 1
	// are still present.
	injectError(c, chg, "clear-snap", snap.Revision{N: 1})

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	isUndone(c, chg.Tasks(), "unlink-snap", 1)

	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, "some-snap", &snapst), IsNil)
	c.Check(snapst.Active, Equals, true)
	c.Check(snapst.Current, Equals, snap.Revision{N: 1})
	c.Assert(snapst.Sequence, HasLen, 1)
	c.Check(snapst.Sequence[0].Revision, Equals, snap.Revision{N: 1})

	expected := fakeOps{
		{
			op:    "auto-disconnect:Doing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/1"),
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/2"),
			stype: "app",
		},
		{
			op:    "remove-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/1"),
		},
		{
			op: "update-aliases",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Check(len(b.ops), Equals, len(expected))
	c.Assert(b.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(b.ops, DeepEquals, expected)
}

func (s *snapmgrTestSuite) TestRemoveManyUndoLeavesInactiveSnapAfterDataIsLost(c *C) {
	b := &snapdBackend{}
	snapstate.SetSnapManagerBackend(s.snapmgr, b)
	AddForeignTaskHandlers(s.o.TaskRunner(), b)

	s.state.Lock()
	defer s.state.Unlock()
	makeTestSnaps(c, s.state)

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", snap.R(0), nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	// inject an error after removing data of both revisions (which includes
	// current rev 1), before discarding the snap completely.
	injectError(c, chg, "discard-snap", snap.Revision{N: 1})

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	isUndone(c, chg.Tasks(), "unlink-snap", 1)

	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, "some-snap", &snapst), IsNil)

	// revision 1 is still present but not active, since the error happened
	// after its data was removed.
	c.Check(snapst.Active, Equals, false)
	c.Check(snapst.Current, Equals, snap.Revision{N: 1})
	c.Assert(snapst.Sequence, HasLen, 1)
	c.Check(snapst.Sequence[0].Revision, Equals, snap.Revision{N: 1})

	expected := fakeOps{
		{
			op:    "auto-disconnect:Doing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/1"),
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/2"),
			stype: "app",
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/1"),
		},
		{
			op:   "remove-snap-common-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/1"),
		},
		{
			op:   "remove-snap-data-dir",
			name: "some-snap",
			path: filepath.Join(dirs.SnapDataDir, "some-snap"),
		},
		{
			op:    "remove-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op: "update-aliases",
		},
	}

	// start with an easier-to-read error if this fails:
	c.Check(len(b.ops), Equals, len(expected))
	c.Assert(b.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(b.ops, DeepEquals, expected)
}
