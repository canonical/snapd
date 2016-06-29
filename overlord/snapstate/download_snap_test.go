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

package snapstate_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type downloadSnapSuite struct {
	state     *state.State
	snapmgr   *snapstate.SnapManager
	fakeStore *fakeStore

	fakeBackend *fakeSnappyBackend

	reset func()
}

var _ = Suite(&downloadSnapSuite{})

func (s *downloadSnapSuite) SetUpTest(c *C) {
	s.fakeBackend = &fakeSnappyBackend{}
	s.state = state.New(nil)
	s.state.Lock()
	defer s.state.Unlock()

	s.fakeStore = &fakeStore{
		fakeBackend: s.fakeBackend,
	}
	snapstate.ReplaceStore(s.state, s.fakeStore)

	var err error
	s.snapmgr, err = snapstate.Manager(s.state)
	c.Assert(err, IsNil)
	s.snapmgr.AddForeignTaskHandlers(s.fakeBackend)

	snapstate.SetSnapManagerBackend(s.snapmgr, s.fakeBackend)

	s.reset = snapstate.MockReadInfo(s.fakeBackend.ReadInfo)
}

func (s *downloadSnapSuite) TearDownTest(c *C) {
	s.reset()
}

func (s *downloadSnapSuite) TestDoPrepareSnapCompatbility(c *C) {
	s.state.Lock()
	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		Name:    "foo",
		Channel: "some-channel",
		// explicitly set to "nil", this ensures the compatibility
		// code path in the task is hit and the store is queried
		// in the task (instead of using the new
		// SnapSetup.{SideInfo,DownloadInfo} that gets set in
		// snapstate.{Install,Update} directely.
		SideInfo:     nil,
		DownloadInfo: nil,
	})
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()

	s.snapmgr.Ensure()
	s.snapmgr.Wait()

	// the compat code called the store "Snap" endpoint
	c.Assert(s.fakeBackend.ops, DeepEquals, []fakeOp{
		{
			op:    "storesvc-snap",
			name:  "foo",
			revno: snap.R(11),
		},
		{
			op:   "storesvc-download",
			name: "foo",
		},
	})

	s.state.Lock()
	defer s.state.Unlock()
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Candidate, DeepEquals, &snap.SideInfo{
		OfficialName: "foo",
		SnapID:       "snapIDsnapidsnapidsnapidsnapidsn",
		Revision:     snap.R(11),
		Channel:      "some-channel",
	})
	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (s *downloadSnapSuite) TestDoPrepareSnapNormal(c *C) {
	s.state.Lock()

	si := &snap.SideInfo{
		OfficialName: "my-side-info",
		SnapID:       "mySnapID",
		Revision:     snap.R(11),
		Channel:      "my-channel",
	}

	// download, ensure the store does not query
	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		Name:     "foo",
		Channel:  "some-channel",
		SideInfo: si,
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "http://some-url.com/snap",
		},
	})
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()

	s.snapmgr.Ensure()
	s.snapmgr.Wait()

	// only the download endpoint of the store was hit
	c.Assert(s.fakeBackend.ops, DeepEquals, []fakeOp{
		{
			op:   "storesvc-download",
			name: "foo",
		},
	})

	s.state.Lock()
	defer s.state.Unlock()
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)
	// candidate comes from your SnapSetup.Candidate
	c.Check(snapst.Candidate, DeepEquals, si)
	c.Check(t.Status(), Equals, state.DoneStatus)
}
