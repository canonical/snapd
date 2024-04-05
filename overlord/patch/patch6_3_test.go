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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/patch"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type patch63Suite struct{}

var _ = Suite(&patch63Suite{})

var statePatch6_3JSON = []byte(`
{
	"data": {
		"patch-level": 6,
		"snaps": {
                  "local-install": {
			"type": "app",
			"sequence": [
			  {
				"name": "local-install",
                                "snap-id": "",
				"revision": "x1"
			  },
			  {
				"name": "local-install",
				"snap-id": "",
				"revision": "x2"
			  }
			],
			"active": true,
			"current": "x2"
                  },
		  "prefix-postfix-slashes": {
			"type": "app",
			"sequence": [
			  {
				"name": "prefix-postfix-slashes",
				"snap-id": "Hswp9oOzj3b4mw8gcC00XtxWnKH9QiCQ",
				"revision": "30",
                                "channel": "edge",
                                "title": "some-title"
			  },
			  {
				"name": "prefix-postfix-slashes",
				"snap-id": "Hswp9oOzj3b4mw8gcC00XtxWnKH9QiCQ",
				"revision": "32",
                                "channel": "edge",
                                "title": "some-title"
			  }
			],
			"active": true,
			"current": "32",
			"channel": "//edge//",
                        "user-id": 1
		  },
		  "one-prefix-slash": {
			"type": "app",
			"sequence": [
			  {
				"name": "white",
				"snap-id": "one-prefix-slash-id",
				"revision": "2"
			  }
			],
			"active": true,
			"current": "2",
			"channel": "/stable"
		  },
		  "track-with-risk": {
			"type": "app",
			"sequence": [
			  {
				"name": "track-with-risk",
				"snap-id": "track-with-snapid-id",
				"revision": "3"
			  }
			],
			"active": true,
			"current": "3",
			"channel": "latest/stable"
		  },
		  "track-with-risk-branch": {
			"type": "app",
			"sequence": [
			  {
				"name": "red",
				"snap-id": "track-with-risk-branch-snapid-id",
				"revision": "3"
			  }
			],
			"active": true,
			"current": "3",
			"channel": "1.0/stable/branch"
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
                               "task-ids": ["1", "8"]
                       },
                       "9": {
                               "id": "9",
                               "kind": "install",
                               "summary": "...",
                               "status": 4,
                               "clean": true,
                               "task-ids": ["10"]
                       },
                       "99": {
                               "id": "99",
                               "kind": "install",
                               "summary": "...",
                               "status": 0,
                               "clean": true,
                               "task-ids": ["99"]
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
                               "channel": "/stable",
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
                                 "name": "other-snap",
                                 "snap-id": "other-snap-id",
                                 "revision": "1",
                                 "channel": "stable",
                                 "title": "other-snap"
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
                       "kind": "link-snap",
                       "summary": "",
                       "status": 2,
                       "data": {
                         "old-candidate-index": -1,
                         "old-channel": "/18/edge//",
                         "snap-setup-task": "1"
                      },
                      "change": "6"
                 },
                 "10": {
                       "id": "10",
                       "kind": "other",
                       "summary": "",
                       "status": 4,
                       "data": {
                         "old-channel": "/edge",
                         "snap-setup": {
                               "channel": "/stable",
                               "type": "app",
                                "snap-path": "/path",
                                "side-info": {
                                 "name": "some-snap",
                                 "snap-id": "some-snap-id",
                                 "revision": "1",
                                 "channel": "stable",
                                 "title": "snapd"
                               }
                          }
                       },
                       "change": "9"
                 },
                 "99": {
                       "id": "99",
                       "kind": "prepare-snap",
                       "summary": "",
                       "status": 0,
                       "data": {
                         "snap-setup": {
                               "type": "app",
                               "snap-path": "/path",
                               "side-info": {
                                 "name": "local-snap",
                                 "snap-id": "",
                                 "revision": "unset"
                               }
                          }
                       },
                       "change": "99"
                 }
        }
}`)

func (s *patch63Suite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	snap.MockSanitizePlugsSlots(func(*snap.Info) {})
}

func (s *patch63Suite) TestPatch63(c *C) {
	restore1 := patch.MockLevel(6, 3)
	defer restore1()

	r := bytes.NewReader(statePatch6_3JSON)
	st, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	c.Assert(patch.Apply(st), IsNil)
	st.Lock()
	defer st.Unlock()

	all, err := snapstate.All(st)
	c.Assert(err, IsNil)
	// our mocks are ok
	c.Check(all, HasLen, 5)
	c.Check(all["prefix-postfix-slashes"], NotNil)
	// our patch changed this
	c.Check(all["prefix-postfix-slashes"].TrackingChannel, Equals, "latest/edge")
	// none of the other information has changed
	c.Check(all["prefix-postfix-slashes"], DeepEquals, &snapstate.SnapState{
		SnapType: "app",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{
				RealName:    "prefix-postfix-slashes",
				SnapID:      "Hswp9oOzj3b4mw8gcC00XtxWnKH9QiCQ",
				Revision:    snap.R(30),
				Channel:     "edge",
				EditedTitle: "some-title",
			}, {
				RealName:    "prefix-postfix-slashes",
				SnapID:      "Hswp9oOzj3b4mw8gcC00XtxWnKH9QiCQ",
				Revision:    snap.R(32),
				Channel:     "edge",
				EditedTitle: "some-title",
			},
		}),
		Active:          true,
		Current:         snap.R(32),
		TrackingChannel: "latest/edge",
		UserID:          1,
	})
	// another transition
	c.Check(all["one-prefix-slash"].TrackingChannel, Equals, "latest/stable")
	// full
	c.Check(all["track-with-risk"].TrackingChannel, Equals, "latest/stable")
	// unchanged
	c.Check(all["track-with-risk-branch"].TrackingChannel, Equals, "1.0/stable/branch")
	// also unchanged
	c.Check(all["local-install"].TrackingChannel, Equals, "")

	// check tasks
	task := st.Task("1")
	c.Assert(task, NotNil)
	// this was converted
	var snapsup snapstate.SnapSetup
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "latest/stable")

	// validity check that old stuff is untouched
	c.Check(snapsup.Flags.IsAutoRefresh, Equals, true)
	c.Assert(snapsup.Media, HasLen, 5)
	c.Check(snapsup.Media[0].URL, Equals, "a")
	c.Assert(snapsup.DownloadInfo, NotNil)
	c.Check(snapsup.DownloadInfo.DownloadURL, Equals, "foo")
	c.Check(snapsup.DownloadInfo.Deltas, HasLen, 1)

	// old-channel data got updated
	task = st.Task("8")
	c.Assert(task, NotNil)
	var oldCh string
	c.Assert(task.Get("old-channel", &oldCh), IsNil)
	c.Check(oldCh, Equals, "18/edge")

	// task 10 not updated because the change is ready
	task = st.Task("10")
	c.Assert(task, NotNil)
	c.Assert(task.Get("snap-setup", &snapsup), IsNil)
	c.Check(snapsup.Channel, Equals, "/stable")
	err = task.Get("old-channel", &oldCh)
	c.Assert(err, IsNil)
	c.Check(oldCh, Equals, "/edge")
}
