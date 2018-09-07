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
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/context"
	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/snapshotstate"
	"github.com/snapcore/snapd/overlord/snapshotstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
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

func (snapshotSuite) createConflictingChange(c *check.C) (st *state.State, restore func()) {
	shotfile, err := os.Create(filepath.Join(c.MkDir(), "foo.zip"))
	c.Assert(err, check.IsNil)
	shotfile.Close()

	o := overlord.Mock()
	st = o.State()

	fakeIter := func(_ context.Context, f func(*backend.Reader) error) error {
		c.Assert(f(&backend.Reader{
			Snapshot: client.Snapshot{SetID: 42, Snap: "foo"},
			File:     shotfile,
		}), check.IsNil)

		return nil
	}
	restoreIter := snapshotstate.MockBackendIter(fakeIter)
	st.Lock()
	defer func() {
		if c.Failed() {
			// something went wrong
			st.Unlock()
		}
	}()

	stmgr, err := snapstate.Manager(st, o.TaskRunner())
	c.Assert(err, check.IsNil)
	o.AddManager(stmgr)
	shmgr := snapshotstate.Manager(st, o.TaskRunner())
	o.AddManager(shmgr)

	snapstate.Set(st, "foo", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	chg := st.NewChange("rm foo", "...")
	rmTasks, err := snapstate.Remove(st, "foo", snap.R(0))
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

	_, _, _, err := snapshotstate.Save(st, []string{"foo"}, nil)
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
	st.Lock()
	defer st.Unlock()

	stmgr, err := snapstate.Manager(st, o.TaskRunner())
	c.Assert(err, check.IsNil)
	o.AddManager(stmgr)
	shmgr := snapshotstate.Manager(st, o.TaskRunner())
	o.AddManager(shmgr)

	snapstate.Set(st, "foo", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	chg := st.NewChange("snapshot-save", "...")
	_, _, saveTasks, err := snapshotstate.Save(st, nil, nil)
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
	_, _, _, err := snapshotstate.Save(st, nil, nil)
	c.Check(err, check.ErrorMatches, "bzzt")
}

func (snapshotSuite) TestSaveChecksSetIDError(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	st.Set("last-snapshot-set-id", "3/4")

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
			"a-snap": {Active: true},
			"b-snap": {},
			"c-snap": {Active: true},
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
		// snapstate.All isn't called when a snap name is passed in
		return nil, errors.New("bzzt")
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

func (snapshotSuite) TestSaveIntegration(c *check.C) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root")
	}
	o := overlord.Mock()
	st := o.State()
	st.Lock()
	defer st.Unlock()

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

	snapshots := make(map[string]*client.Snapshot, 3)
	for i, name := range []string{"one-snap", "too-snap", "tri-snap"} {
		sideInfo := &snap.SideInfo{RealName: name, Revision: snap.R(i + 1)}
		snapstate.Set(st, name, &snapstate.SnapState{
			Active:   true,
			Sequence: []*snap.SideInfo{sideInfo},
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
			Epoch:    *snap.E("0"),
		}
	}

	stmgr, err := snapstate.Manager(st, o.TaskRunner())
	c.Assert(err, check.IsNil)
	o.AddManager(stmgr)
	shmgr := snapshotstate.Manager(st, o.TaskRunner())
	o.AddManager(shmgr)
	o.AddManager(o.TaskRunner())

	setID, saved, taskset, err := snapshotstate.Save(st, nil, []string{"a-user"})
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
		c.Skip("this test cannot run as root")
	}
	o := overlord.Mock()
	st := o.State()
	st.Lock()
	defer st.Unlock()

	c.Assert(os.MkdirAll(dirs.SnapshotsDir, 0755), check.IsNil)
	// sanity check: no files in snapshot dir
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

	for i, name := range []string{"one-snap", "too-snap", "tri-snap"} {
		sideInfo := &snap.SideInfo{RealName: name, Revision: snap.R(i + 1)}
		snapstate.Set(st, name, &snapstate.SnapState{
			Active:   true,
			Sequence: []*snap.SideInfo{sideInfo},
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

	stmgr, err := snapstate.Manager(st, o.TaskRunner())
	c.Assert(err, check.IsNil)
	o.AddManager(stmgr)
	shmgr := snapshotstate.Manager(st, o.TaskRunner())
	o.AddManager(shmgr)
	o.AddManager(o.TaskRunner())

	setID, saved, taskset, err := snapshotstate.Save(st, nil, []string{"a-user"})
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
	c.Check(tasks[0].Summary(), testutil.Contains, `"one-snap"`) // sanity check: task 0 is one-snap's
	c.Check(tasks[0].Status(), check.Equals, state.UndoneStatus)

	// task 1 (for "too-snap") will have errored
	c.Check(tasks[1].Summary(), testutil.Contains, `"too-snap"`) // sanity check: task 1 is too-snap's
	c.Check(tasks[1].Status(), check.Equals, state.ErrorStatus)
	c.Check(strings.Join(tasks[1].Log(), "\n"), check.Matches, `\S+ ERROR cannot create archive: .* Permission denied .and \d+ more.`)

	// task 2 (for "tri-snap") will have errored as well, hopefully, but it's a race (see the "tar" comment above)
	c.Check(tasks[2].Summary(), testutil.Contains, `"tri-snap"`) // sanity check: task 2 is tri-snap's
	c.Check(tasks[2].Status(), check.Equals, state.ErrorStatus, check.Commentf("if this ever fails, duplicate the fake tar sleeps please"))
	// sometimes you'll get one, sometimes you'll get the other (depending on ordering of events)
	c.Check(strings.Join(tasks[2].Log(), "\n"), check.Matches, `\S+ ERROR( tar failed:)? context canceled`)

	// no zips left behind, not for errors, not for undos \o/
	out, err = exec.Command("find", dirs.SnapshotsDir, "-type", "f").CombinedOutput()
	c.Assert(err, check.IsNil)
	c.Check(string(out), check.Equals, "")
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

	fakeSnapstateAll := func(*state.State) (map[string]*snapstate.SnapState, error) {
		return map[string]*snapstate.SnapState{
			"foo": {Active: true},
		}, nil
	}

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
	st.Lock()
	defer st.Unlock()

	stmgr, err := snapstate.Manager(st, o.TaskRunner())
	c.Assert(err, check.IsNil)
	o.AddManager(stmgr)
	shmgr := snapshotstate.Manager(st, o.TaskRunner())
	o.AddManager(shmgr)

	snapstate.Set(st, "foo", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(1)},
		},
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

func (snapshotSuite) TestRestoreIntegration(c *check.C) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root")
	}
	o := overlord.Mock()
	st := o.State()
	st.Lock()
	defer st.Unlock()

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

	for i, name := range []string{"one-snap", "too-snap", "tri-snap"} {
		sideInfo := &snap.SideInfo{RealName: name, Revision: snap.R(i + 1)}
		snapstate.Set(st, name, &snapstate.SnapState{
			Active:   true,
			Sequence: []*snap.SideInfo{sideInfo},
			Current:  sideInfo.Revision,
			SnapType: "app",
		})
		snapInfo := snaptest.MockSnap(c, fmt.Sprintf("{name: %s, version: v1}", name), sideInfo)

		c.Assert(os.MkdirAll(filepath.Join(homedir, "snap", name, fmt.Sprint(i+1), "canary-"+name), 0755), check.IsNil)
		c.Assert(os.MkdirAll(filepath.Join(homedir, "snap", name, "common", "common-"+name), 0755), check.IsNil)

		_, err := backend.Save(context.TODO(), 42, snapInfo, nil, []string{"a-user"})
		c.Assert(err, check.IsNil)
	}

	// move the old away
	c.Assert(os.Rename(filepath.Join(homedir, "snap"), filepath.Join(homedir, "snap.old")), check.IsNil)

	stmgr, err := snapstate.Manager(st, o.TaskRunner())
	c.Assert(err, check.IsNil)
	o.AddManager(stmgr)
	shmgr := snapshotstate.Manager(st, o.TaskRunner())
	o.AddManager(shmgr)
	o.AddManager(o.TaskRunner())

	found, taskset, err := snapshotstate.Restore(st, 42, nil, []string{"a-user"})
	c.Assert(err, check.IsNil)
	sort.Strings(found)
	c.Check(found, check.DeepEquals, []string{"one-snap", "too-snap", "tri-snap"})

	change := st.NewChange("restore-snapshot", "...")
	change.AddAll(taskset)

	st.Unlock()
	c.Assert(o.Settle(5*time.Second), check.IsNil)
	st.Lock()
	c.Check(change.Err(), check.IsNil)

	// check it was all brought back \o/
	out, err := exec.Command("diff", "-rN", filepath.Join(homedir, "snap"), filepath.Join("snap.old")).CombinedOutput()
	c.Assert(err, check.IsNil)
	c.Check(string(out), check.Equals, "")
}

func (snapshotSuite) TestRestoreIntegrationFails(c *check.C) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root")
	}
	o := overlord.Mock()
	st := o.State()
	st.Lock()
	defer st.Unlock()

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

	for i, name := range []string{"one-snap", "too-snap", "tri-snap"} {
		sideInfo := &snap.SideInfo{RealName: name, Revision: snap.R(i + 1)}
		snapstate.Set(st, name, &snapstate.SnapState{
			Active:   true,
			Sequence: []*snap.SideInfo{sideInfo},
			Current:  sideInfo.Revision,
			SnapType: "app",
		})
		snapInfo := snaptest.MockSnap(c, fmt.Sprintf("{name: %s, version: vv1}", name), sideInfo)

		c.Assert(os.MkdirAll(filepath.Join(homedir, "snap", name, fmt.Sprint(i+1), "canary-"+name), 0755), check.IsNil)
		c.Assert(os.MkdirAll(filepath.Join(homedir, "snap", name, "common", "common-"+name), 0755), check.IsNil)

		_, err := backend.Save(context.TODO(), 42, snapInfo, nil, []string{"a-user"})
		c.Assert(err, check.IsNil)
	}

	// move the old away
	c.Assert(os.Rename(filepath.Join(homedir, "snap"), filepath.Join(homedir, "snap.old")), check.IsNil)
	// but poison the well
	c.Assert(os.MkdirAll(filepath.Join(homedir, "snap"), 0755), check.IsNil)
	c.Assert(os.MkdirAll(filepath.Join(homedir, "snap", "too-snap"), 0), check.IsNil)

	stmgr, err := snapstate.Manager(st, o.TaskRunner())
	c.Assert(err, check.IsNil)
	o.AddManager(stmgr)
	shmgr := snapshotstate.Manager(st, o.TaskRunner())
	o.AddManager(shmgr)
	o.AddManager(o.TaskRunner())

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

	tasks := change.Tasks()
	c.Check(tasks, check.HasLen, 3)
	for _, task := range tasks {
		if strings.Contains(task.Summary(), `"too-snap"`) {
			c.Check(task.Status(), check.Equals, state.ErrorStatus)
			c.Check(strings.Join(task.Log(), "\n"), check.Matches, `\S+ ERROR mkdir \S+: permission denied`)
		} else {
			if task.Status() == state.ErrorStatus {
				c.Check(strings.Join(task.Log(), "\n"), check.Matches, `\S+ ERROR context canceled`)
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
