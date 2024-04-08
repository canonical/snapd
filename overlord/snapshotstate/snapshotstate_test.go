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

package snapshotstate_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapshotstate"
	"github.com/snapcore/snapd/overlord/snapshotstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type snapshotSuite struct {
	testutil.BaseTest
}

var _ = check.Suite(&snapshotSuite{})

// tie gocheck into testing
func TestSnapshot(t *testing.T) { check.TestingT(t) }

func (s *snapshotSuite) SetUpTest(c *check.C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	os.MkdirAll(dirs.SnapshotsDir, os.ModePerm)

	old := snapstate.EnforcedValidationSets
	s.AddCleanup(func() {
		snapstate.EnforcedValidationSets = old
	})
	snapstate.EnforcedValidationSets = func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		return nil, nil
	}
}

func (s *snapshotSuite) TearDownTest(c *check.C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("/")
}

func (snapshotSuite) TestNewSnapshotSetID(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	// Disk last set id unset, state set id unset, use 1
	sid, err := snapshotstate.NewSnapshotSetID(st)
	c.Assert(err, check.IsNil)
	c.Check(sid, check.Equals, uint64(1))

	var stateSetID uint64
	c.Assert(st.Get("last-snapshot-set-id", &stateSetID), check.IsNil)
	c.Check(stateSetID, check.Equals, uint64(1))

	c.Assert(os.WriteFile(filepath.Join(dirs.SnapshotsDir, "9_some-snap-1.zip"), []byte{}, 0644), check.IsNil)

	// Disk last set id 9 > state set id 1, use 9++ = 10
	sid, err = snapshotstate.NewSnapshotSetID(st)
	c.Assert(err, check.IsNil)
	c.Check(sid, check.Equals, uint64(10))

	c.Assert(st.Get("last-snapshot-set-id", &stateSetID), check.IsNil)
	c.Check(stateSetID, check.Equals, uint64(10))

	// Disk last set id 9 < state set id 10, use 10++ = 11
	sid, err = snapshotstate.NewSnapshotSetID(st)
	c.Assert(err, check.IsNil)
	c.Check(sid, check.Equals, uint64(11))

	c.Assert(st.Get("last-snapshot-set-id", &stateSetID), check.IsNil)
	c.Check(stateSetID, check.Equals, uint64(11))

	c.Assert(os.WriteFile(filepath.Join(dirs.SnapshotsDir, "88_some-snap-1.zip"), []byte{}, 0644), check.IsNil)

	// Disk last set id 88 > state set id 11, use 88++ = 89
	sid, err = snapshotstate.NewSnapshotSetID(st)
	c.Assert(err, check.IsNil)
	c.Check(sid, check.Equals, uint64(89))

	c.Assert(st.Get("last-snapshot-set-id", &stateSetID), check.IsNil)
	c.Check(stateSetID, check.Equals, uint64(89))
}

func (snapshotSuite) TestAllActiveSnapNames(c *check.C) {
	fakeSnapstateAll := func(*state.State) (map[string]*snapstate.SnapState, error) {
		return map[string]*snapstate.SnapState{
			"a-snap": {Active: true},
			"b-snap": {},
			"c-snap": {Active: true},
		}, nil
	}

	defer snapshotstate.MockSnapstateAll(fakeSnapstateAll)()

	// loop to check sortedness
	for i := 0; i < 100; i++ {
		names, err := snapshotstate.AllActiveSnapNames(nil)
		c.Assert(err, check.IsNil)
		c.Check(names, check.DeepEquals, []string{"a-snap", "c-snap"})
	}
}

func (snapshotSuite) TestAllActiveSnapNamesError(c *check.C) {
	errBad := errors.New("bad")
	fakeSnapstateAll := func(*state.State) (map[string]*snapstate.SnapState, error) {
		return nil, errBad
	}

	defer snapshotstate.MockSnapstateAll(fakeSnapstateAll)()

	names, err := snapshotstate.AllActiveSnapNames(nil)
	c.Check(err, check.Equals, errBad)
	c.Check(names, check.IsNil)
}

func (snapshotSuite) TestSnapSummariesInSnapshotSet(c *check.C) {
	shotfileA, err := os.Create(filepath.Join(c.MkDir(), "foo.zip"))
	c.Assert(err, check.IsNil)
	defer shotfileA.Close()
	shotfileB, err := os.Create(filepath.Join(c.MkDir(), "foo.zip"))
	c.Assert(err, check.IsNil)
	defer shotfileB.Close()

	setID := uint64(42)
	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			// wanted
			Snapshot: client.Snapshot{SetID: setID, Snap: "a-snap", SnapID: "a-id", Epoch: snap.Epoch{Read: []uint32{42}, Write: []uint32{17}}},
			File:     shotfileA,
		}), check.IsNil)
		c.Assert(f(&backend.Reader{
			// not wanted (bad set id)
			Snapshot: client.Snapshot{SetID: setID + 1, Snap: "a-snap", SnapID: "a-id"},
			File:     shotfileA,
		}), check.IsNil)
		c.Assert(f(&backend.Reader{
			// wanted
			Snapshot: client.Snapshot{SetID: setID, Snap: "b-snap", SnapID: "b-id"},
			File:     shotfileB,
		}), check.IsNil)
		return nil
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	summaries, err := snapshotstate.SnapSummariesInSnapshotSet(setID, nil)
	c.Assert(err, check.IsNil)
	c.Assert(summaries.AsMaps(), check.DeepEquals, []map[string]string{
		{"snap": "a-snap", "snapID": "a-id", "filename": shotfileA.Name(), "epoch": `{"read":[42],"write":[17]}`},
		{"snap": "b-snap", "snapID": "b-id", "filename": shotfileB.Name(), "epoch": "0"},
	})
}

func (snapshotSuite) TestSnapSummariesInSnapshotSetSnaps(c *check.C) {
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "foo.zip"))
	c.Assert(err, check.IsNil)
	defer shotfile.Close()
	setID := uint64(42)
	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			// wanted
			Snapshot: client.Snapshot{SetID: setID, Snap: "a-snap", SnapID: "a-id"},
			File:     shotfile,
		}), check.IsNil)
		c.Assert(f(&backend.Reader{
			// not wanted (bad set id)
			Snapshot: client.Snapshot{SetID: setID + 1, Snap: "a-snap", SnapID: "a-id"},
			File:     shotfile,
		}), check.IsNil)
		c.Assert(f(&backend.Reader{
			// not wanted (bad snap name)
			Snapshot: client.Snapshot{SetID: setID, Snap: "c-snap", SnapID: "c-id"},
			File:     shotfile,
		}), check.IsNil)
		return nil
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	summaries, err := snapshotstate.SnapSummariesInSnapshotSet(setID, []string{"a-snap"})
	c.Assert(err, check.IsNil)
	c.Check(summaries.AsMaps(), check.DeepEquals, []map[string]string{
		{"snap": "a-snap", "snapID": "a-id", "filename": shotfile.Name(), "epoch": "0"},
	})
}

func (snapshotSuite) TestSnapSummariesInSnapshotSetErrors(c *check.C) {
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "foo.zip"))
	c.Assert(err, check.IsNil)
	defer shotfile.Close()
	setID := uint64(42)
	errBad := errors.New("bad")
	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			// wanted
			Snapshot: client.Snapshot{SetID: setID, Snap: "a-snap"},
			File:     shotfile,
		}), check.IsNil)

		return errBad
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	summaries, err := snapshotstate.SnapSummariesInSnapshotSet(setID, nil)
	c.Assert(err, check.Equals, errBad)
	c.Check(summaries, check.IsNil)
}

func (snapshotSuite) TestSnapSummariesInSnapshotSetNotFound(c *check.C) {
	setID := uint64(42)
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "foo.zip"))
	c.Assert(err, check.IsNil)
	defer shotfile.Close()
	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			// not wanted
			Snapshot: client.Snapshot{SetID: setID - 1, Snap: "a-snap"},
			File:     shotfile,
		}), check.IsNil)

		return nil
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	summaries, err := snapshotstate.SnapSummariesInSnapshotSet(setID, nil)
	c.Assert(err, check.Equals, client.ErrSnapshotSetNotFound)
	c.Check(summaries, check.IsNil)
}

func (snapshotSuite) TestSnapSummariesInSnapshotSetEmptyNotFound(c *check.C) {
	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error { return nil }
	defer snapshotstate.MockBackendIter(fakeIter)()

	summaries, err := snapshotstate.SnapSummariesInSnapshotSet(42, nil)
	c.Assert(err, check.Equals, client.ErrSnapshotSetNotFound)
	c.Check(summaries, check.IsNil)
}

func (snapshotSuite) TestSnapSummariesInSnapshotSetSnapNotFound(c *check.C) {
	setID := uint64(42)
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "foo.zip"))
	c.Assert(err, check.IsNil)
	defer shotfile.Close()
	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			// not wanted
			Snapshot: client.Snapshot{SetID: setID, Snap: "a-snap"},
			File:     shotfile,
		}), check.IsNil)

		return nil
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	summaries, err := snapshotstate.SnapSummariesInSnapshotSet(setID, []string{"b-snap"})
	c.Assert(err, check.Equals, client.ErrSnapshotSnapsNotFound)
	c.Check(summaries, check.IsNil)
}

func (snapshotSuite) TestCheckConflict(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	chg := st.NewChange("some-change", "...")
	tsk := st.NewTask("some-task", "...")
	tsk.SetStatus(state.DoingStatus)
	chg.AddTask(tsk)

	// no snapshot state
	err := snapshotstate.CheckSnapshotConflict(st, 42, "some-task")
	c.Assert(err, check.ErrorMatches, "internal error: task 1 .some-task. is missing snapshot information")

	// wrong snapshot state
	tsk.Set("snapshot-setup", "hello")
	err = snapshotstate.CheckSnapshotConflict(st, 42, "some-task")
	c.Assert(err, check.ErrorMatches, "internal error.* could not unmarshal.*")

	tsk.Set("snapshot-setup", map[string]int{"set-id": 42})

	err = snapshotstate.CheckSnapshotConflict(st, 42, "some-task")
	c.Assert(err, check.ErrorMatches, "cannot operate on snapshot set #42 while change \"1\" is in progress")

	// no change with that label
	c.Assert(snapshotstate.CheckSnapshotConflict(st, 42, "some-other-task"), check.IsNil)

	// no change with that snapshot id
	c.Assert(snapshotstate.CheckSnapshotConflict(st, 43, "some-task"), check.IsNil)

	// no non-ready change
	tsk.SetStatus(state.DoneStatus)
	c.Assert(snapshotstate.CheckSnapshotConflict(st, 42, "some-task"), check.IsNil)
}

func (snapshotSuite) TestCheckConflictSnapshotOpInProgress(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	snapshotstate.SetSnapshotOpInProgress(st, 1, "foo-op")
	snapshotstate.SetSnapshotOpInProgress(st, 2, "bar-op")

	c.Assert(snapshotstate.CheckSnapshotConflict(st, 1, "foo-op"), check.ErrorMatches, `cannot operate on snapshot set #1 while operation foo-op is in progress`)
	// unrelated set-id doesn't conflict
	c.Assert(snapshotstate.CheckSnapshotConflict(st, 3, "foo-op"), check.IsNil)
	c.Assert(snapshotstate.CheckSnapshotConflict(st, 3, "bar-op"), check.IsNil)
	// non-conflicting op
	c.Assert(snapshotstate.CheckSnapshotConflict(st, 1, "safe-op"), check.IsNil)
}

func (snapshotSuite) TestSaveChecksSnapnamesError(c *check.C) {
	defer snapshotstate.MockSnapstateAll(func(*state.State) (map[string]*snapstate.SnapState, error) {
		return nil, errors.New("bzzt")
	})()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	_, _, _, err := snapshotstate.Save(st, nil, nil, nil)
	c.Check(err, check.ErrorMatches, "bzzt")
}

func (snapshotSuite) createConflictingChange(c *check.C) (st *state.State, restore func()) {
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "foo.zip"))
	c.Assert(err, check.IsNil)
	shotfile.Close()

	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			Snapshot: client.Snapshot{SetID: 42, Snap: "foo"},
			File:     shotfile,
		}), check.IsNil)

		return nil
	}
	restoreIter := snapshotstate.MockBackendIter(fakeIter)

	o := overlord.Mock()
	st = o.State()

	stmgr, err := snapstate.Manager(st, o.TaskRunner())
	c.Assert(err, check.IsNil)
	o.AddManager(stmgr)
	shmgr := snapshotstate.Manager(st, o.TaskRunner())
	o.AddManager(shmgr)

	st.Lock()
	defer func() {
		if c.Failed() {
			// something went wrong
			st.Unlock()
		}
	}()

	snapstate.Set(st, "foo", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	r := snapstatetest.UseFallbackDeviceModel()
	defer r()

	chg := st.NewChange("rm foo", "...")
	rmTasks, err := snapstate.Remove(st, "foo", snap.R(0), nil)
	c.Assert(err, check.IsNil)
	c.Assert(rmTasks, check.NotNil)
	chg.AddAll(rmTasks)

	return st, func() {
		shotfile.Close()
		st.Unlock()
		restoreIter()
	}
}

func (s snapshotSuite) TestSaveChecksSnapstateConflict(c *check.C) {
	st, restore := s.createConflictingChange(c)
	defer restore()

	_, _, _, err := snapshotstate.Save(st, []string{"foo"}, nil, nil)
	c.Assert(err, check.NotNil)
	c.Check(err, check.FitsTypeOf, &snapstate.ChangeConflictError{})
}

func (snapshotSuite) TestSaveConflictsWithSnapstate(c *check.C) {
	fakeSnapstateAll := func(*state.State) (map[string]*snapstate.SnapState, error) {
		return map[string]*snapstate.SnapState{
			"foo": {Active: true},
		}, nil
	}

	defer snapshotstate.MockSnapstateAll(fakeSnapstateAll)()

	o := overlord.Mock()
	st := o.State()

	stmgr, err := snapstate.Manager(st, o.TaskRunner())
	c.Assert(err, check.IsNil)
	o.AddManager(stmgr)
	shmgr := snapshotstate.Manager(st, o.TaskRunner())
	o.AddManager(shmgr)

	st.Lock()
	defer st.Unlock()

	snapstate.Set(st, "foo", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	chg := st.NewChange("snapshot-save", "...")
	_, _, saveTasks, err := snapshotstate.Save(st, nil, nil, nil)
	c.Assert(err, check.IsNil)
	chg.AddAll(saveTasks)

	_, err = snapstate.Disable(st, "foo")
	c.Assert(err, check.ErrorMatches, `snap "foo" has "snapshot-save" change in progress`)
}

func (snapshotSuite) TestSaveChecksSnapstateConflictError(c *check.C) {
	defer snapshotstate.MockSnapstateCheckChangeConflictMany(func(*state.State, []string, string) error {
		return errors.New("bzzt")
	})()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	_, _, _, err := snapshotstate.Save(st, nil, nil, nil)
	c.Check(err, check.ErrorMatches, "bzzt")
}

func (snapshotSuite) TestSaveChecksSetIDError(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	st.Set("last-snapshot-set-id", "3/4")

	_, _, _, err := snapshotstate.Save(st, nil, nil, nil)
	c.Check(err, check.ErrorMatches, ".* could not unmarshal .*")
}

func (snapshotSuite) TestSaveNoSnapsInState(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	setID, saved, taskset, err := snapshotstate.Save(st, nil, nil, nil)
	c.Assert(err, check.IsNil)
	c.Check(setID, check.Equals, uint64(1))
	c.Check(saved, check.HasLen, 0)
	c.Check(taskset.Tasks(), check.HasLen, 0)
}

func (snapshotSuite) TestSaveSnapNotInstalled(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	setID, saved, taskset, err := snapshotstate.Save(st, []string{"foo"}, nil, nil)
	c.Assert(err, check.ErrorMatches, `snap "foo" is not installed`)
	c.Check(setID, check.Equals, uint64(0))
	c.Check(saved, check.HasLen, 0)
	c.Check(taskset, check.IsNil)
}

func (snapshotSuite) TestSaveSomeSnaps(c *check.C) {
	fakeSnapstateAll := func(*state.State) (map[string]*snapstate.SnapState, error) {
		return map[string]*snapstate.SnapState{
			"a-snap": {Active: true},
			"b-snap": {},
			"c-snap": {Active: true},
		}, nil
	}

	defer snapshotstate.MockSnapstateAll(fakeSnapstateAll)()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	snapshotOptions := map[string]*snap.SnapshotOptions{
		"a-snap": {Exclude: []string{"$SNAP_COMMON/exclude", "$SNAP_DATA/exclude"}},
	}

	setID, saved, taskset, err := snapshotstate.Save(st, nil, nil, snapshotOptions)
	c.Assert(err, check.IsNil)
	c.Check(setID, check.Equals, uint64(1))
	c.Check(saved, check.DeepEquals, []string{"a-snap", "c-snap"})
	tasks := taskset.Tasks()
	c.Assert(tasks, check.HasLen, 2)
	c.Check(tasks[0].Kind(), check.Equals, "save-snapshot")
	c.Check(tasks[0].Summary(), check.Equals, `Save data of snap "a-snap" in snapshot set #1`)

	var snapshot [2]map[string]interface{}
	c.Assert(tasks[0].Get("snapshot-setup", &snapshot[0]), check.IsNil)
	c.Check(snapshot[0], check.DeepEquals, map[string]interface{}{
		"set-id":  1.,
		"snap":    "a-snap",
		"options": map[string]interface{}{"exclude": []interface{}{"$SNAP_COMMON/exclude", "$SNAP_DATA/exclude"}},
		"current": "unset",
	})

	c.Check(tasks[1].Kind(), check.Equals, "save-snapshot")
	c.Check(tasks[1].Summary(), check.Equals, `Save data of snap "c-snap" in snapshot set #1`)
	c.Assert(tasks[1].Get("snapshot-setup", &snapshot[1]), check.IsNil)
	c.Check(snapshot[1], check.DeepEquals, map[string]interface{}{
		"set-id":  1.,
		"snap":    "c-snap",
		"current": "unset",
	})
}

func (s snapshotSuite) TestSaveOneSnap(c *check.C) {
	defer snapshotstate.MockSnapstateAll(func(*state.State) (map[string]*snapstate.SnapState, error) {
		// snapstate.All isn't called when a snap name is passed in
		return nil, errors.New("bzzt")
	})()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	snapstate.Set(st, "a-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "a-snap", Revision: snap.R(1)},
		}),
		Current: snap.R(1),
	})

	setID, saved, taskset, err := snapshotstate.Save(st, []string{"a-snap"}, []string{"a-user"}, nil)
	c.Assert(err, check.IsNil)
	c.Check(setID, check.Equals, uint64(1))
	c.Check(saved, check.DeepEquals, []string{"a-snap"})
	tasks := taskset.Tasks()
	c.Assert(tasks, check.HasLen, 1)
	c.Check(tasks[0].Kind(), check.Equals, "save-snapshot")
	c.Check(tasks[0].Summary(), check.Equals, `Save data of snap "a-snap" in snapshot set #1`)
	var snapshot map[string]interface{}
	c.Check(tasks[0].Get("snapshot-setup", &snapshot), check.IsNil)
	c.Check(snapshot, check.DeepEquals, map[string]interface{}{
		"set-id":  1.,
		"snap":    "a-snap",
		"users":   []interface{}{"a-user"},
		"current": "unset",
	})
}

func (snapshotSuite) TestSaveIntegration(c *check.C) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root (runuser will fail)")
	}

	c.Assert(os.MkdirAll(dirs.SnapshotsDir, 0755), check.IsNil)
	homedir := filepath.Join(dirs.GlobalRootDir, "home", "a-user")

	defer backend.MockUserLookup(func(username string) (*user.User, error) {
		if username != "a-user" {
			c.Fatalf("unexpected user %q", username)
		}
		return &user.User{
			Uid:      fmt.Sprint(sys.Geteuid()),
			Username: username,
			HomeDir:  homedir,
		}, nil
	})()

	o := overlord.Mock()
	st := o.State()

	stmgr, err := snapstate.Manager(st, o.TaskRunner())
	c.Assert(err, check.IsNil)
	o.AddManager(stmgr)
	shmgr := snapshotstate.Manager(st, o.TaskRunner())
	o.AddManager(shmgr)
	o.AddManager(o.TaskRunner())

	st.Lock()
	defer st.Unlock()

	snapshotOptions := map[string]*snap.SnapshotOptions{
		"one-snap": {Exclude: []string{"$SNAP_COMMON/exclude-a-snap", "$SNAP_DATA/exclude-a-snap"}},
		"tri-snap": {Exclude: []string{"$SNAP_COMMON/exclude-b-snap", "$SNAP_DATA/exclude-b-snap"}},
	}

	snapshots := make(map[string]*client.Snapshot, 3)
	for i, name := range []string{"one-snap", "too-snap", "tri-snap"} {
		sideInfo := &snap.SideInfo{RealName: name, Revision: snap.R(i + 1)}
		snapstate.Set(st, name, &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
			Current:  sideInfo.Revision,
			SnapType: "app",
		})
		snaptest.MockSnap(c, fmt.Sprintf("{name: %s, version: v1}", name), sideInfo)

		c.Assert(os.MkdirAll(filepath.Join(homedir, "snap", name, fmt.Sprint(i+1), "canary-"+name), 0755), check.IsNil)
		c.Assert(os.MkdirAll(filepath.Join(homedir, "snap", name, "common", "common-"+name), 0755), check.IsNil)

		snapshots[name] = &client.Snapshot{
			SetID:    1,
			Snap:     name,
			Version:  "v1",
			Revision: sideInfo.Revision,
			Epoch:    snap.E("0"),
			Options:  snapshotOptions[name],
		}
	}

	setID, saved, taskset, err := snapshotstate.Save(st, nil, []string{"a-user"}, snapshotOptions)
	c.Assert(err, check.IsNil)
	c.Check(setID, check.Equals, uint64(1))
	c.Check(saved, check.DeepEquals, []string{"one-snap", "too-snap", "tri-snap"})

	change := st.NewChange("save-snapshot", "...")
	change.AddAll(taskset)

	t0 := time.Now()

	st.Unlock()
	c.Assert(o.Settle(5*time.Second), check.IsNil)
	st.Lock()
	c.Check(change.Err(), check.IsNil)

	tf := time.Now()
	c.Assert(backend.Iter(context.TODO(), func(r *backend.Reader) error {
		c.Check(r.Check(context.TODO(), nil), check.IsNil)

		// check the unknowables, and zero them out
		c.Check(r.Snapshot.Time.After(t0), check.Equals, true)
		c.Check(r.Snapshot.Time.Before(tf), check.Equals, true)
		c.Check(r.Snapshot.Size > 0, check.Equals, true)
		c.Assert(r.Snapshot.SHA3_384, check.HasLen, 1)
		c.Check(r.Snapshot.SHA3_384["user/a-user.tgz"], check.HasLen, 96)

		r.Snapshot.Time = time.Time{}
		r.Snapshot.Size = 0
		r.Snapshot.SHA3_384 = nil

		c.Check(&r.Snapshot, check.DeepEquals, snapshots[r.Snapshot.Snap])
		return nil
	}), check.IsNil)
}

func (snapshotSuite) TestSaveIntegrationFails(c *check.C) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root (runuser will fail)")
	}
	c.Assert(os.MkdirAll(dirs.SnapshotsDir, 0755), check.IsNil)
	// precondition check: no files in snapshot dir
	out, err := exec.Command("find", dirs.SnapshotsDir, "-type", "f").CombinedOutput()
	c.Assert(err, check.IsNil)
	c.Check(string(out), check.Equals, "")

	homedir := filepath.Join(dirs.GlobalRootDir, "home", "a-user")

	// Mock "tar" so that the tars finish in the expected order.
	// Locally .01s and .02s do the trick with count=1000;
	// padded a lot bigger for slower systems.
	mocktar := testutil.MockCommand(c, "tar", `
case "$*" in
*/too-snap/*)
    sleep .5
    ;;
*/tri-snap/*)
    sleep 1
    ;;
esac
export LANG=C
exec /bin/tar "$@"
`)
	defer mocktar.Restore()

	defer backend.MockUserLookup(func(username string) (*user.User, error) {
		if username != "a-user" {
			c.Fatalf("unexpected user %q", username)
		}
		return &user.User{
			Uid:      fmt.Sprint(sys.Geteuid()),
			Username: username,
			HomeDir:  homedir,
		}, nil
	})()

	o := overlord.Mock()
	st := o.State()

	stmgr, err := snapstate.Manager(st, o.TaskRunner())
	c.Assert(err, check.IsNil)
	o.AddManager(stmgr)
	shmgr := snapshotstate.Manager(st, o.TaskRunner())
	o.AddManager(shmgr)
	o.AddManager(o.TaskRunner())

	st.Lock()
	defer st.Unlock()

	for i, name := range []string{"one-snap", "too-snap", "tri-snap"} {
		sideInfo := &snap.SideInfo{RealName: name, Revision: snap.R(i + 1)}
		snapstate.Set(st, name, &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
			Current:  sideInfo.Revision,
			SnapType: "app",
		})
		snaptest.MockSnap(c, fmt.Sprintf("{name: %s, version: v1}", name), sideInfo)

		c.Assert(os.MkdirAll(filepath.Join(homedir, "snap", name, fmt.Sprint(i+1), "canary-"+name), 0755), check.IsNil)
		c.Assert(os.MkdirAll(filepath.Join(homedir, "snap", name, "common"), 0755), check.IsNil)
		mode := os.FileMode(0755)
		if i == 1 {
			mode = 0
		}
		c.Assert(os.Mkdir(filepath.Join(homedir, "snap", name, "common", "common-"+name), mode), check.IsNil)
	}

	setID, saved, taskset, err := snapshotstate.Save(st, nil, []string{"a-user"}, nil)
	c.Assert(err, check.IsNil)
	c.Check(setID, check.Equals, uint64(1))
	c.Check(saved, check.DeepEquals, []string{"one-snap", "too-snap", "tri-snap"})

	change := st.NewChange("save-snapshot", "...")
	change.AddAll(taskset)

	st.Unlock()
	c.Assert(o.Settle(5*time.Second), check.IsNil)
	st.Lock()
	c.Check(change.Err(), check.NotNil)
	tasks := change.Tasks()
	c.Assert(tasks, check.HasLen, 3)

	// task 0 (for "one-snap") will have been undone
	c.Check(tasks[0].Summary(), testutil.Contains, `"one-snap"`) // validity check: task 0 is one-snap's
	c.Check(tasks[0].Status(), check.Equals, state.UndoneStatus)

	// task 1 (for "too-snap") will have errored
	c.Check(tasks[1].Summary(), testutil.Contains, `"too-snap"`) // validity check: task 1 is too-snap's
	c.Check(tasks[1].Status(), check.Equals, state.ErrorStatus)
	c.Check(strings.Join(tasks[1].Log(), "\n"), check.Matches, `(?ms)\S+ ERROR cannot create archive:
/bin/tar: common/common-too-snap: .* Permission denied
/bin/tar: Exiting with failure status due to previous errors`)

	// task 2 (for "tri-snap") will have errored as well, hopefully, but it's a race (see the "tar" comment above)
	c.Check(tasks[2].Summary(), testutil.Contains, `"tri-snap"`) // validity check: task 2 is tri-snap's
	c.Check(tasks[2].Status(), check.Equals, state.ErrorStatus, check.Commentf("if this ever fails, duplicate the fake tar sleeps please"))
	// sometimes you'll get one, sometimes you'll get the other (depending on ordering of events)
	c.Check(strings.Join(tasks[2].Log(), "\n"), check.Matches, `\S+ ERROR( tar failed:)? context canceled`)

	// no zips left behind, not for errors, not for undos \o/
	out, err = exec.Command("find", dirs.SnapshotsDir, "-type", "f").CombinedOutput()
	c.Assert(err, check.IsNil)
	c.Check(string(out), check.Equals, "")
}

func (snapshotSuite) testSaveIntegrationTarFails(c *check.C, tarLogLines int, expectedErr string) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root (runuser will fail)")
	}
	c.Assert(os.MkdirAll(dirs.SnapshotsDir, 0755), check.IsNil)
	// precondition check: no files in snapshot dir
	out, err := exec.Command("find", dirs.SnapshotsDir, "-type", "f").CombinedOutput()
	c.Assert(err, check.IsNil)
	c.Check(string(out), check.Equals, "")

	homedir := filepath.Join(dirs.GlobalRootDir, "home", "a-user")
	// mock tar so that it outputs a desired number of lines
	tarFailScript := fmt.Sprintf(`
export LANG=C
for c in $(seq %d); do echo "log line $c" >&2 ; done
exec /bin/tar "$@"
`, tarLogLines)
	if tarLogLines == 1 {
		tarFailScript = "echo nope >&2 ; exit 1"
	} else if tarLogLines == 0 {
		tarFailScript = "exit 1"
	}
	mocktar := testutil.MockCommand(c, "tar", tarFailScript)
	defer mocktar.Restore()

	defer backend.MockUserLookup(func(username string) (*user.User, error) {
		if username != "a-user" {
			c.Fatalf("unexpected user %q", username)
		}
		return &user.User{
			Uid:      fmt.Sprint(sys.Geteuid()),
			Username: username,
			HomeDir:  homedir,
		}, nil
	})()

	o := overlord.Mock()
	st := o.State()

	stmgr, err := snapstate.Manager(st, o.TaskRunner())
	c.Assert(err, check.IsNil)
	o.AddManager(stmgr)
	shmgr := snapshotstate.Manager(st, o.TaskRunner())
	o.AddManager(shmgr)
	o.AddManager(o.TaskRunner())

	st.Lock()
	defer st.Unlock()

	sideInfo := &snap.SideInfo{RealName: "tar-fail-snap", Revision: snap.R(1)}
	snapstate.Set(st, "tar-fail-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
		Current:  sideInfo.Revision,
		SnapType: "app",
	})
	snaptest.MockSnap(c, "name: tar-fail-snap\nversion: v1", sideInfo)
	c.Assert(os.MkdirAll(filepath.Join(homedir, "snap/tar-fail-snap/1/canary-tar-fail-snap"), 0755), check.IsNil)
	c.Assert(os.MkdirAll(filepath.Join(homedir, "snap/tar-fail-snap/common"), 0755), check.IsNil)
	// these dir permissions (000) make tar unhappy
	c.Assert(os.Mkdir(filepath.Join(homedir, "snap/tar-fail-snap/common/common-tar-fail-snap"), 00), check.IsNil)

	setID, saved, taskset, err := snapshotstate.Save(st, nil, []string{"a-user"}, nil)
	c.Assert(err, check.IsNil)
	c.Check(setID, check.Equals, uint64(1))
	c.Check(saved, check.DeepEquals, []string{"tar-fail-snap"})

	change := st.NewChange("save-snapshot", "...")
	change.AddAll(taskset)

	st.Unlock()
	c.Assert(o.Settle(testutil.HostScaledTimeout(5*time.Second)), check.IsNil)
	st.Lock()
	c.Check(change.Err(), check.NotNil)
	tasks := change.Tasks()
	c.Assert(tasks, check.HasLen, 1)

	// task 1 (for "too-snap") will have errored
	c.Check(tasks[0].Summary(), testutil.Contains, `"tar-fail-snap"`) // validity check: task 1 is too-snap's
	c.Check(tasks[0].Status(), check.Equals, state.ErrorStatus)
	c.Check(strings.Join(tasks[0].Log(), "\n"), check.Matches, expectedErr)

	// no zips left behind, not for errors, not for undos \o/
	out, err = exec.Command("find", dirs.SnapshotsDir, "-type", "f").CombinedOutput()
	c.Assert(err, check.IsNil)
	c.Check(string(out), check.Equals, "")
}

func (s *snapshotSuite) TestSaveIntegrationTarFailsManyLines(c *check.C) {
	// cutoff at 5 lines, 3 lines of log + 2 lines from tar
	s.testSaveIntegrationTarFails(c, 3, `(?ms)\S+ ERROR cannot create archive:
log line 1
log line 2
log line 3
/bin/tar: common/common-tar-fail-snap: .* Permission denied
/bin/tar: Exiting with failure status due to previous errors`)
}

func (s *snapshotSuite) TestSaveIntegrationTarFailsTrimmedLines(c *check.C) {
	s.testSaveIntegrationTarFails(c, 10, `(?ms)\S+ ERROR cannot create archive \(showing last 5 lines out of 12\):
log line 8
log line 9
log line 10
/bin/tar: common/common-tar-fail-snap: .* Permission denied
/bin/tar: Exiting with failure status due to previous errors`)
}

func (s *snapshotSuite) TestSaveIntegrationTarFailsSingleLine(c *check.C) {
	s.testSaveIntegrationTarFails(c, 1, `(?ms)\S+ ERROR cannot create archive:
nope`)
}

func (s *snapshotSuite) TestSaveIntegrationTarFailsNoLines(c *check.C) {
	s.testSaveIntegrationTarFails(c, 0, `(?ms)\S+ ERROR tar failed: exit status 1`)
}

func (snapshotSuite) TestRestoreChecksIterError(c *check.C) {
	defer snapshotstate.MockBackendIter(func(context.Context, func(*backend.Reader) error) error {
		return errors.New("bzzt")
	})()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, _, err := snapshotstate.Restore(st, 42, nil, nil)
	c.Assert(err, check.ErrorMatches, "bzzt")
}

func (s snapshotSuite) TestRestoreChecksSnapstateConflicts(c *check.C) {
	st, restore := s.createConflictingChange(c)
	defer restore()

	_, _, err := snapshotstate.Restore(st, 42, nil, nil)
	c.Assert(err, check.NotNil)
	c.Check(err, check.FitsTypeOf, &snapstate.ChangeConflictError{})

}

func (snapshotSuite) TestRestoreConflictsWithSnapstate(c *check.C) {
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "foo.zip"))
	c.Assert(err, check.IsNil)
	defer shotfile.Close()

	sideInfo := &snap.SideInfo{RealName: "foo", Revision: snap.R(1)}
	fakeSnapstateAll := func(*state.State) (map[string]*snapstate.SnapState, error) {
		return map[string]*snapstate.SnapState{
			"foo": {
				Active:   true,
				Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
				Current:  sideInfo.Revision,
			},
		}, nil
	}
	snaptest.MockSnap(c, "{name: foo, version: v1}", sideInfo)

	defer snapshotstate.MockSnapstateAll(fakeSnapstateAll)()

	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			Snapshot: client.Snapshot{SetID: 42, Snap: "foo"},
			File:     shotfile,
		}), check.IsNil)

		return nil
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	o := overlord.Mock()
	st := o.State()

	stmgr, err := snapstate.Manager(st, o.TaskRunner())
	c.Assert(err, check.IsNil)
	o.AddManager(stmgr)
	shmgr := snapshotstate.Manager(st, o.TaskRunner())
	o.AddManager(shmgr)

	st.Lock()
	defer st.Unlock()

	snapstate.Set(st, "foo", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	chg := st.NewChange("snapshot-restore", "...")
	_, restoreTasks, err := snapshotstate.Restore(st, 42, nil, nil)
	c.Assert(err, check.IsNil)
	chg.AddAll(restoreTasks)

	_, err = snapstate.Disable(st, "foo")
	c.Assert(err, check.ErrorMatches, `snap "foo" has "snapshot-restore" change in progress`)
}

func (snapshotSuite) TestRestoreChecksForgetConflicts(c *check.C) {
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "yadda.zip"))
	c.Assert(err, check.IsNil)
	defer shotfile.Close()
	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			// not wanted
			Snapshot: client.Snapshot{SetID: 42, Snap: "a-snap"},
			File:     shotfile,
		}), check.IsNil)

		return nil
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	chg := st.NewChange("forget-snapshot-change", "...")
	tsk := st.NewTask("forget-snapshot", "...")
	tsk.SetStatus(state.DoingStatus)
	tsk.Set("snapshot-setup", map[string]int{"set-id": 42})
	chg.AddTask(tsk)

	_, _, err = snapshotstate.Restore(st, 42, nil, nil)
	c.Assert(err, check.ErrorMatches, `cannot operate on snapshot set #42 while change \"1\" is in progress`)
}

func (snapshotSuite) TestRestoreChecksChangesToSnapID(c *check.C) {
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "yadda.zip"))
	c.Assert(err, check.IsNil)
	defer shotfile.Close()
	fakeSnapstateAll := func(*state.State) (map[string]*snapstate.SnapState, error) {
		return map[string]*snapstate.SnapState{
			"a-snap": {
				Active: true,
				Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
					{RealName: "a-snap", Revision: snap.R(1), SnapID: "1234567890"},
				}),
				Current: snap.R(1),
			},
		}, nil
	}
	defer snapshotstate.MockSnapstateAll(fakeSnapstateAll)()
	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			// not wanted
			Snapshot: client.Snapshot{SetID: 42, Snap: "a-snap", SnapID: "0987654321"},
			File:     shotfile,
		}), check.IsNil)

		return nil
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, _, err = snapshotstate.Restore(st, 42, nil, nil)
	c.Assert(err, check.ErrorMatches, `cannot restore snapshot for "a-snap": current snap \(ID 1234567…\) does not match snapshot \(ID 0987654…\)`)
}

func (snapshotSuite) TestRestoreChecksChangesToEpoch(c *check.C) {
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "yadda.zip"))
	c.Assert(err, check.IsNil)
	defer shotfile.Close()

	sideInfo := &snap.SideInfo{RealName: "a-snap", Revision: snap.R(1)}
	fakeSnapstateAll := func(*state.State) (map[string]*snapstate.SnapState, error) {
		return map[string]*snapstate.SnapState{
			"a-snap": {
				Active:   true,
				Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
				Current:  sideInfo.Revision,
			},
		}, nil
	}
	defer snapshotstate.MockSnapstateAll(fakeSnapstateAll)()
	snaptest.MockSnap(c, "{name: a-snap, version: v1, epoch: 17}", sideInfo)

	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			// not wanted
			Snapshot: client.Snapshot{
				SetID: 42,
				Snap:  "a-snap",
				Epoch: snap.E("42"),
			},
			File: shotfile,
		}), check.IsNil)

		return nil
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, _, err = snapshotstate.Restore(st, 42, nil, nil)
	c.Assert(err, check.ErrorMatches, `cannot restore snapshot for "a-snap": current snap \(epoch 17\) cannot read snapshot data \(epoch 42\)`)
}

func (snapshotSuite) TestRestoreWorksWithCompatibleEpoch(c *check.C) {
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "yadda.zip"))
	c.Assert(err, check.IsNil)
	defer shotfile.Close()

	sideInfo := &snap.SideInfo{RealName: "a-snap", Revision: snap.R(1)}
	fakeSnapstateAll := func(*state.State) (map[string]*snapstate.SnapState, error) {
		return map[string]*snapstate.SnapState{
			"a-snap": {
				Active:   true,
				Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
				Current:  sideInfo.Revision,
			},
		}, nil
	}
	defer snapshotstate.MockSnapstateAll(fakeSnapstateAll)()
	snaptest.MockSnap(c, "{name: a-snap, version: v1, epoch: {read: [17, 42], write: [42]}}", sideInfo)

	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			// not wanted
			Snapshot: client.Snapshot{
				SetID: 42,
				Snap:  "a-snap",
				Epoch: snap.E("17"),
			},
			File: shotfile,
		}), check.IsNil)

		return nil
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	found, taskset, err := snapshotstate.Restore(st, 42, nil, nil)
	c.Assert(err, check.IsNil)
	c.Check(found, check.DeepEquals, []string{"a-snap"})
	tasks := taskset.Tasks()
	c.Assert(tasks, check.HasLen, 2)
	c.Check(tasks[0].Kind(), check.Equals, "restore-snapshot")
	c.Check(tasks[1].Kind(), check.Equals, "cleanup-after-restore")
	c.Check(tasks[0].Summary(), check.Equals, `Restore data of snap "a-snap" from snapshot set #42`)
	var snapshot map[string]interface{}
	c.Check(tasks[0].Get("snapshot-setup", &snapshot), check.IsNil)
	c.Check(snapshot, check.DeepEquals, map[string]interface{}{
		"set-id":   42.,
		"snap":     "a-snap",
		"filename": shotfile.Name(),
		"current":  "1",
	})
}

func (snapshotSuite) TestRestore(c *check.C) {
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "yadda.zip"))
	c.Assert(err, check.IsNil)
	defer shotfile.Close()
	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			// not wanted
			Snapshot: client.Snapshot{SetID: 42, Snap: "a-snap"},
			File:     shotfile,
		}), check.IsNil)

		return nil
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	found, taskset, err := snapshotstate.Restore(st, 42, []string{"a-snap", "b-snap"}, []string{"a-user"})
	c.Assert(err, check.IsNil)
	c.Check(found, check.DeepEquals, []string{"a-snap"})
	tasks := taskset.Tasks()
	c.Assert(tasks, check.HasLen, 2)
	c.Check(tasks[0].Kind(), check.Equals, "restore-snapshot")
	c.Check(tasks[1].Kind(), check.Equals, "cleanup-after-restore")
	c.Check(tasks[0].Summary(), check.Equals, `Restore data of snap "a-snap" from snapshot set #42`)
	var snapshot map[string]interface{}
	c.Check(tasks[0].Get("snapshot-setup", &snapshot), check.IsNil)
	c.Check(snapshot, check.DeepEquals, map[string]interface{}{
		"set-id":   42.,
		"snap":     "a-snap",
		"filename": shotfile.Name(),
		"users":    []interface{}{"a-user"},
		"current":  "unset",
	})
}

func (snapshotSuite) TestRestoreIntegration(c *check.C) {
	testRestoreIntegration(c, dirs.UserHomeSnapDir, nil)
}

func (snapshotSuite) TestRestoreIntegrationHiddenSnapDir(c *check.C) {
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	restore := snapshotstate.MockGetSnapDirOptions(func(*state.State, string) (*dirs.SnapDirOptions, error) {
		return opts, nil
	})
	defer restore()

	testRestoreIntegration(c, dirs.HiddenSnapDataHomeDir, opts)
}

func testRestoreIntegration(c *check.C, snapDataDir string, opts *dirs.SnapDirOptions) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root (runuser will fail)")
	}

	c.Assert(os.MkdirAll(dirs.SnapshotsDir, 0755), check.IsNil)
	homedirA := filepath.Join(dirs.GlobalRootDir, "home", "a-user")
	homedirB := filepath.Join(dirs.GlobalRootDir, "home", "b-user")

	defer backend.MockUserLookup(func(username string) (*user.User, error) {
		if username != "a-user" && username != "b-user" {
			c.Fatalf("unexpected user %q", username)
			return nil, user.UnknownUserError(username)
		}
		return &user.User{
			Uid:      fmt.Sprint(sys.Geteuid()),
			Username: username,
			HomeDir:  filepath.Join(dirs.GlobalRootDir, "home", username),
		}, nil

	})()

	o := overlord.Mock()
	st := o.State()

	stmgr, err := snapstate.Manager(st, o.TaskRunner())
	c.Assert(err, check.IsNil)
	o.AddManager(stmgr)
	shmgr := snapshotstate.Manager(st, o.TaskRunner())
	o.AddManager(shmgr)
	o.AddManager(o.TaskRunner())

	st.Lock()

	for i, name := range []string{"one-snap", "too-snap", "tri-snap"} {
		sideInfo := &snap.SideInfo{RealName: name, Revision: snap.R(i + 1)}
		snapstate.Set(st, name, &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
			Current:  sideInfo.Revision,
			SnapType: "app",
		})
		snapInfo := snaptest.MockSnap(c, fmt.Sprintf("{name: %s, version: v1}", name), sideInfo)

		for _, home := range []string{homedirA, homedirB} {
			c.Assert(os.MkdirAll(filepath.Join(home, snapDataDir, name, fmt.Sprint(i+1), "canary-"+name), 0755), check.IsNil)
			c.Assert(os.MkdirAll(filepath.Join(home, snapDataDir, name, "common", "common-"+name), 0755), check.IsNil)
		}

		_, err := backend.Save(context.TODO(), 42, snapInfo, nil, []string{"a-user", "b-user"}, nil, opts)
		c.Assert(err, check.IsNil)
	}

	// move the old away
	c.Assert(os.Rename(filepath.Join(homedirA, snapDataDir), filepath.Join(homedirA, "snap.old")), check.IsNil)
	// remove b-user's home
	c.Assert(os.RemoveAll(homedirB), check.IsNil)

	found, taskset, err := snapshotstate.Restore(st, 42, nil, []string{"a-user", "b-user"})
	c.Assert(err, check.IsNil)
	sort.Strings(found)
	c.Check(found, check.DeepEquals, []string{"one-snap", "too-snap", "tri-snap"})

	change := st.NewChange("restore-snapshot", "...")
	change.AddAll(taskset)

	st.Unlock()
	c.Assert(o.Settle(5*time.Second), check.IsNil)
	st.Lock()
	c.Check(change.Err(), check.IsNil)
	defer st.Unlock()

	// the three restores warn about the missing home (but no errors, no panics)
	c.Assert(change.Tasks(), check.HasLen, 4)
	restoreTasks := change.Tasks()[:3]
	for _, task := range restoreTasks {
		c.Check(strings.Join(task.Log(), "\n"), check.Matches, `.* Skipping restore of "[^"]+/home/b-user/[^"]+" as "[^"]+/home/b-user" doesn't exist.`)
	}

	// check it was all brought back \o/
	out, err := exec.Command("diff", "-rN", filepath.Join(homedirA, snapDataDir), filepath.Join("snap.old")).CombinedOutput()
	c.Check(err, check.IsNil)
	c.Check(string(out), check.Equals, "")
}

func (snapshotSuite) TestRestoreIntegrationFails(c *check.C) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root (runuser will fail)")
	}
	c.Assert(os.MkdirAll(dirs.SnapshotsDir, 0755), check.IsNil)
	homedir := filepath.Join(dirs.GlobalRootDir, "home", "a-user")

	defer backend.MockUserLookup(func(username string) (*user.User, error) {
		if username != "a-user" {
			c.Fatalf("unexpected user %q", username)
		}
		return &user.User{
			Uid:      fmt.Sprint(sys.Geteuid()),
			Username: username,
			HomeDir:  homedir,
		}, nil
	})()

	o := overlord.Mock()
	st := o.State()

	stmgr, err := snapstate.Manager(st, o.TaskRunner())
	c.Assert(err, check.IsNil)
	o.AddManager(stmgr)
	shmgr := snapshotstate.Manager(st, o.TaskRunner())
	o.AddManager(shmgr)
	o.AddManager(o.TaskRunner())

	st.Lock()

	for i, name := range []string{"one-snap", "too-snap", "tri-snap"} {
		sideInfo := &snap.SideInfo{RealName: name, Revision: snap.R(i + 1)}
		snapstate.Set(st, name, &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
			Current:  sideInfo.Revision,
			SnapType: "app",
		})
		snapInfo := snaptest.MockSnap(c, fmt.Sprintf("{name: %s, version: vv1}", name), sideInfo)

		c.Assert(os.MkdirAll(filepath.Join(homedir, "snap", name, fmt.Sprint(i+1), "canary-"+name), 0755), check.IsNil)
		c.Assert(os.MkdirAll(filepath.Join(homedir, "snap", name, "common", "common-"+name), 0755), check.IsNil)

		_, err := backend.Save(context.TODO(), 42, snapInfo, nil, []string{"a-user"}, nil, nil)
		c.Assert(err, check.IsNil)
	}

	// move the old away
	c.Assert(os.Rename(filepath.Join(homedir, "snap"), filepath.Join(homedir, "snap.old")), check.IsNil)
	// but poison the well
	c.Assert(os.MkdirAll(filepath.Join(homedir, "snap"), 0755), check.IsNil)
	c.Assert(os.MkdirAll(filepath.Join(homedir, "snap", "too-snap"), 0), check.IsNil)

	found, taskset, err := snapshotstate.Restore(st, 42, nil, []string{"a-user"})
	c.Assert(err, check.IsNil)
	sort.Strings(found)
	c.Check(found, check.DeepEquals, []string{"one-snap", "too-snap", "tri-snap"})

	change := st.NewChange("restore-snapshot", "...")
	change.AddAll(taskset)

	st.Unlock()
	c.Assert(o.Settle(5*time.Second), check.IsNil)
	st.Lock()
	c.Check(change.Err(), check.NotNil)
	defer st.Unlock()

	tasks := change.Tasks()
	c.Check(tasks, check.HasLen, 4)
	restoreTasks := tasks[:3]
	for _, task := range restoreTasks {
		if strings.Contains(task.Summary(), `"too-snap"`) {
			// too-snap was set up to fail, should always fail with
			// 'permission denied' (see the mkdirall w/mode 0 above)
			c.Check(task.Status(), check.Equals, state.ErrorStatus)
			c.Check(strings.Join(task.Log(), "\n"), check.Matches, `\S+ ERROR mkdir \S+: permission denied`)
		} else {
			// the other two might fail (ErrorStatus) if they're
			// still running when too-snap fails, or they might have
			// finished and needed to be undone (UndoneStatus); it's
			// a race, but either is fine.
			if task.Status() == state.ErrorStatus {
				c.Check(strings.Join(task.Log(), "\n"), check.Matches, `\S+ ERROR.* context canceled`)
			} else {
				c.Check(task.Status(), check.Equals, state.UndoneStatus)
			}
		}
	}

	// remove the poison
	c.Assert(os.Remove(filepath.Join(homedir, "snap", "too-snap")), check.IsNil)

	// check that nothing else was put there
	out, err := exec.Command("find", filepath.Join(homedir, "snap")).CombinedOutput()
	c.Assert(err, check.IsNil)
	c.Check(strings.TrimSpace(string(out)), check.Equals, filepath.Join(homedir, "snap"))
}

func (snapshotSuite) TestCheckChecksIterError(c *check.C) {
	defer snapshotstate.MockBackendIter(func(context.Context, func(*backend.Reader) error) error {
		return errors.New("bzzt")
	})()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, _, err := snapshotstate.Check(st, 42, nil, nil)
	c.Assert(err, check.ErrorMatches, "bzzt")
}

func (s snapshotSuite) TestCheckDoesNotTriggerSnapstateConflict(c *check.C) {
	st, restore := s.createConflictingChange(c)
	defer restore()

	_, _, err := snapshotstate.Check(st, 42, nil, nil)
	c.Assert(err, check.IsNil)
}

func (snapshotSuite) TestCheckChecksForgetConflicts(c *check.C) {
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "yadda.zip"))
	c.Assert(err, check.IsNil)
	defer shotfile.Close()
	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			// not wanted
			Snapshot: client.Snapshot{SetID: 42, Snap: "a-snap"},
			File:     shotfile,
		}), check.IsNil)

		return nil
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	chg := st.NewChange("forget-snapshot-change", "...")
	tsk := st.NewTask("forget-snapshot", "...")
	tsk.SetStatus(state.DoingStatus)
	tsk.Set("snapshot-setup", map[string]int{"set-id": 42})
	chg.AddTask(tsk)

	_, _, err = snapshotstate.Check(st, 42, nil, nil)
	c.Assert(err, check.ErrorMatches, `cannot operate on snapshot set #42 while change \"1\" is in progress`)
}

func (snapshotSuite) TestCheck(c *check.C) {
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "yadda.zip"))
	c.Assert(err, check.IsNil)
	defer shotfile.Close()
	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			// not wanted
			Snapshot: client.Snapshot{SetID: 42, Snap: "a-snap"},
			File:     shotfile,
		}), check.IsNil)

		return nil
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	found, taskset, err := snapshotstate.Check(st, 42, []string{"a-snap", "b-snap"}, []string{"a-user"})
	c.Assert(err, check.IsNil)
	c.Check(found, check.DeepEquals, []string{"a-snap"})
	tasks := taskset.Tasks()
	c.Assert(tasks, check.HasLen, 1)
	c.Check(tasks[0].Kind(), check.Equals, "check-snapshot")
	c.Check(tasks[0].Summary(), check.Equals, `Check data of snap "a-snap" in snapshot set #42`)
	var snapshot map[string]interface{}
	c.Check(tasks[0].Get("snapshot-setup", &snapshot), check.IsNil)
	c.Check(snapshot, check.DeepEquals, map[string]interface{}{
		"set-id":   42.,
		"snap":     "a-snap",
		"filename": shotfile.Name(),
		"users":    []interface{}{"a-user"},
		"current":  "unset",
	})
}

func (snapshotSuite) TestForgetChecksIterError(c *check.C) {
	defer snapshotstate.MockBackendIter(func(context.Context, func(*backend.Reader) error) error {
		return errors.New("bzzt")
	})()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, _, err := snapshotstate.Forget(st, 42, nil)
	c.Assert(err, check.ErrorMatches, "bzzt")
}

func (s snapshotSuite) TestForgetDoesNotTriggerSnapstateConflict(c *check.C) {
	st, restore := s.createConflictingChange(c)
	defer restore()

	_, _, err := snapshotstate.Forget(st, 42, nil)
	c.Assert(err, check.IsNil)
}

func (snapshotSuite) TestForgetChecksCheckConflicts(c *check.C) {
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "yadda.zip"))
	c.Assert(err, check.IsNil)
	defer shotfile.Close()
	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			// not wanted
			Snapshot: client.Snapshot{SetID: 42, Snap: "a-snap"},
			File:     shotfile,
		}), check.IsNil)

		return nil
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	chg := st.NewChange("check-snapshot-change", "...")
	tsk := st.NewTask("check-snapshot", "...")
	tsk.SetStatus(state.DoingStatus)
	tsk.Set("snapshot-setup", map[string]int{"set-id": 42})
	chg.AddTask(tsk)

	_, _, err = snapshotstate.Forget(st, 42, nil)
	c.Assert(err, check.ErrorMatches, `cannot operate on snapshot set #42 while change \"1\" is in progress`)
}

func (snapshotSuite) TestForgetChecksExportConflicts(c *check.C) {
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "yadda.zip"))
	c.Assert(err, check.IsNil)
	defer shotfile.Close()
	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			Snapshot: client.Snapshot{SetID: 42, Snap: "a-snap"},
			File:     shotfile,
		}), check.IsNil)

		return nil
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	snapshotstate.SetSnapshotOpInProgress(st, 42, "export-snapshot")

	_, _, err = snapshotstate.Forget(st, 42, nil)
	c.Assert(err, check.ErrorMatches, `cannot operate on snapshot set #42 while operation export-snapshot is in progress`)
}

func (snapshotSuite) TestForgetChecksRestoreConflicts(c *check.C) {
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "yadda.zip"))
	c.Assert(err, check.IsNil)
	defer shotfile.Close()
	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			// not wanted
			Snapshot: client.Snapshot{SetID: 42, Snap: "a-snap"},
			File:     shotfile,
		}), check.IsNil)

		return nil
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	chg := st.NewChange("restore-snapshot-change", "...")
	tsk := st.NewTask("restore-snapshot", "...")
	tsk.SetStatus(state.DoingStatus)
	tsk.Set("snapshot-setup", map[string]int{"set-id": 42})
	chg.AddTask(tsk)

	_, _, err = snapshotstate.Forget(st, 42, nil)
	c.Assert(err, check.ErrorMatches, `cannot operate on snapshot set #42 while change \"1\" is in progress`)
}

func (snapshotSuite) TestForget(c *check.C) {
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "yadda.zip"))
	c.Assert(err, check.IsNil)
	defer shotfile.Close()
	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			// not wanted
			Snapshot: client.Snapshot{SetID: 42, Snap: "a-snap"},
			File:     shotfile,
		}), check.IsNil)

		return nil
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	found, taskset, err := snapshotstate.Forget(st, 42, []string{"a-snap", "b-snap"})
	c.Assert(err, check.IsNil)
	c.Check(found, check.DeepEquals, []string{"a-snap"})
	tasks := taskset.Tasks()
	c.Assert(tasks, check.HasLen, 1)
	c.Check(tasks[0].Kind(), check.Equals, "forget-snapshot")
	c.Check(tasks[0].Summary(), check.Equals, `Drop data of snap "a-snap" from snapshot set #42`)
	var snapshot map[string]interface{}
	c.Check(tasks[0].Get("snapshot-setup", &snapshot), check.IsNil)
	c.Check(snapshot, check.DeepEquals, map[string]interface{}{
		"set-id":   42.,
		"snap":     "a-snap",
		"filename": shotfile.Name(),
		"current":  "unset",
	})
}

func (snapshotSuite) TestSaveExpiration(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	var expirations map[uint64]interface{}
	tm, err := time.Parse(time.RFC3339, "2019-03-11T11:24:00Z")
	c.Assert(err, check.IsNil)
	c.Assert(snapshotstate.SaveExpiration(st, 12, tm), check.IsNil)

	tm, err = time.Parse(time.RFC3339, "2019-02-12T12:50:00Z")
	c.Assert(err, check.IsNil)
	c.Assert(snapshotstate.SaveExpiration(st, 13, tm), check.IsNil)

	c.Assert(st.Get("snapshots", &expirations), check.IsNil)
	c.Check(expirations, check.DeepEquals, map[uint64]interface{}{
		12: map[string]interface{}{"expiry-time": "2019-03-11T11:24:00Z"},
		13: map[string]interface{}{"expiry-time": "2019-02-12T12:50:00Z"},
	})
}

func (snapshotSuite) TestRemoveSnapshotState(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	st.Set("snapshots", map[uint64]interface{}{
		12: map[string]interface{}{"expiry-time": "2019-01-11T11:11:00Z"},
		13: map[string]interface{}{"expiry-time": "2019-02-12T12:11:00Z"},
		14: map[string]interface{}{"expiry-time": "2019-03-12T13:11:00Z"},
	})

	snapshotstate.RemoveSnapshotState(st, 12, 14)

	var snapshots map[uint64]interface{}
	c.Assert(st.Get("snapshots", &snapshots), check.IsNil)
	c.Check(snapshots, check.DeepEquals, map[uint64]interface{}{
		13: map[string]interface{}{"expiry-time": "2019-02-12T12:11:00Z"},
	})
}

func (snapshotSuite) TestExpiredSnapshotSets(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	tm, err := time.Parse(time.RFC3339, "2019-03-11T11:24:00Z")
	c.Assert(err, check.IsNil)
	c.Assert(snapshotstate.SaveExpiration(st, 12, tm), check.IsNil)

	tm, err = time.Parse(time.RFC3339, "2019-02-12T12:50:00Z")
	c.Assert(err, check.IsNil)
	c.Assert(snapshotstate.SaveExpiration(st, 13, tm), check.IsNil)

	tm, err = time.Parse(time.RFC3339, "2020-03-11T11:24:00Z")
	c.Assert(err, check.IsNil)
	expired, err := snapshotstate.ExpiredSnapshotSets(st, tm)
	c.Assert(err, check.IsNil)
	c.Check(expired, check.DeepEquals, map[uint64]bool{12: true, 13: true})

	tm, err = time.Parse(time.RFC3339, "2019-03-01T11:24:00Z")
	c.Assert(err, check.IsNil)
	expired, err = snapshotstate.ExpiredSnapshotSets(st, tm)
	c.Assert(err, check.IsNil)
	c.Check(expired, check.DeepEquals, map[uint64]bool{13: true})
}

func (snapshotSuite) TestAutomaticSnapshotDisabled(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	tr := config.NewTransaction(st)
	tr.Set("core", "snapshots.automatic.retention", "no")
	tr.Commit()

	_, err := snapshotstate.AutomaticSnapshot(st, "foo")
	c.Assert(err, check.Equals, snapstate.ErrNothingToDo)
}

func (snapshotSuite) TestAutomaticSnapshot(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	tr := config.NewTransaction(st)
	tr.Set("core", "snapshots.automatic.retention", "24h")
	tr.Commit()

	ts, err := snapshotstate.AutomaticSnapshot(st, "foo")
	c.Assert(err, check.IsNil)

	tasks := ts.Tasks()
	c.Assert(tasks, check.HasLen, 1)
	c.Check(tasks[0].Kind(), check.Equals, "save-snapshot")
	c.Check(tasks[0].Summary(), check.Equals, `Save data of snap "foo" in automatic snapshot set #1`)
	var snapshot map[string]interface{}
	c.Check(tasks[0].Get("snapshot-setup", &snapshot), check.IsNil)
	c.Check(snapshot, check.DeepEquals, map[string]interface{}{
		"set-id":  1.,
		"snap":    "foo",
		"current": "unset",
		"auto":    true,
	})
}

func (snapshotSuite) TestAutomaticSnapshotDefaultClassic(c *check.C) {
	release.MockOnClassic(true)

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	du, err := snapshotstate.AutomaticSnapshotExpiration(st)
	c.Assert(err, check.IsNil)
	c.Assert(du, check.Equals, snapshotstate.DefaultAutomaticSnapshotExpiration)
}

func (snapshotSuite) TestAutomaticSnapshotDefaultUbuntuCore(c *check.C) {
	release.MockOnClassic(false)

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	du, err := snapshotstate.AutomaticSnapshotExpiration(st)
	c.Assert(err, check.IsNil)
	c.Assert(du, check.Equals, time.Duration(0))
}

func (snapshotSuite) TestListError(c *check.C) {
	restore := snapshotstate.MockBackendList(func(context.Context, uint64, []string) ([]client.SnapshotSet, error) {
		return nil, fmt.Errorf("boom")
	})
	defer restore()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, err := snapshotstate.List(context.TODO(), st, 0, nil)
	c.Assert(err, check.ErrorMatches, "boom")
}

func (snapshotSuite) TestListSetsAutoFlag(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	st.Set("snapshots", map[uint64]interface{}{
		1: map[string]interface{}{"expiry-time": "2019-01-11T11:11:00Z"},
		2: map[string]interface{}{"expiry-time": "2019-02-12T12:11:00Z"},
	})

	restore := snapshotstate.MockBackendList(func(ctx context.Context, setID uint64, snapNames []string) ([]client.SnapshotSet, error) {
		// three sets, first two are automatic (implied by expiration times in the state), the third isn't.
		return []client.SnapshotSet{
			{
				ID: 1,
				Snapshots: []*client.Snapshot{
					{
						Snap:  "foo",
						SetID: 1,
					},
					{
						Snap:  "bar",
						SetID: 1,
					},
				},
			},
			{
				ID: 2,
				Snapshots: []*client.Snapshot{
					{
						Snap:  "baz",
						SetID: 2,
					},
				},
			},
			{
				ID: 3,
				Snapshots: []*client.Snapshot{
					{
						Snap:  "baz",
						SetID: 3,
					},
				},
			},
		}, nil
	})
	defer restore()

	sets, err := snapshotstate.List(context.TODO(), st, 0, nil)
	c.Assert(err, check.IsNil)
	c.Assert(sets, check.HasLen, 3)

	for _, sset := range sets {
		switch sset.ID {
		case 1:
			c.Check(sset.Snapshots, check.HasLen, 2, check.Commentf("set #%d", sset.ID))
		default:
			c.Check(sset.Snapshots, check.HasLen, 1, check.Commentf("set #%d", sset.ID))
		}

		switch sset.ID {
		case 1, 2:
			for _, snapshot := range sset.Snapshots {
				c.Check(snapshot.Auto, check.Equals, true)
			}
		default:
			for _, snapshot := range sset.Snapshots {
				c.Check(snapshot.Auto, check.Equals, false)
			}
		}
	}
}

func (snapshotSuite) TestImportSnapshotHappy(c *check.C) {
	st := state.New(nil)

	fakeSnapNames := []string{"baz", "bar", "foo"}
	fakeSnapshotData := "fake-import-data"

	buf := bytes.NewBufferString(fakeSnapshotData)
	restore := snapshotstate.MockBackendImport(func(ctx context.Context, id uint64, r io.Reader, flags *backend.ImportFlags) ([]string, error) {
		d, err := io.ReadAll(r)
		c.Assert(err, check.IsNil)
		c.Check(fakeSnapshotData, check.Equals, string(d))
		return fakeSnapNames, nil
	})
	defer restore()

	sid, names, err := snapshotstate.Import(context.TODO(), st, buf)
	c.Assert(err, check.IsNil)
	c.Check(sid, check.Equals, uint64(1))
	c.Check(names, check.DeepEquals, fakeSnapNames)
}

func (snapshotSuite) TestImportSnapshotImportError(c *check.C) {
	st := state.New(nil)

	restore := snapshotstate.MockBackendImport(func(ctx context.Context, id uint64, r io.Reader, flags *backend.ImportFlags) ([]string, error) {
		return nil, errors.New("some-error")
	})
	defer restore()

	r := bytes.NewBufferString("faked-import-data")
	sid, _, err := snapshotstate.Import(context.TODO(), st, r)
	c.Assert(err, check.NotNil)
	c.Assert(err.Error(), check.Equals, "some-error")
	c.Check(sid, check.Equals, uint64(0))
}

func (snapshotSuite) TestImportSnapshotDuplicate(c *check.C) {
	st := state.New(nil)

	restore := snapshotstate.MockBackendImport(func(ctx context.Context, id uint64, r io.Reader, flags *backend.ImportFlags) ([]string, error) {
		return nil, backend.DuplicatedSnapshotImportError{SetID: 3, SnapNames: []string{"foo-snap"}}
	})
	defer restore()

	st.Lock()
	st.Set("snapshots", map[uint64]interface{}{
		2: map[string]interface{}{"expiry-time": "2019-01-11T11:11:00Z"},
		3: map[string]interface{}{"expiry-time": "2019-02-12T12:11:00Z"},
	})
	st.Unlock()

	sid, snapNames, err := snapshotstate.Import(context.TODO(), st, bytes.NewBufferString(""))
	c.Assert(err, check.IsNil)
	c.Check(sid, check.Equals, uint64(3))
	c.Check(snapNames, check.DeepEquals, []string{"foo-snap"})

	st.Lock()
	defer st.Unlock()
	// expiry-time has been removed for snapshot set 3
	var snapshots map[uint64]interface{}
	c.Assert(st.Get("snapshots", &snapshots), check.IsNil)
	c.Check(snapshots, check.DeepEquals, map[uint64]interface{}{
		2: map[string]interface{}{"expiry-time": "2019-01-11T11:11:00Z"},
	})
}

func (snapshotSuite) TestEstimateSnapshotSize(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	sideInfo := &snap.SideInfo{RealName: "some-snap", Revision: snap.R(2)}
	snapstate.Set(st, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
		Current:  sideInfo.Revision,
	})

	defer snapshotstate.MockBackendEstimateSnapshotSize(func(*snap.Info, []string, *dirs.SnapDirOptions) (uint64, error) {
		return 123, nil
	})()

	sz, err := snapshotstate.EstimateSnapshotSize(st, "some-snap", nil)
	c.Assert(err, check.IsNil)
	c.Check(sz, check.Equals, uint64(123))
}

func (snapshotSuite) TestEstimateSnapshotSizeWithConfig(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	sideInfo := &snap.SideInfo{RealName: "some-snap", Revision: snap.R(2)}
	snapstate.Set(st, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
		Current:  sideInfo.Revision,
	})

	defer snapshotstate.MockBackendEstimateSnapshotSize(func(*snap.Info, []string, *dirs.SnapDirOptions) (uint64, error) {
		return 100, nil
	})()

	defer snapshotstate.MockConfigGetSnapConfig(func(_ *state.State, snapname string) (*json.RawMessage, error) {
		c.Check(snapname, check.Equals, "some-snap")
		buf := json.RawMessage(`{"hello": "there"}`)
		return &buf, nil
	})()

	sz, err := snapshotstate.EstimateSnapshotSize(st, "some-snap", nil)
	c.Assert(err, check.IsNil)
	// size is 100 + 18
	c.Check(sz, check.Equals, uint64(118))
}

func (snapshotSuite) TestEstimateSnapshotSizeError(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	sideInfo := &snap.SideInfo{RealName: "some-snap", Revision: snap.R(2)}
	snapstate.Set(st, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
		Current:  sideInfo.Revision,
	})

	defer snapshotstate.MockBackendEstimateSnapshotSize(func(*snap.Info, []string, *dirs.SnapDirOptions) (uint64, error) {
		return 0, fmt.Errorf("an error")
	})()

	_, err := snapshotstate.EstimateSnapshotSize(st, "some-snap", nil)
	c.Assert(err, check.ErrorMatches, `an error`)
}

func (snapshotSuite) TestEstimateSnapshotSizeWithUsers(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	sideInfo := &snap.SideInfo{RealName: "some-snap", Revision: snap.R(2)}
	snapstate.Set(st, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
		Current:  sideInfo.Revision,
	})

	var gotUsers []string
	defer snapshotstate.MockBackendEstimateSnapshotSize(func(info *snap.Info, users []string, opts *dirs.SnapDirOptions) (uint64, error) {
		gotUsers = users
		return 0, nil
	})()

	_, err := snapshotstate.EstimateSnapshotSize(st, "some-snap", []string{"user1", "user2"})
	c.Assert(err, check.IsNil)
	c.Check(gotUsers, check.DeepEquals, []string{"user1", "user2"})
}

func (snapshotSuite) TestExportSnapshotConflictsWithForget(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("forget-snapshot-change", "...")
	tsk := st.NewTask("forget-snapshot", "...")
	tsk.SetStatus(state.DoingStatus)
	tsk.Set("snapshot-setup", map[string]int{"set-id": 42})
	chg.AddTask(tsk)

	_, err := snapshotstate.Export(context.TODO(), st, 42)
	c.Assert(err, check.NotNil)
	c.Assert(err.Error(), check.Equals, `cannot operate on snapshot set #42 while change "1" is in progress`)
}

func (snapshotSuite) TestImportSnapshotDuplicatedNoConflict(c *check.C) {
	buf := &bytes.Buffer{}
	var importCalls int
	restore := snapshotstate.MockBackendImport(func(ctx context.Context, id uint64, r io.Reader, flags *backend.ImportFlags) ([]string, error) {
		importCalls++
		c.Check(id, check.Equals, uint64(1))
		return nil, backend.DuplicatedSnapshotImportError{SetID: 42, SnapNames: []string{"foo-snap"}}
	})
	defer restore()

	st := state.New(nil)
	setID, snaps, err := snapshotstate.Import(context.TODO(), st, buf)
	c.Check(importCalls, check.Equals, 1)
	c.Assert(err, check.IsNil)
	c.Check(setID, check.Equals, uint64(42))
	c.Check(snaps, check.DeepEquals, []string{"foo-snap"})
}

func (snapshotSuite) TestImportSnapshotConflictsWithForget(c *check.C) {
	buf := &bytes.Buffer{}
	var importCalls int
	restore := snapshotstate.MockBackendImport(func(ctx context.Context, id uint64, r io.Reader, flags *backend.ImportFlags) ([]string, error) {
		importCalls++
		switch importCalls {
		case 1:
			c.Assert(flags, check.IsNil)
		case 2:
			c.Assert(flags, check.NotNil)
			c.Assert(flags.NoDuplicatedImportCheck, check.Equals, true)
			return []string{"foo"}, nil
		default:
			c.Fatal("unexpected number call to Import")
		}
		// DuplicatedSnapshotImportError is the only case where we can encounter
		// conflict on import (trying to reuse existing snapshot).
		return nil, backend.DuplicatedSnapshotImportError{SetID: 42, SnapNames: []string{"not-relevant-because-of-retry"}}
	})
	defer restore()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	// conflicting change
	chg := st.NewChange("forget-snapshot-change", "...")
	tsk := st.NewTask("forget-snapshot", "...")
	tsk.SetStatus(state.DoingStatus)
	tsk.Set("snapshot-setup", map[string]int{"set-id": 42})
	chg.AddTask(tsk)

	st.Unlock()
	setID, snaps, err := snapshotstate.Import(context.TODO(), st, buf)
	st.Lock()
	c.Check(importCalls, check.Equals, 2)
	c.Assert(err, check.IsNil)
	c.Check(setID, check.Equals, uint64(1))
	c.Check(snaps, check.DeepEquals, []string{"foo"})
}

func (snapshotSuite) TestExportSnapshotSetsOpInProgress(c *check.C) {
	restore := snapshotstate.MockBackendNewSnapshotExport(func(ctx context.Context, setID uint64) (se *backend.SnapshotExport, err error) {
		return nil, nil
	})
	defer restore()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, err := snapshotstate.Export(context.TODO(), st, 42)
	c.Assert(err, check.IsNil)

	ops := st.Cached("snapshot-ops")
	c.Assert(ops, check.DeepEquals, map[uint64]string{
		uint64(42): "export-snapshot",
	})
}

func (snapshotSuite) TestSetSnapshotOpInProgress(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	c.Assert(snapshotstate.UnsetSnapshotOpInProgress(st, 9999), check.Equals, "")

	snapshotstate.SetSnapshotOpInProgress(st, 1, "foo-op")
	snapshotstate.SetSnapshotOpInProgress(st, 2, "bar-op")

	val := st.Cached("snapshot-ops")
	c.Check(val, check.DeepEquals, map[uint64]string{
		uint64(1): "foo-op",
		uint64(2): "bar-op",
	})

	c.Check(snapshotstate.UnsetSnapshotOpInProgress(st, 1), check.Equals, "foo-op")

	val = st.Cached("snapshot-ops")
	c.Check(val, check.DeepEquals, map[uint64]string{
		uint64(2): "bar-op",
	})

	c.Check(snapshotstate.UnsetSnapshotOpInProgress(st, 2), check.Equals, "bar-op")

	val = st.Cached("snapshot-ops")
	c.Check(val, check.HasLen, 0)
}
