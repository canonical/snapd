// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

type patch62Suite struct{}

var _ = Suite(&patch62Suite{})

var statePatch6_2JSON = []byte(`
{
	"data": {
		"patch-level": 6,
		"snaps": {
		  "snapd": {
			"type": "app",
			"sequence": [
			  {
				"name": "snapd",
				"snap-id": "snapd-snap-id",
				"revision": "2"
			  }
			],
			"active": true,
			"current": "2",
			"channel": "stable"
		  },
		  "other": {
			"type": "app",
			"sequence": [
			  {
				"name": "snapd",
				"snap-id": "foo",
				"revision": "2"
			  }
			],
			"active": true,
			"current": "2",
			"channel": "stable"
		  }
		}
	  },
	  "changes": {
			"6": {
				"id": "6",
				"kind": "auto-refresh",
				"summary": "...",
				"status": 0,
				"clean": true,
				"data": {
				"api-data": {
					"snap-names": ["snapd"]
				},
				"snap-names": ["snapd"]
				},
				"task-ids": ["1"]
			}
	  },
	  "tasks": {
		"1": {
			"id": "1",
			"kind": "download-snap",
			"summary": "...",
			"status": 2,
			"clean": true,
			"progress": {
			  "label": "...",
			  "done": 1,
			  "total": 2
			},
			"data": {
			  "snap-setup": {
				"channel": "stable",
				"type": "app",
				"is-auto-refresh": true,
				"snap-path": "/path",
				"download-info": {
				  "download-url": "foo",
				  "size": 1234,
				  "sha3-384": "1",
				  "deltas": [
					{
					  "from-revision": 10934,
					  "to-revision": 10972,
					  "format": "xdelta3",
					  "download-url": "foo",
					  "size": 16431136,
					  "sha3-384": "1"
					}
				  ]
				},
				"side-info": {
				  "name": "snapd",
				  "snap-id": "snapd-snap-id",
				  "revision": "1",
				  "channel": "stable",
				  "contact": "foo",
				  "title": "snapd",
				  "summary": "...",
				  "description": "..."
				},
				"media": [
				  {
					"type": "icon",
					"url": "a"
				  },
				  {
					"type": "screenshot",
					"url": "2"
				  },
				  {
					"type": "screenshot",
					"url": "3"
				  },
				  {
					"type": "screenshot",
					"url": "4"
				  },
				  {
					"type": "video",
					"url": "5"
				  }
				]
			  }
			},
			"wait-tasks": ["3"],
			"halt-tasks": ["4"],
			"change": "6"
		  }
		}
	  }
	}
}`)

func (s *patch62Suite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(dirs.SnapStateFile, statePatch6_2JSON, 0644)
	c.Assert(err, IsNil)

	snap.MockSanitizePlugsSlots(func(*snap.Info) {})
}

func (s *patch62Suite) TestPatch62(c *C) {
	restore1 := patch.MockLevel(6, 2)
	defer restore1()

	restore2 := snap.MockSnapdSnapID("snapd-snap-id")
	defer restore2()

	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	defer r.Close()
	st, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	c.Assert(patch.Apply(st), IsNil)
	st.Lock()
	defer st.Unlock()

	// our mocks are correct
	c.Assert(st.Changes(), HasLen, 1)
	c.Assert(st.Tasks(), HasLen, 1)

	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(st, "snapd", &snapst), IsNil)
	c.Check(snapst.SnapType, Equals, "snapd")

	// sanity check - "other" is untouched
	c.Assert(snapstate.Get(st, "other", &snapst), IsNil)
	c.Check(snapst.SnapType, Equals, "app")

	// check tasks
	task := st.Task("1")
	c.Assert(task, NotNil)

	var snapsup snapstate.SnapSetup
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Type, Equals, snap.TypeSnapd)

	// sanity check
	c.Check(snapsup.Flags.IsAutoRefresh, Equals, true)
	c.Assert(snapsup.Media, HasLen, 5)
	c.Check(snapsup.Media[0].URL, Equals, "a")
	c.Assert(snapsup.DownloadInfo, NotNil)
	c.Check(snapsup.DownloadInfo.DownloadURL, Equals, "foo")
}
