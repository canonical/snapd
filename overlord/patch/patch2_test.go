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
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type patch2Suite struct{}

var _ = Suite(&patch2Suite{})

var statePatch2JSON = []byte(`
{
	"data": {
		"patch-level": 1,
		"snaps": {
			"foo": {
				"sequence": [{
					"name": "",
					"revision": "x1"
				}, {
					"name": "",
					"revision": "x2"
				}],
				"current": "x2"
			},
			"bar": {
				"candidate": {
					"name": "",
					"revision": "x1",
					"snapid": "mysnapid"
				}
			}
		}
	},
	"changes": {
		"1": {
			"id": "1",
			"kind": "some-change",
			"summary": "summary-1",
			"status": 0,
			"task-ids": ["1"]
		},
		"2": {
			"id": "2",
			"kind": "some-other-change",
			"summary": "summary-2",
			"status": 0,
			"task-ids": ["2"]
		}
	},
	"tasks": {
		"1": {
			"id": "1",
			"kind": "something",
			"summary": "meep",
			"status": 4,
			"data": {
				"snap-setup": {
					"name": "foo",
					"revision": "x3"
				}
			},
			"halt-tasks": [
				"7"
			],
			"change": "1"
		},
		"2": {
			"id": "2",
			"kind": "something-else",
			"summary": "meep",
			"status": 4,
			"data": {
				"snap-setup": {
					"name": "bar",
					"revision": "26"
				}
			},
			"halt-tasks": [
				"3"
			],
			"change": "2"
		}
	}
}
`)

func (s *patch2Suite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(dirs.SnapStateFile, statePatch2JSON, 0644)
	c.Assert(err, IsNil)
}

func (s *patch2Suite) TestPatch2(c *C) {
	restorer := patch.MockLevel(2, 1)
	defer restorer()

	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	defer r.Close()
	st, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	// go from patch level 1 -> 2
	err = patch.Apply(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	// our mocks are correct
	c.Assert(st.Changes(), HasLen, 2)
	c.Assert(st.Tasks(), HasLen, 2)

	var snapsup snapstate.SnapSetup
	// transition of:
	// - SnapSetup.{Name,Revision} -> SnapSetup.SideInfo.{RealName,Revision}
	t := st.Task("1")
	err = t.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R("x3"),
	})

	// transition of:
	// - SnapState.Sequence is backfilled with "RealName" (if missing)
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Sequence[0].RealName, Equals, "foo")
	c.Check(snapst.Sequence[1].RealName, Equals, "foo")

	// transition of:
	// - Candidate for "bar" -> tasks SnapSetup.SideInfo
	t = st.Task("2")
	err = t.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "bar",
		Revision: snap.R("x1"),
	})

	// FIXME: bar is now empty and should no longer be there?
	err = snapstate.Get(st, "bar", &snapst)
	c.Assert(err, IsNil)
}
