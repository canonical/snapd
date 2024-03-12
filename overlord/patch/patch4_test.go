// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package patch_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/patch"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

type patch4Suite struct{}

var _ = Suite(&patch4Suite{})

var statePatch4JSON = []byte(`
{
	"last-task-id": 999,
	"last-change-id": 99,

	"data": {
		"patch-level": 3,
		"snaps": {
			"a": {
				"sequence": [
					{"name": "", "revision": "1"},
					{"name": "", "revision": "2"},
					{"name": "", "revision": "3"}],
				"current": "2"},
			"b": {
				"sequence": [
					{"name": "", "revision": "1"},
					{"name": "", "revision": "2"}],
				"current": "2"}
		}
	},
	"changes": {
		"1": {
			"id": "1",
			"kind": "revert-snap",
			"summary": "revert a snap",
			"status": 2,
			"data": {"snap-names": ["a"]},
			"task-ids": ["1","2","3","4"]
		},
		"2": {
			"id": "2",
			"kind": "refresh-snap",
			"summary": "refresh b snap",
			"status": 2,
			"data": {"snap-names": ["b"]},
			"task-ids": ["10","11","12","13","14","15","16"]
		},
		"3": {
			"id": "3",
			"kind": "install-snap",
			"summary": "install c snap",
			"status": 0,
			"data": {"snap-names": ["c"]},
			"task-ids": ["17", "18"]
		}
	},
	"tasks": {
		"1": {
			"id": "1",
			"kind": "prepare-snap",
			"summary": "",
			"status": 4,
			"data": {
			    "snap-setup": {
				"side-info": {"revision": "2", "name": "a"}
			    }
			},
			"halt-tasks": ["2"],
			"change": "1"
		},
		"2": {
			"id": "2",
			"kind": "unlink-current-snap",
			"summary": "",
			"status": 4,
			"data": {
			    "snap-setup-task": "1"
			},
			"wait-tasks": ["1"],
			"halt-tasks": ["3"],
			"change": "1"
		},
		"3": {
			"id": "3",
			"kind": "setup-profiles",
			"summary": "",
			"status": 4,
			"data": {
			    "snap-setup-task": "1"
			},
			"wait-tasks": ["2"],
			"halt-tasks": ["4"],
			"change": "1"
		},
		"4": {
			"id": "4",
			"kind": "link-snap",
			"summary": "make snap avaiblabla",
			"status": 4,
			"data": {
			    "had-candidate": true,
			    "snap-setup-task": "1"
			},
			"wait-tasks": ["3"],
			"change": "1"
		},

		"10": {
			"id": "10",
			"kind": "download-snap",
			"summary": "... download ...",
			"status": 4,
			"data": {"snap-setup": {"side-info": {"revision": "2", "name": "a"}}},
			"halt-tasks": ["11"],
			"change": "2"
		}, "11": {
			"id": "11",
			"kind": "validate-snap",
			"summary": "... check asserts...",
			"status": 4,
			"data": {"snap-setup-task": "10"},
			"wait-tasks": ["10"],
			"halt-tasks": ["12"],
			"change": "2"
		}, "12": {
			"id": "12",
			"kind": "mount-snap",
			"summary": "... mount...",
			"status": 4,
			"data": {"snap-setup-task": "10", "snap-type": "app"},
			"wait-tasks": ["11"],
			"halt-tasks": ["13"],
			"change": "2"
		}, "13": {
			"id": "13",
			"kind": "unlink-current-snap",
			"summary": "... unlink...",
			"status": 4,
			"data": {"snap-setup-task": "10"},
			"wait-tasks": ["12"],
			"halt-tasks": ["14"],
			"change": "2"
		}, "14": {
			"id": "14",
			"kind": "copy-snap-data",
			"summary": "... copy...",
			"status": 0,
			"data": {"snap-setup-task": "10"},
			"wait-tasks": ["13"],
			"halt-tasks": ["15"],
			"change": "2"
		}, "15": {
			"id": "15",
			"kind": "setup-profiles",
			"summary": "... set up profile...",
			"status": 0,
			"data": {"snap-setup-task": "10"},
			"wait-tasks": ["14"],
			"halt-tasks": ["16"],
			"change": "2"
		}, "16": {
			"id": "16",
			"kind": "link-snap",
			"summary": "... link...",
			"status": 0,
			"data": {"snap-setup-task": "10", "had-candidate": false},
			"wait-tasks": ["15"],
			"change": "2"
		},

                "17": {
			"id": "17",
			"kind": "prepare-snap",
			"summary": "",
			"status": 4,
			"data": {
			    "snap-setup": {
				"side-info": {"revision": "1", "name": "c"}
			    }
			},
			"halt-tasks": ["18"],
			"change": "1"
		}, "18": {
			"id": "18",
			"kind": "link-snap",
			"summary": "make snap avaiblabla",
			"status": 0,
			"data": {
			    "snap-setup-task": "17"
			},
			"wait-tasks": ["17"],
			"change": "3"
		}
	}
}
`)

func (s *patch4Suite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(dirs.SnapStateFile, statePatch4JSON, 0644)
	c.Assert(err, IsNil)
}

func (s *patch4Suite) TestPatch4OnReverts(c *C) {
	restorer := patch.MockLevel(4, 1)
	defer restorer()

	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	defer r.Close()
	st, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	func() {
		st.Lock()
		defer st.Unlock()

		// simulate that the task was running (but the change
		// is not fully done yet)
		task := st.Task("4")
		c.Assert(task, NotNil)
		task.SetStatus(state.DoneStatus)

		snapsup, err := patch.Patch4TaskSnapSetup(task)
		c.Assert(err, IsNil)
		c.Check(snapsup.Flags.Revert(), Equals, false)

		var had bool
		var idx int
		c.Check(task.Get("had-candidate", &had), IsNil)
		c.Check(had, Equals, true)
		c.Check(task.Get("old-candidate-index", &idx), testutil.ErrorIs, state.ErrNoState)
		c.Check(len(task.Change().Tasks()), Equals, 4)
	}()

	// go from patch level 3 -> 4
	err = patch.Apply(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	task := st.Task("4")
	c.Assert(task, NotNil)

	snapsup, err := patch.Patch4TaskSnapSetup(task)
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Revert(), Equals, true)

	var had bool
	var idx int
	c.Check(task.Get("had-candidate", &had), testutil.ErrorIs, state.ErrNoState)
	c.Check(task.Get("old-candidate-index", &idx), IsNil)
	c.Check(idx, Equals, 1)
	c.Check(len(task.Change().Tasks()), Equals, 4)
}

func (s *patch4Suite) TestPatch4OnRevertsNoCandidateYet(c *C) {
	restorer := patch.MockLevel(4, 1)
	defer restorer()

	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	defer r.Close()
	st, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	func() {
		st.Lock()
		defer st.Unlock()

		task := st.Task("4")
		c.Assert(task, NotNil)
		// its ready to run but has not run yet
		task.Clear("had-candidate")
		task.SetStatus(state.DoStatus)

		snapsup, err := patch.Patch4TaskSnapSetup(task)
		c.Assert(err, IsNil)
		c.Check(snapsup.Flags.Revert(), Equals, false)

		var had bool
		var idx int
		c.Check(task.Get("had-candidate", &had), testutil.ErrorIs, state.ErrNoState)
		c.Check(task.Get("old-candidate-index", &idx), testutil.ErrorIs, state.ErrNoState)
		c.Check(len(task.Change().Tasks()), Equals, 4)
	}()

	// go from patch level 3 -> 4
	err = patch.Apply(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	task := st.Task("4")
	c.Assert(task, NotNil)

	snapsup, err := patch.Patch4TaskSnapSetup(task)
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Revert(), Equals, true)

	var had bool
	var idx int
	c.Check(task.Get("had-candidate", &had), testutil.ErrorIs, state.ErrNoState)
	c.Check(task.Get("old-candidate-index", &idx), IsNil)
	c.Check(idx, Equals, 1)
	c.Check(len(task.Change().Tasks()), Equals, 4)
}

func (s *patch4Suite) TestPatch4OnRefreshes(c *C) {
	restorer := patch.MockLevel(4, 1)
	defer restorer()

	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	defer r.Close()
	st, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	func() {
		st.Lock()
		defer st.Unlock()

		task := st.Task("16")
		c.Assert(task, NotNil)
		// simulate that the task was running (but the change
		// is not fully done yet)
		task.SetStatus(state.DoneStatus)

		snapsup, err := patch.Patch4TaskSnapSetup(task)
		c.Assert(err, IsNil)
		c.Check(snapsup.Flags.Revert(), Equals, false)

		var had bool
		var idx int
		c.Check(task.Get("had-candidate", &had), IsNil)
		c.Check(had, Equals, false)
		c.Check(task.Get("old-candidate-index", &idx), testutil.ErrorIs, state.ErrNoState)
		c.Check(len(task.Change().Tasks()), Equals, 7)
	}()

	// go from patch level 3 -> 4
	err = patch.Apply(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	task := st.Task("16")
	c.Assert(task, NotNil)

	snapsup, err := patch.Patch4TaskSnapSetup(task)
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Revert(), Equals, false)

	var had bool
	var idx int
	c.Check(task.Get("had-candidate", &had), testutil.ErrorIs, state.ErrNoState)
	c.Check(task.Get("old-candidate-index", &idx), IsNil)
	c.Check(idx, Equals, 1)
	// we added cleanup
	c.Check(len(task.Change().Tasks()), Equals, 7+1)
}

// This test simulates a link-snap task that is scheduled but has not
// run yet. It has no "had-candidate" data set yet.
func (s *patch4Suite) TestPatch4OnRefreshesNoHadCandidateYet(c *C) {
	restorer := patch.MockLevel(4, 1)
	defer restorer()

	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	defer r.Close()
	st, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	func() {
		st.Lock()
		defer st.Unlock()

		task := st.Task("16")
		c.Assert(task, NotNil)
		// its ready to run but has not run yet
		task.Clear("had-candidate")
		task.SetStatus(state.DoStatus)

		snapsup, err := patch.Patch4TaskSnapSetup(task)
		c.Assert(err, IsNil)
		c.Check(snapsup.Flags.Revert(), Equals, false)

		var had bool
		var idx int
		c.Check(task.Get("had-candidate", &had), testutil.ErrorIs, state.ErrNoState)
		c.Check(task.Get("old-candidate-index", &idx), testutil.ErrorIs, state.ErrNoState)
		c.Check(len(task.Change().Tasks()), Equals, 7)
	}()

	// go from patch level 3 -> 4
	err = patch.Apply(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	task := st.Task("16")
	c.Assert(task, NotNil)

	snapsup, err := patch.Patch4TaskSnapSetup(task)
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Revert(), Equals, false)

	var had bool
	var idx int
	c.Check(task.Get("had-candidate", &had), testutil.ErrorIs, state.ErrNoState)
	c.Check(task.Get("old-candidate-index", &idx), IsNil)
	c.Check(idx, Equals, 1)
	// we added cleanup
	c.Check(len(task.Change().Tasks()), Equals, 7+1)
}
