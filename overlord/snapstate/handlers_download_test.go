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
		state:       s.state,
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

func (s *downloadSnapSuite) TestDoDownloadSnapCompatbility(c *C) {
	s.state.Lock()
	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
		},
		Channel: "some-channel",
		// explicitly set to "nil", this ensures the compatibility
		// code path in the task is hit and the store is queried
		// in the task (instead of using the new
		// SnapSetup.{SideInfo,DownloadInfo} that gets set in
		// snapstate.{Install,Update} directely.
		DownloadInfo: nil,
	})
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()

	s.snapmgr.Ensure()
	s.snapmgr.Wait()

	// the compat code called the store "Snap" endpoint
	c.Assert(s.fakeBackend.ops, DeepEquals, fakeOps{
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

	var snapsup snapstate.SnapSetup
	t.Get("snap-setup", &snapsup)
	c.Check(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "foo",
		SnapID:   "snapIDsnapidsnapidsnapidsnapidsn",
		Revision: snap.R(11),
		Channel:  "some-channel",
	})
	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (s *downloadSnapSuite) TestDoDownloadSnapNormal(c *C) {
	s.state.Lock()

	si := &snap.SideInfo{
		RealName: "foo",
		SnapID:   "mySnapID",
		Revision: snap.R(11),
		Channel:  "my-channel",
	}

	// download, ensure the store does not query
	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
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
	c.Assert(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:   "storesvc-download",
			name: "foo",
		},
	})

	s.state.Lock()
	defer s.state.Unlock()

	var snapsup snapstate.SnapSetup
	t.Get("snap-setup", &snapsup)
	c.Check(snapsup.SideInfo, DeepEquals, si)
	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (s *downloadSnapSuite) TestDoUndoDownloadSnap(c *C) {
	s.state.Lock()
	si := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(33),
	}
	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "http://something.com/snap",
		},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.snapmgr.Ensure()
		s.snapmgr.Wait()
	}

	s.state.Lock()
	defer s.state.Unlock()

	// task was undone
	c.Check(t.Status(), Equals, state.UndoneStatus)

	// and nothing is in the state for "foo"
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, Equals, state.ErrNoState)

}
