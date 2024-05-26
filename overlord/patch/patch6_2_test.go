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
	"bytes"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/patch"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type patch62Suite struct{}

var _ = Suite(&patch62Suite{})

// State with snapd snap marked as 'app' (to be converted to 'snapd'
// type) and a regular 'other' snap, plus three tasks - two of them
// need to have their SnapSetup migrated to 'snapd' type.
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
				"snap-id": "PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4",
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
				"task-ids": ["1", "8"]
			},
			"9": {
				"id": "9",
				"kind": "auto-refresh",
				"summary": "...",
				"status": 4,
				"clean": true,
				"data": {
				"api-data": {
					"snap-names": ["snapd"]
				},
				"snap-names": ["snapd"]
				},
				"task-ids": ["10"]
			}
	  },
	  "tasks": {
		"1": {
			"id": "1",
			"kind": "download-snap",
			"summary": "...",
			"status": 2,
			"clean": true,
			"data": {
			  "snap-setup": {
				"channel": "stable",
				"type": "app",
				"is-auto-refresh": true,
				"snap-path": "/path",
				"download-info": {
				  "download-url": "foo",
				  "size": 1234,
				  "sha3-384": "123456",
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
				  "snap-id": "PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4",
				  "revision": "1",
				  "channel": "stable",
				  "title": "snapd"
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
			"change": "6"
		  },
		  "8": {
			"id": "8",
			"kind": "other",
			"summary": "",
			"status": 4,
			"data": {
			  "snap-setup": {
				"channel": "stable",
				"type": "app",
				"snap-path": "/path",
				"side-info": {
				  "name": "snapd",
				  "snap-id": "PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4",
				  "revision": "1",
				  "channel": "stable",
				  "title": "snapd"
				}
			  }
			},
			"change": "6"
		  },
		  "10": {
			"id": "10",
			"kind": "other",
			"summary": "",
			"status": 4,
			"data": {
			  "snap-setup": {
				"channel": "stable",
				"type": "app",
				"snap-path": "/path",
				"side-info": {
				  "name": "snapd",
				  "snap-id": "PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4",
				  "revision": "1",
				  "channel": "stable",
				  "title": "snapd"
				}
			  }
			},
			"change": "9"
		  }
		}
	  }
	}
}`)

// State with 'snapd' snap with proper snap type, and an extra 'other'
// snap with snapd snap-id (PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4) but
// improper 'app' type.
var statePatch6_2JSONWithSnapd = []byte(`
{
	"data": {
		"patch-level": 6,
		"snaps": {
		  "snapd": {
			"type": "snapd",
			"sequence": [
			  {
				"name": "snapd",
				"snap-id": "PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4",
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
				"name": "other",
				"snap-id": "PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4",
				"revision": "1"
			  }
			],
			"active": true,
			"current": "1",
			"channel": "stable"
		  }
		}
	  },
	  "changes": {}
	  },
	  "tasks": {}
	}
}`)

// State with two snaps with snapd snap-id
// (PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4) and improper snap types
var statePatch6_2JSONWithSnapd2 = []byte(`
{
	"data": {
		"patch-level": 6,
		"snaps": {
		  "snapd": {
			"type": "app",
			"sequence": [
			  {
				"name": "snapd",
				"snap-id": "PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4",
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
				"name": "other",
				"snap-id": "PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4",
				"revision": "1"
			  }
			],
			"active": true,
			"current": "1",
			"channel": "stable"
		  }
		}
	  },
	  "changes": {}
	  },
	  "tasks": {}
	}
}`)

func (s *patch62Suite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	snap.MockSanitizePlugsSlots(func(*snap.Info) {})
}

func (s *patch62Suite) TestPatch62(c *C) {
	restore1 := patch.MockLevel(6, 2)
	defer restore1()

	r := bytes.NewReader(statePatch6_2JSON)
	st := mylog.Check2(state.ReadState(nil, r))

	c.Assert(patch.Apply(st), IsNil)
	st.Lock()
	defer st.Unlock()

	// our mocks are correct
	c.Assert(st.Changes(), HasLen, 2)
	c.Assert(st.Tasks(), HasLen, 3)

	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(st, "snapd", &snapst), IsNil)
	c.Check(snapst.SnapType, Equals, "snapd")

	// validity check - "other" is untouched
	c.Assert(snapstate.Get(st, "other", &snapst), IsNil)
	c.Check(snapst.SnapType, Equals, "app")

	// check tasks
	task := st.Task("1")
	c.Assert(task, NotNil)

	var snapsup snapstate.SnapSetup
	mylog.Check(task.Get("snap-setup", &snapsup))

	c.Check(snapsup.Type, Equals, snap.TypeSnapd)

	// validity check, structures not defined explicitly via patch62* are preserved
	c.Check(snapsup.Flags.IsAutoRefresh, Equals, true)
	c.Assert(snapsup.Media, HasLen, 5)
	c.Check(snapsup.Media[0].URL, Equals, "a")
	c.Assert(snapsup.DownloadInfo, NotNil)
	c.Check(snapsup.DownloadInfo.DownloadURL, Equals, "foo")
	c.Check(snapsup.DownloadInfo.Deltas, HasLen, 1)

	task = st.Task("8")
	c.Assert(task, NotNil)
	c.Assert(task.Get("snap-setup", &snapsup), IsNil)
	c.Check(snapsup.Type, Equals, snap.TypeSnapd)

	// task 10 not updated because the change is ready
	task = st.Task("10")
	c.Assert(task, NotNil)
	c.Assert(task.Get("snap-setup", &snapsup), IsNil)
	c.Check(snapsup.Type, Equals, snap.TypeApp)
}

func (s *patch62Suite) TestPatch62StopsAfterFirstSnapd(c *C) {
	restore1 := patch.MockLevel(6, 2)
	defer restore1()

	for i, sj := range [][]byte{statePatch6_2JSONWithSnapd, statePatch6_2JSONWithSnapd2} {
		// validity check
		c.Assert(patch.Level, Equals, 6)
		c.Assert(patch.Sublevel, Equals, 2)

		r := bytes.NewReader(sj)
		st := mylog.Check2(state.ReadState(nil, r))

		c.Assert(patch.Apply(st), IsNil)
		st.Lock()
		defer st.Unlock()

		var snapdCount int
		for _, name := range []string{"snapd", "other"} {
			var snapst snapstate.SnapState
			c.Assert(snapstate.Get(st, name, &snapst), IsNil)
			if snapst.SnapType == "snapd" {
				snapdCount++
			}
		}
		c.Check(snapdCount, Equals, 1, Commentf("#%d", i))
	}
}
