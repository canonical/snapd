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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/patch"
	"github.com/snapcore/snapd/overlord/state"
)

type patch6Suite struct{}

var _ = Suite(&patch6Suite{})

var statePatch5JSON = []byte(`
{
	"last-task-id": 999,
	"last-change-id": 99,

	"data": {
		"patch-level": 5,
		"snaps": {
			"a": {
				"sequence": [{"name": "", "revision": "2"}],
                                "flags": 1,
				"current": "2"},
			"b": {
				"sequence": [{"name": "b", "revision": "2"}],
                                "flags": 2,
				"current": "2"},
			"c": {
				"sequence": [{"name": "c", "revision": "2"}],
                                "flags": 4,
				"current": "2"}
		}
	},
	"changes": {
		"1": {
			"id": "1",
			"kind": "install-snap",
			"summary": "install a snap",
			"status": 0,
			"data": {"snap-names": ["a"]},
			"task-ids": ["11","12"]
		},
		"2": {
			"id": "2",
			"kind": "install-snap",
			"summary": "install b snap",
			"status": 0,
			"data": {"snap-names": ["b"]},
			"task-ids": ["11","12"]
		},
		"3": {
			"id": "3",
			"kind": "revert-snap",
			"summary": "revert c snap",
			"status": 0,
			"data": {"snap-names": ["c"]},
			"task-ids": ["21","22"]
		}
	},
	"tasks": {
                "11": {
                        "id": "11",
                        "change": "1",
                        "kind": "download-snap",
                        "summary": "Download snap a from channel edge",
                        "status": 4,
                        "data": {"snap-setup": {
                                "channel": "edge",
                                "flags": 1
                        }},
                        "halt-tasks": ["12"]
                },
                "12": {"id": "12", "change": "1", "kind": "some-other-task"},
                "21": {
                        "id": "21",
                        "change": "2",
                        "kind": "download-snap",
                        "summary": "Download snap b from channel beta",
                        "status": 4,
                        "data": {"snap-setup": {
                                "channel": "beta",
                                "flags": 2
                        }},
                        "halt-tasks": ["22"]
                },
                "22": {"id": "22", "change": "2", "kind": "some-other-task"},
                "31": {
                        "id": "31",
                        "change": "3",
                        "kind": "prepare-snap",
                        "summary": "Prepare snap c",
                        "status": 4,
                        "data": {"snap-setup": {
                                "channel": "stable",
                                "flags": 1073741828
                        }},
                        "halt-tasks": ["32"]
                },
                "32": {"id": "32", "change": "3", "kind": "some-other-task"}
	}
}
`)

func (s *patch6Suite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(dirs.SnapStateFile, statePatch5JSON, 0644)
	c.Assert(err, IsNil)
}

func (s *patch6Suite) TestPatch6(c *C) {
	restorer := patch.MockLevel(6, 1)
	defer restorer()

	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	defer r.Close()
	st, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	func() {
		st.Lock()
		defer st.Unlock()

		stateMap, err := patch.Patch4StateMap(st)
		c.Assert(err, IsNil)
		c.Check(int(stateMap["a"].Flags), Equals, 1)
		c.Check(int(stateMap["b"].Flags), Equals, 2)
		c.Check(int(stateMap["c"].Flags), Equals, 4)
	}()

	c.Assert(patch.Apply(st), IsNil)

	st.Lock()
	defer st.Unlock()

	stateMap, err := patch.Patch6StateMap(st)
	c.Assert(err, IsNil)

	c.Check(stateMap["a"].DevMode, Equals, true)
	c.Check(stateMap["a"].TryMode, Equals, false)
	c.Check(stateMap["a"].JailMode, Equals, false)

	c.Check(stateMap["b"].DevMode, Equals, false)
	c.Check(stateMap["b"].TryMode, Equals, true)
	c.Check(stateMap["b"].JailMode, Equals, false)

	c.Check(stateMap["c"].DevMode, Equals, false)
	c.Check(stateMap["c"].TryMode, Equals, false)
	c.Check(stateMap["c"].JailMode, Equals, true)

	for _, task := range st.Tasks() {
		snapsup, err := patch.Patch6SnapSetup(task)
		if err == state.ErrNoState {
			continue
		}
		c.Assert(err, IsNil)

		var snaps []string
		c.Assert(task.Change().Get("snap-names", &snaps), IsNil)
		c.Assert(snaps, HasLen, 1)

		switch snaps[0] {
		case "a":
			c.Check(snapsup.DevMode, Equals, true, Commentf("a"))
			c.Check(snapsup.TryMode, Equals, false, Commentf("a"))
			c.Check(snapsup.JailMode, Equals, false, Commentf("a"))
			c.Check(snapsup.Revert, Equals, false, Commentf("a"))
		case "b":
			c.Check(snapsup.DevMode, Equals, false, Commentf("b"))
			c.Check(snapsup.TryMode, Equals, true, Commentf("b"))
			c.Check(snapsup.JailMode, Equals, false, Commentf("b"))
			c.Check(snapsup.Revert, Equals, false, Commentf("b"))
		case "c":
			c.Check(snapsup.DevMode, Equals, false, Commentf("c"))
			c.Check(snapsup.TryMode, Equals, false, Commentf("c"))
			c.Check(snapsup.JailMode, Equals, true, Commentf("c"))
			c.Check(snapsup.Revert, Equals, true, Commentf("c"))
		}
	}
}
