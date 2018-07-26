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
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/net/context"
	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapshotstate"
	"github.com/snapcore/snapd/overlord/snapshotstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type snapshotSuite struct{}

var _ = check.Suite(&snapshotSuite{})

// tie gocheck into testing
func TestSnapshot(t *testing.T) { check.TestingT(t) }

func (snapshotSuite) SetUpTest(c *check.C) {
	dirs.SetRootDir(c.MkDir())
}

func (snapshotSuite) TearDownTest(c *check.C) {
	dirs.SetRootDir("/")
}

func (snapshotSuite) TestNewSnapshotSetID(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	sid, err := snapshotstate.NewSnapshotSetID(st)
	c.Assert(err, check.IsNil)
	c.Check(sid, check.Equals, uint64(1))

	sid, err = snapshotstate.NewSnapshotSetID(st)
	c.Assert(err, check.IsNil)
	c.Check(sid, check.Equals, uint64(2))
}

func (snapshotSuite) TestAllActiveSnapNames(c *check.C) {
	fakeSnapstateAll := func(*state.State) (map[string]*snapstate.SnapState, error) {
		return map[string]*snapstate.SnapState{
			"a-snap": {Current: snap.R(-1)},
			"b-snap": {},
			"c-snap": {Current: snap.R(1)},
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

func (snapshotSuite) TestSnapnamesInSnapshotSet(c *check.C) {
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
			Snapshot: client.Snapshot{SetID: setID, Snap: "a-snap"},
			File:     shotfileA,
		}), check.IsNil)
		c.Assert(f(&backend.Reader{
			// not wanted (bad set id)
			Snapshot: client.Snapshot{SetID: setID + 1, Snap: "a-snap"},
			File:     shotfileA,
		}), check.IsNil)
		c.Assert(f(&backend.Reader{
			// wanted
			Snapshot: client.Snapshot{SetID: setID, Snap: "b-snap"},
			File:     shotfileB,
		}), check.IsNil)
		return nil
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	snaps, files, err := snapshotstate.SnapNamesInSnapshotSet(setID, nil)
	c.Assert(err, check.IsNil)
	c.Check(snaps, check.DeepEquals, []string{"a-snap", "b-snap"})
	c.Check(files, check.DeepEquals, []string{shotfileA.Name(), shotfileB.Name()})
}

func (snapshotSuite) TestSnapnamesInSnapshotSetSnaps(c *check.C) {
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "foo.zip"))
	c.Assert(err, check.IsNil)
	defer shotfile.Close()
	setID := uint64(42)
	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			// wanted
			Snapshot: client.Snapshot{SetID: setID, Snap: "a-snap"},
			File:     shotfile,
		}), check.IsNil)
		c.Assert(f(&backend.Reader{
			// not wanted (bad set id)
			Snapshot: client.Snapshot{SetID: setID + 1, Snap: "a-snap"},
			File:     shotfile,
		}), check.IsNil)
		c.Assert(f(&backend.Reader{
			// not wanted (bad snap name)
			Snapshot: client.Snapshot{SetID: setID, Snap: "c-snap"},
			File:     shotfile,
		}), check.IsNil)
		return nil
	}
	defer snapshotstate.MockBackendIter(fakeIter)()

	snaps, files, err := snapshotstate.SnapNamesInSnapshotSet(setID, []string{"a-snap"})
	c.Assert(err, check.IsNil)
	c.Check(snaps, check.DeepEquals, []string{"a-snap"})
	c.Check(files, check.DeepEquals, []string{shotfile.Name()})
}

func (snapshotSuite) TestSnapnamesInSnapshotSetErrors(c *check.C) {
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

	snaps, files, err := snapshotstate.SnapNamesInSnapshotSet(setID, nil)
	c.Assert(err, check.Equals, errBad)
	c.Check(snaps, check.IsNil)
	c.Check(files, check.IsNil)
}

func (snapshotSuite) TestSnapnamesInSnapshotSetNotFound(c *check.C) {
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

	snaps, files, err := snapshotstate.SnapNamesInSnapshotSet(setID, nil)
	c.Assert(err, check.Equals, client.ErrSnapshotSetNotFound)
	c.Check(snaps, check.IsNil)
	c.Check(files, check.IsNil)
}

func (snapshotSuite) TestSnapnamesInSnapshotSetEmptyNotFound(c *check.C) {
	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error { return nil }
	defer snapshotstate.MockBackendIter(fakeIter)()

	snaps, files, err := snapshotstate.SnapNamesInSnapshotSet(42, nil)
	c.Assert(err, check.Equals, client.ErrSnapshotSetNotFound)
	c.Check(snaps, check.IsNil)
	c.Check(files, check.IsNil)
}

func (snapshotSuite) TestSnapnamesInSnapshotSetSnapNotFound(c *check.C) {
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

	snaps, files, err := snapshotstate.SnapNamesInSnapshotSet(setID, []string{"b-snap"})
	c.Assert(err, check.Equals, client.ErrSnapshotSnapsNotFound)
	c.Check(snaps, check.IsNil)
	c.Check(files, check.IsNil)
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
	err := snapshotstate.CheckSnapshotTaskConflict(st, 42, "some-task")
	c.Assert(err, check.ErrorMatches, "internal error: task 1 .some-task. is missing snapshot information")

	// wrong snapshot state
	tsk.Set("snapshot-setup", "hello")
	err = snapshotstate.CheckSnapshotTaskConflict(st, 42, "some-task")
	c.Assert(err, check.ErrorMatches, "internal error.* could not unmarshal.*")

	tsk.Set("snapshot-setup", map[string]int{"set-id": 42})

	err = snapshotstate.CheckSnapshotTaskConflict(st, 42, "some-task")
	c.Assert(err, check.ErrorMatches, "cannot operate on snapshot set #42 while change \"1\" is in progress")

	// no change with that label
	c.Assert(snapshotstate.CheckSnapshotTaskConflict(st, 42, "some-other-task"), check.IsNil)

	// no change with that snapshot id
	c.Assert(snapshotstate.CheckSnapshotTaskConflict(st, 43, "some-task"), check.IsNil)

	// no non-ready change
	tsk.SetStatus(state.DoneStatus)
	c.Assert(snapshotstate.CheckSnapshotTaskConflict(st, 42, "some-task"), check.IsNil)
}

func (snapshotSuite) TestSaveChecksSnapnamesError(c *check.C) {
	defer snapshotstate.MockSnapstateAll(func(*state.State) (map[string]*snapstate.SnapState, error) {
		return nil, errors.New("bzzt")
	})()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	_, _, _, err := snapshotstate.Save(st, nil, nil)
	c.Check(err, check.ErrorMatches, "bzzt")
}

func (snapshotSuite) TestSaveChecksSnapstateConflictError(c *check.C) {
	defer snapshotstate.MockSnapstateCheckChangeConflictMany(func(*state.State, []string, string) error {
		return errors.New("bzzt")
	})()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	_, _, _, err := snapshotstate.Save(st, nil, nil)
	c.Check(err, check.ErrorMatches, "bzzt")
}

func (snapshotSuite) TestSaveChecksSetIDError(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	st.Set("snapshots", 42)

	_, _, _, err := snapshotstate.Save(st, nil, nil)
	c.Check(err, check.ErrorMatches, ".* could not unmarshal .*")
}

func (snapshotSuite) TestSaveNoSnapsInState(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	setID, saved, taskset, err := snapshotstate.Save(st, nil, nil)
	c.Assert(err, check.IsNil)
	c.Check(setID, check.Equals, uint64(1))
	c.Check(saved, check.HasLen, 0)
	c.Check(taskset.Tasks(), check.HasLen, 0)
}

func (snapshotSuite) TestSaveSomeSnaps(c *check.C) {
	fakeSnapstateAll := func(*state.State) (map[string]*snapstate.SnapState, error) {
		return map[string]*snapstate.SnapState{
			"a-snap": {Current: snap.R(-1)},
			"b-snap": {},
			"c-snap": {Current: snap.R(1)},
		}, nil
	}

	defer snapshotstate.MockSnapstateAll(fakeSnapstateAll)()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	setID, saved, taskset, err := snapshotstate.Save(st, nil, nil)
	c.Assert(err, check.IsNil)
	c.Check(setID, check.Equals, uint64(1))
	c.Check(saved, check.DeepEquals, []string{"a-snap", "c-snap"})
	tasks := taskset.Tasks()
	c.Assert(tasks, check.HasLen, 2)
	c.Check(tasks[0].Kind(), check.Equals, "save-snapshot")
	c.Check(tasks[0].Summary(), check.Equals, `Save data of snap "a-snap" in snapshot set #1`)
	c.Check(tasks[1].Kind(), check.Equals, "save-snapshot")
	c.Check(tasks[1].Summary(), check.Equals, `Save data of snap "c-snap" in snapshot set #1`)
}

func (snapshotSuite) TestSaveOneSnap(c *check.C) {
	fakeSnapstateAll := func(*state.State) (map[string]*snapstate.SnapState, error) {
		return map[string]*snapstate.SnapState{
			"a-snap": {Current: snap.R(-1)},
			"c-snap": {Current: snap.R(1)},
		}, nil
	}

	defer snapshotstate.MockSnapstateAll(fakeSnapstateAll)()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	setID, saved, taskset, err := snapshotstate.Save(st, []string{"a-snap"}, []string{"a-user"})
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
		"set-id": 1.,
		"snap":   "a-snap",
		"users":  []interface{}{"a-user"},
	})
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

func (snapshotSuite) TestRestoreChecksSnapstateConflicts(c *check.C) {
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

	defer snapshotstate.MockSnapstateCheckChangeConflictMany(func(*state.State, []string, string) error {
		return errors.New("bzzt")
	})()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	_, _, err = snapshotstate.Restore(st, 42, []string{"a-snap"}, nil)
	c.Assert(err, check.ErrorMatches, "bzzt")
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
	c.Assert(tasks, check.HasLen, 1)
	c.Check(tasks[0].Kind(), check.Equals, "restore-snapshot")
	c.Check(tasks[0].Summary(), check.Equals, `Restore data of snap "a-snap" from snapshot set #42`)
	var snapshot map[string]interface{}
	c.Check(tasks[0].Get("snapshot-setup", &snapshot), check.IsNil)
	c.Check(snapshot, check.DeepEquals, map[string]interface{}{
		"set-id":   42.,
		"snap":     "a-snap",
		"filename": shotfile.Name(),
		"users":    []interface{}{"a-user"},
	})
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
	})
}
