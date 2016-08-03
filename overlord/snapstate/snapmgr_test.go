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
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
)

func TestSnapManager(t *testing.T) { TestingT(t) }

type snapmgrTestSuite struct {
	state   *state.State
	snapmgr *snapstate.SnapManager

	fakeBackend *fakeSnappyBackend
	fakeStore   *fakeStore

	user *auth.UserState

	reset func()
}

func (s *snapmgrTestSuite) settle() {
	// FIXME: use the real settle here
	for i := 0; i < 50; i++ {
		s.snapmgr.Ensure()
		s.snapmgr.Wait()
	}
}

var _ = Suite(&snapmgrTestSuite{})

func (s *snapmgrTestSuite) SetUpTest(c *C) {
	s.fakeBackend = &fakeSnappyBackend{}
	s.state = state.New(nil)
	s.fakeStore = &fakeStore{
		fakeCurrentProgress: 75,
		fakeTotalProgress:   100,
		fakeBackend:         s.fakeBackend,
		state:               s.state,
	}

	var err error
	s.snapmgr, err = snapstate.Manager(s.state)
	c.Assert(err, IsNil)
	s.snapmgr.AddForeignTaskHandlers(s.fakeBackend)

	snapstate.SetSnapManagerBackend(s.snapmgr, s.fakeBackend)

	restore1 := snapstate.MockReadInfo(s.fakeBackend.ReadInfo)
	restore2 := snapstate.MockOpenSnapFile(s.fakeBackend.OpenSnapFile)

	s.reset = func() {
		restore2()
		restore1()
	}

	s.state.Lock()
	snapstate.ReplaceStore(s.state, s.fakeStore)
	s.user, err = auth.NewUser(s.state, "username", "macaroon", []string{"discharge"})
	c.Assert(err, IsNil)
	s.state.Unlock()
}

func (s *snapmgrTestSuite) TearDownTest(c *C) {
	s.reset()
}

func (s *snapmgrTestSuite) TestStore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, nil)
	store1 := snapstate.Store(s.state)
	c.Check(store1, FitsTypeOf, &store.Store{})

	// cached
	store2 := snapstate.Store(s.state)
	c.Check(store1, Equals, store2)
}

func verifyInstallUpdateTasks(c *C, curActive bool, ts *state.TaskSet, st *state.State) int {
	i := 0
	n := 5
	if curActive {
		n++
	}
	c.Assert(ts.Tasks()[i].Kind(), Equals, "download-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "mount-snap")
	i++
	if curActive {
		c.Assert(ts.Tasks()[i].Kind(), Equals, "unlink-current-snap")
		i++
	}
	c.Assert(ts.Tasks()[i].Kind(), Equals, "copy-snap-data")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "setup-profiles")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "link-snap")
	return n
}

func (s *snapmgrTestSuite) TestInstallTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", 0, 0)
	c.Assert(err, IsNil)

	n := verifyInstallUpdateTasks(c, false, ts, s.state)
	c.Assert(ts.Tasks(), HasLen, n)
	c.Assert(s.state.NumTask(), Equals, n)
}

func (s *snapmgrTestSuite) TestRevertTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(7)},
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
	})

	ts, err := snapstate.Revert(s.state, "some-snap")
	c.Assert(err, IsNil)

	i := 0
	c.Assert(ts.Tasks(), HasLen, 4)
	c.Assert(s.state.NumTask(), Equals, 4)
	c.Assert(ts.Tasks()[i].Kind(), Equals, "prepare-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "unlink-current-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "setup-profiles")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "link-snap")
}

func (s *snapmgrTestSuite) TestUpdateCreatesGCTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current: snap.R(4),
	})

	ts, err := snapstate.Update(s.state, "some-snap", "", 0, 0)
	c.Assert(err, IsNil)

	i := verifyInstallUpdateTasks(c, true, ts, s.state)
	c.Assert(ts.Tasks(), HasLen, i+4)
	c.Assert(s.state.NumTask(), Equals, i+4)
	c.Assert(ts.Tasks()[i].Kind(), Equals, "clear-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "discard-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "clear-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "discard-snap")
}

func (s *snapmgrTestSuite) TestUpdateCreatesDiscardAfterCurrentTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current: snap.R(1),
	})

	ts, err := snapstate.Update(s.state, "some-snap", "", 0, 0)
	c.Assert(err, IsNil)

	i := verifyInstallUpdateTasks(c, true, ts, s.state)
	c.Assert(ts.Tasks(), HasLen, i+6)
	c.Assert(s.state.NumTask(), Equals, i+6)
	c.Assert(ts.Tasks()[i].Kind(), Equals, "clear-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "discard-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "clear-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "discard-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "clear-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "discard-snap")
}

func (s *snapmgrTestSuite) TestRevertCreatesNoGCTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(1)},
			{RealName: "some-snap", Revision: snap.R(2)},
			{RealName: "some-snap", Revision: snap.R(3)},
			{RealName: "some-snap", Revision: snap.R(4)},
		},
		Current: snap.R(2),
	})

	ts, err := snapstate.RevertToRevision(s.state, "some-snap", snap.R(4))
	c.Assert(err, IsNil)

	// ensure that we do not run any form of garbage-collection
	i := 0
	c.Assert(ts.Tasks(), HasLen, 4)
	c.Assert(s.state.NumTask(), Equals, 4)
	c.Assert(ts.Tasks()[i].Kind(), Equals, "prepare-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "unlink-current-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "setup-profiles")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "link-snap")
}

func (s *snapmgrTestSuite) TestEnableTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  false,
	})

	ts, err := snapstate.Enable(s.state, "some-snap")
	c.Assert(err, IsNil)

	i := 0
	c.Assert(ts.Tasks(), HasLen, 2)
	c.Assert(s.state.NumTask(), Equals, 2)
	c.Assert(ts.Tasks()[i].Kind(), Equals, "prepare-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "link-snap")
}

func (s *snapmgrTestSuite) TestDisableTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	ts, err := snapstate.Disable(s.state, "some-snap")
	c.Assert(err, IsNil)

	i := 0
	c.Assert(ts.Tasks(), HasLen, 1)
	c.Assert(s.state.NumTask(), Equals, 1)
	c.Assert(ts.Tasks()[i].Kind(), Equals, "unlink-snap")
}

func (s *snapmgrTestSuite) TestEnableConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  false,
	})

	ts, err := snapstate.Enable(s.state, "some-snap")
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("enable", "...").AddAll(ts)

	_, err = snapstate.Enable(s.state, "some-snap")
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestDisableConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	ts, err := snapstate.Disable(s.state, "some-snap")
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("install", "...").AddAll(ts)

	_, err = snapstate.Disable(s.state, "some-snap")
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestDoInstallChannelDefault(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "", 0, 0)
	c.Assert(err, IsNil)

	var ss snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &ss)
	c.Assert(err, IsNil)

	c.Check(ss.Channel, Equals, "stable")
}

func (s *snapmgrTestSuite) TestInstallConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", 0, 0)
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("install", "...").AddAll(ts)

	_, err = snapstate.Install(s.state, "some-snap", "some-channel", 0, 0)
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestInstallPathConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", 0, 0)
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("install", "...").AddAll(ts)

	mockSnap := makeTestSnap(c, "name: some-snap\nversion: 1.0")
	_, err = snapstate.InstallPath(s.state, "some-snap", mockSnap, "", 0)
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestUpdateTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
	})

	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", s.user.ID, 0)
	c.Assert(err, IsNil)
	n := verifyInstallUpdateTasks(c, true, ts, s.state)
	c.Assert(ts.Tasks(), HasLen, n)
	c.Assert(s.state.NumTask(), Equals, n)

	var ss snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &ss)
	c.Assert(err, IsNil)

	c.Check(ss.Channel, Equals, "some-channel")
}

func (s *snapmgrTestSuite) TestUpdateChannelFallback(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
	})

	ts, err := snapstate.Update(s.state, "some-snap", "", s.user.ID, 0)
	c.Assert(err, IsNil)

	var ss snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &ss)
	c.Assert(err, IsNil)

	c.Check(ss.Channel, Equals, "edge")
}

func (s *snapmgrTestSuite) TestUpdatePassDevMode(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
	})

	_, err := snapstate.Update(s.state, "some-snap", "some-channel", s.user.ID, snapstate.DevMode)
	c.Assert(err, IsNil)

	c.Assert(s.fakeBackend.ops, HasLen, 1)
	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-list-refresh",
		cand: store.RefreshCandidate{
			SnapID:   "some-snap-id",
			Revision: snap.R(7),
			Epoch:    "",
			DevMode:  true,
			Channel:  "some-channel",
		},
		revno: snap.R(11),
	})
}

func (s *snapmgrTestSuite) TestUpdateConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
	})

	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", s.user.ID, 0)
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("refresh", "...").AddAll(ts)

	_, err = snapstate.Update(s.state, "some-snap", "some-channel", s.user.ID, 0)
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestRemoveTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(11)},
		},
		Current: snap.R(11),
	})

	ts, err := snapstate.Remove(s.state, "foo")
	c.Assert(err, IsNil)

	i := 0
	c.Assert(ts.Tasks(), HasLen, 5)
	// all tasks are accounted
	c.Assert(s.state.NumTask(), Equals, 5)
	c.Assert(ts.Tasks()[i].Kind(), Equals, "unlink-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "remove-profiles")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "clear-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "discard-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "discard-conns")
}

func (s *snapmgrTestSuite) TestRemoveConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", Revision: snap.R(11)}},
		Current:  snap.R(11),
	})

	ts, err := snapstate.Remove(s.state, "some-snap")
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("remove", "...").AddAll(ts)

	_, err = snapstate.Remove(s.state, "some-snap")
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestInstallRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", s.user.ID, 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.Macaroon,
		name:     "some-snap",
	}})
	c.Assert(s.fakeBackend.ops, DeepEquals, []fakeOp{
		{
			op:    "storesvc-snap",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:  "current",
			old: "<no-current>",
		},
		{
			op:   "open-snap-file",
			name: "downloaded-snap-path",
		},
		{
			op:    "setup-snap",
			name:  "downloaded-snap-path",
			revno: snap.R(11),
		},
		{
			op:   "copy-data",
			name: "/snap/some-snap/11",
			old:  "<no-old>",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				Channel:  "some-channel",
				SnapID:   "snapIDsnapidsnapidsnapidsnapidsn",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/11",
		},
	})

	// check progress
	ta := ts.Tasks()
	task := ta[0]
	cur, total := task.Progress()
	c.Assert(cur, Equals, s.fakeStore.fakeCurrentProgress)
	c.Assert(total, Equals, s.fakeStore.fakeTotalProgress)
	c.Check(task.Summary(), Equals, `Download snap "some-snap" (11) from channel "some-channel"`)

	// check link snap summary
	linkTask := ta[len(ta)-1]
	c.Check(linkTask.Summary(), Equals, `Make snap "some-snap" (11) available to the system`)

	// verify snap-setup in the task state
	var ss snapstate.SnapSetup
	err = task.Get("snap-setup", &ss)
	c.Assert(err, IsNil)
	c.Assert(ss, DeepEquals, snapstate.SnapSetup{
		Channel:  "some-channel",
		UserID:   s.user.ID,
		SnapPath: "downloaded-snap-path",
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
		},
		SideInfo: ss.SideInfo,
	})
	c.Assert(ss.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(11),
		Channel:  "some-channel",
		SnapID:   "snapIDsnapidsnapidsnapidsnapidsn",
	})

	// verify snaps in the system state
	var snaps map[string]*snapstate.SnapState
	err = s.state.Get("snaps", &snaps)
	c.Assert(err, IsNil)

	snapst := snaps["some-snap"]
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Channel, Equals, "some-channel")
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Channel:  "some-channel",
		SnapID:   "snapIDsnapidsnapidsnapidsnapidsn",
		Revision: snap.R(11),
	})
}

func (s *snapmgrTestSuite) TestUpdateRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
		SnapID:   "some-snap-id",
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", s.user.ID, 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := []fakeOp{
		{
			op: "storesvc-list-refresh",
			cand: store.RefreshCandidate{
				Channel:  "some-channel",
				SnapID:   "some-snap-id",
				Revision: snap.R(7),
				Epoch:    "",
				DevMode:  false,
			},
			revno: snap.R(11),
		},
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:  "current",
			old: "/snap/some-snap/7",
		},
		{
			op:   "open-snap-file",
			name: "downloaded-snap-path",
		},
		{
			op:    "setup-snap",
			name:  "downloaded-snap-path",
			revno: snap.R(11),
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		{
			op:   "copy-data",
			name: "/snap/some-snap/11",
			old:  "/snap/some-snap/7",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/11",
		},
	}

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.Macaroon,
		name:     "some-snap",
	}})
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// check progress
	task := ts.Tasks()[0]
	cur, total := task.Progress()
	c.Assert(cur, Equals, s.fakeStore.fakeCurrentProgress)
	c.Assert(total, Equals, s.fakeStore.fakeTotalProgress)

	// verify snapSetup info
	var ss snapstate.SnapSetup
	err = task.Get("snap-setup", &ss)
	c.Assert(err, IsNil)
	c.Assert(ss, DeepEquals, snapstate.SnapSetup{
		Channel: "some-channel",
		Flags:   0,
		UserID:  s.user.ID,

		SnapPath: "downloaded-snap-path",
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
		},
		SideInfo: ss.SideInfo,
	})
	c.Assert(ss.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(11),
		Channel:  "some-channel",
		SnapID:   "some-snap-id",
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "",
		Revision: snap.R(7),
	})
	c.Assert(snapst.Sequence[1], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Channel:  "some-channel",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	})
}

func (s *snapmgrTestSuite) TestUpdateUndoRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", s.user.ID, 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.linkSnapFailTrigger = "/snap/some-snap/11"

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := []fakeOp{
		{
			op: "storesvc-list-refresh",
			cand: store.RefreshCandidate{
				Channel:  "some-channel",
				SnapID:   "some-snap-id",
				Revision: snap.R(7),
				Epoch:    "",
				DevMode:  false,
			},
			revno: snap.R(11),
		},
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:  "current",
			old: "/snap/some-snap/7",
		},
		{
			op:   "open-snap-file",
			name: "downloaded-snap-path",
		},
		{
			op:    "setup-snap",
			name:  "downloaded-snap-path",
			revno: snap.R(11),
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		{
			op:   "copy-data",
			name: "/snap/some-snap/11",
			old:  "/snap/some-snap/7",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap.failed",
			name: "/snap/some-snap/11",
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/11",
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:   "undo-copy-snap-data",
			name: "/snap/some-snap/11",
			old:  "/snap/some-snap/7",
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/7",
		},
		{
			op:    "undo-setup-snap",
			name:  "/snap/some-snap/11",
			stype: "app",
		},
	}

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.Macaroon,
		name:     "some-snap",
	}})
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 1)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "",
		Revision: snap.R(7),
	})
}

func (s *snapmgrTestSuite) TestUpdateTotalUndoRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "stable",
		Current:  si.Revision,
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", s.user.ID, 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	tasks := ts.Tasks()
	last := tasks[len(tasks)-1]

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(last)
	chg.AddTask(terr)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := []fakeOp{
		{
			op: "storesvc-list-refresh",
			cand: store.RefreshCandidate{
				Channel:  "some-channel",
				SnapID:   "some-snap-id",
				Revision: snap.R(7),
				Epoch:    "",
				DevMode:  false,
			},
			revno: snap.R(11),
		},
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:  "current",
			old: "/snap/some-snap/7",
		},
		{
			op:   "open-snap-file",
			name: "downloaded-snap-path",
		},
		{
			op:    "setup-snap",
			name:  "downloaded-snap-path",
			revno: snap.R(11),
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		{
			op:   "copy-data",
			name: "/snap/some-snap/11",
			old:  "/snap/some-snap/7",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/11",
		},
		// undoing everything from here down...
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/11",
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:   "undo-copy-snap-data",
			name: "/snap/some-snap/11",
			old:  "/snap/some-snap/7",
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/7",
		},
		{
			op:    "undo-setup-snap",
			name:  "/snap/some-snap/11",
			stype: "app",
		},
	}

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.Macaroon,
		name:     "some-snap",
	}})
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Channel, Equals, "stable")
	c.Assert(snapst.Sequence, HasLen, 1)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "",
		Revision: snap.R(7),
	})
}

func (s *snapmgrTestSuite) TestUpdateSameRevision(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", "channel-for-7", s.user.ID, 0)
	c.Assert(err, ErrorMatches, `snap "some-snap" has no updates available`)
}

func (s *snapmgrTestSuite) TestUpdateBlockedRevision(c *C) {
	si7 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	si11 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si7, &si11},
		Current:  si7.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", "some-channel", s.user.ID, 0)
	c.Check(err, ErrorMatches, `snap "some-snap" has no updates available`)

	c.Assert(s.fakeBackend.ops, HasLen, 1)
	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-list-refresh",
		cand: store.RefreshCandidate{
			SnapID:   "some-snap-id",
			Revision: snap.R(7),
			Epoch:    "",
			DevMode:  false,
			Block:    []snap.Revision{snap.R(11)},
			Channel:  "some-channel",
		},
	})

}

func (s *snapmgrTestSuite) TestUpdateLocalSnapFails(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", "some-channel", s.user.ID, 0)
	c.Assert(err, ErrorMatches, `cannot refresh local snap "some-snap"`)
}

func (s *snapmgrTestSuite) TestUpdateDisabledUnsupported(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   false,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", "some-channel", s.user.ID, 0)
	c.Assert(err, ErrorMatches, `refreshing disabled snap "some-snap" not supported`)
}

func makeTestSnap(c *C, snapYamlContent string) (snapFilePath string) {
	return snaptest.MakeTestSnapWithFiles(c, snapYamlContent, nil)
}

func (s *snapmgrTestSuite) TestInstallFirstLocalRunThrough(c *C) {
	// use the real thing for this one
	snapstate.MockOpenSnapFile(backend.OpenSnapFile)

	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := makeTestSnap(c, `name: mock
version: 1.0`)
	chg := s.state.NewChange("install", "install a local snap")
	ts, err := snapstate.InstallPath(s.state, "mock", mockSnap, "", 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// ensure only local install was run, i.e. first actions are pseudo-action current
	c.Assert(s.fakeBackend.ops, HasLen, 6)
	c.Check(s.fakeBackend.ops[0].op, Equals, "current")
	c.Check(s.fakeBackend.ops[0].old, Equals, "<no-current>")
	// and setup-snap
	c.Check(s.fakeBackend.ops[1].op, Equals, "setup-snap")
	c.Check(s.fakeBackend.ops[1].name, Matches, `.*/mock_1.0_all.snap`)
	c.Check(s.fakeBackend.ops[1].revno, Equals, snap.R("x1"))

	c.Check(s.fakeBackend.ops[4].op, Equals, "candidate")
	c.Check(s.fakeBackend.ops[4].sinfo, DeepEquals, snap.SideInfo{
		RealName: "mock",
		Revision: snap.R(-1),
	})
	c.Check(s.fakeBackend.ops[5].op, Equals, "link-snap")
	c.Check(s.fakeBackend.ops[5].name, Equals, "/snap/mock/x1")

	// verify snapSetup info
	var ss snapstate.SnapSetup
	task := ts.Tasks()[0]
	err = task.Get("snap-setup", &ss)
	c.Assert(err, IsNil)
	c.Assert(ss, DeepEquals, snapstate.SnapSetup{
		SnapPath: mockSnap,
		SideInfo: ss.SideInfo,
	})
	c.Assert(ss.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "mock",
		Revision: snap.R(-1),
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "mock", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "mock",
		Channel:  "",
		Revision: snap.R(-1),
	})
	c.Assert(snapst.LocalRevision(), Equals, snap.R(-1))
}

func (s *snapmgrTestSuite) TestInstallSubsequentLocalRunThrough(c *C) {
	// use the real thing for this one
	snapstate.MockOpenSnapFile(backend.OpenSnapFile)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "mock", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "mock", Revision: snap.R(-2)},
		},
		Current: snap.R(-2),
	})

	mockSnap := makeTestSnap(c, `name: mock
version: 1.0`)
	chg := s.state.NewChange("install", "install a local snap")
	ts, err := snapstate.InstallPath(s.state, "mock", mockSnap, "", 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// ensure only local install was run, i.e. first action is pseudo-action current
	c.Assert(s.fakeBackend.ops, HasLen, 7)
	c.Check(s.fakeBackend.ops[0].op, Equals, "current")
	c.Check(s.fakeBackend.ops[0].old, Equals, "/snap/mock/x2")
	// and setup-snap
	c.Check(s.fakeBackend.ops[1].op, Equals, "setup-snap")
	c.Check(s.fakeBackend.ops[1].name, Matches, `.*/mock_1.0_all.snap`)
	c.Check(s.fakeBackend.ops[1].revno, Equals, snap.R("x3"))

	c.Check(s.fakeBackend.ops[2].op, Equals, "unlink-snap")
	c.Check(s.fakeBackend.ops[2].name, Equals, "/snap/mock/x2")

	c.Check(s.fakeBackend.ops[3].op, Equals, "copy-data")
	c.Check(s.fakeBackend.ops[3].name, Equals, "/snap/mock/x3")
	c.Check(s.fakeBackend.ops[3].old, Equals, "/snap/mock/x2")

	c.Check(s.fakeBackend.ops[4].op, Equals, "setup-profiles:Doing")
	c.Check(s.fakeBackend.ops[4].name, Equals, "mock")
	c.Check(s.fakeBackend.ops[4].revno, Equals, snap.R(-3))

	c.Check(s.fakeBackend.ops[5].op, Equals, "candidate")
	c.Check(s.fakeBackend.ops[5].sinfo, DeepEquals, snap.SideInfo{
		RealName: "mock",
		Revision: snap.R(-3),
	})
	c.Check(s.fakeBackend.ops[6].op, Equals, "link-snap")
	c.Check(s.fakeBackend.ops[6].name, Equals, "/snap/mock/x3")

	// verify snapSetup info
	var ss snapstate.SnapSetup
	task := ts.Tasks()[0]
	err = task.Get("snap-setup", &ss)
	c.Assert(err, IsNil)
	c.Assert(ss, DeepEquals, snapstate.SnapSetup{
		SnapPath: mockSnap,
		SideInfo: ss.SideInfo,
	})
	c.Assert(ss.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "mock",
		Revision: snap.R(-3),
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "mock", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.CurrentSideInfo(), DeepEquals, &snap.SideInfo{
		RealName: "mock",
		Channel:  "",
		Revision: snap.R(-3),
	})
	c.Assert(snapst.LocalRevision(), Equals, snap.R(-3))
}

func (s *snapmgrTestSuite) TestInstallOldSubsequentLocalRunThrough(c *C) {
	// use the real thing for this one
	snapstate.MockOpenSnapFile(backend.OpenSnapFile)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "mock", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "mock", Revision: snap.R(100001)},
		},
		Current: snap.R(100001),
	})

	mockSnap := makeTestSnap(c, `name: mock
version: 1.0`)
	chg := s.state.NewChange("install", "install a local snap")
	ts, err := snapstate.InstallPath(s.state, "mock", mockSnap, "", 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// ensure only local install was run, i.e. first action is pseudo-action current
	ops := s.fakeBackend.ops
	c.Assert(ops, HasLen, 7)
	c.Check(ops[0].op, Equals, "current")
	c.Check(ops[0].old, Equals, "/snap/mock/100001")
	// and setup-snap
	c.Check(ops[1].op, Equals, "setup-snap")
	c.Check(ops[1].name, Matches, `.*/mock_1.0_all.snap`)
	c.Check(ops[1].revno, Equals, snap.R("x1"))

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "mock", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.CurrentSideInfo(), DeepEquals, &snap.SideInfo{
		RealName: "mock",
		Channel:  "",
		Revision: snap.R(-1),
	})
	c.Assert(snapst.LocalRevision(), Equals, snap.R(-1))
}

func (s *snapmgrTestSuite) TestRemoveRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	c.Assert(s.fakeBackend.ops, HasLen, 6)
	expected := []fakeOp{
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-data",
			name: "/snap/some-snap/7",
		},
		{
			op:   "remove-snap-common-data",
			name: "/snap/some-snap/7",
		},
		{
			op:    "remove-snap-files",
			name:  "/snap/some-snap/7",
			stype: "app",
		},
		{
			op:   "discard-conns:Doing",
			name: "some-snap",
		},
	}
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	tasks := ts.Tasks()
	for _, t := range tasks {
		ss, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)

		var expSnapSetup *snapstate.SnapSetup
		if t.Kind() == "discard-conns" {
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
				},
			}
		} else {
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
					Revision: snap.R(7),
				},
			}
		}
		c.Check(ss, DeepEquals, expSnapSetup, Commentf(t.Kind()))
	}

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestRemoveWithManyRevisionsRunThrough(c *C) {
	si3 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(3),
	}

	si5 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(5),
	}

	si7 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si5, &si3, &si7},
		Current:  si7.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	c.Assert(s.fakeBackend.ops, HasLen, 10)
	expected := []fakeOp{
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		{
			op:    "remove-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:   "remove-snap-data",
			name: "/snap/some-snap/7",
		},
		{
			op:    "remove-snap-files",
			name:  "/snap/some-snap/7",
			stype: "app",
		},
		{
			op:   "remove-snap-data",
			name: "/snap/some-snap/3",
		},
		{
			op:    "remove-snap-files",
			name:  "/snap/some-snap/3",
			stype: "app",
		},
		{
			op:   "remove-snap-data",
			name: "/snap/some-snap/5",
		},
		{
			op:   "remove-snap-common-data",
			name: "/snap/some-snap/5",
		},
		{
			op:    "remove-snap-files",
			name:  "/snap/some-snap/5",
			stype: "app",
		},
		{
			op:   "discard-conns:Doing",
			name: "some-snap",
		},
	}
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	tasks := ts.Tasks()
	revnos := []snap.Revision{{7}, {3}, {5}}
	whichRevno := 0
	for _, t := range tasks {
		ss, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)

		var expSnapSetup *snapstate.SnapSetup
		if t.Kind() == "discard-conns" {
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
				},
			}
		} else {
			expSnapSetup = &snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					RealName: "some-snap",
					Revision: revnos[whichRevno],
				},
			}
		}

		c.Check(ss, DeepEquals, expSnapSetup, Commentf(t.Kind()))

		if t.Kind() == "discard-snap" {
			whichRevno++
		}
	}

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *snapmgrTestSuite) TestRemoveRefused(c *C) {
	si := snap.SideInfo{
		RealName: "gadget",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "gadget", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	_, err := snapstate.Remove(s.state, "gadget")

	c.Check(err, ErrorMatches, `snap "gadget" is not removable`)
}

func (s *snapmgrTestSuite) TestUpdateDoesGC(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current: snap.R(4),
	})

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", s.user.ID, 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// ensure garbage collection runs as the last tasks
	ops := s.fakeBackend.ops
	c.Assert(ops[len(ops)-5], DeepEquals, fakeOp{
		op:   "link-snap",
		name: "/snap/some-snap/11",
	})
	c.Assert(ops[len(ops)-4], DeepEquals, fakeOp{
		op:   "remove-snap-data",
		name: "/snap/some-snap/1",
	})
	c.Assert(ops[len(ops)-3], DeepEquals, fakeOp{
		op:    "remove-snap-files",
		name:  "/snap/some-snap/1",
		stype: "app",
	})
	c.Assert(ops[len(ops)-2], DeepEquals, fakeOp{
		op:   "remove-snap-data",
		name: "/snap/some-snap/2",
	})
	c.Assert(ops[len(ops)-1], DeepEquals, fakeOp{
		op:    "remove-snap-files",
		name:  "/snap/some-snap/2",
		stype: "app",
	})

}

func (s *snapmgrTestSuite) TestRevertNoRevertAgain(c *C) {
	siNew := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(77),
	}

	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si, &siNew},
		Current:  snap.R(7),
	})

	ts, err := snapstate.Revert(s.state, "some-snap")
	c.Assert(err, ErrorMatches, "no revision to revert to")
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestRevertNothingToRevertTo(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
	})

	ts, err := snapstate.Revert(s.state, "some-snap")
	c.Assert(err, ErrorMatches, "no revision to revert to")
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestRevertToRevisionNoValidVersion(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(77),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si, &si2},
		Current:  snap.R(77),
	})

	ts, err := snapstate.RevertToRevision(s.state, "some-snap", snap.R("99"))
	c.Assert(err, ErrorMatches, `cannot find revision 99 for snap "some-snap"`)
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestRevertToRevisionAlreadyCurrent(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(77),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si, &si2},
		Current:  snap.R(77),
	})

	ts, err := snapstate.RevertToRevision(s.state, "some-snap", snap.R("77"))
	c.Assert(err, ErrorMatches, `already on requested revision`)
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestRevertRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}
	siOld := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&siOld, &si},
		Current:  si.Revision,
	})

	chg := s.state.NewChange("revert", "revert a snap backwards")
	ts, err := snapstate.Revert(s.state, "some-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	c.Assert(s.fakeBackend.ops, HasLen, 4)
	expected := []fakeOp{
		fakeOp{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		fakeOp{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(2),
		},
		fakeOp{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				Revision: snap.R(2),
			},
		},
		fakeOp{
			op:   "link-snap",
			name: "/snap/some-snap/2",
		},
	}
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify that the R(2) version is active now and R(7) is still there
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Current, Equals, snap.R(2))
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Channel:  "",
		Revision: snap.R(2),
	})
	c.Assert(snapst.Sequence[1], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Channel:  "",
		Revision: snap.R(7),
	})
	c.Assert(snapst.Block(), DeepEquals, []snap.Revision{snap.R(7)})
}

func (s *snapmgrTestSuite) TestRevertWithLocalRevisionRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(-7),
	}
	siOld := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(-2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&siOld, &si},
		Current:  si.Revision,
	})

	chg := s.state.NewChange("revert", "revert a snap backwards")
	ts, err := snapstate.Revert(s.state, "some-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	c.Assert(s.fakeBackend.ops, HasLen, 4)

	// verify that LocalRevision is still -7
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.LocalRevision(), Equals, snap.R(-7))
}

func (s *snapmgrTestSuite) TestRevertToRevisionNewVersion(c *C) {
	siNew := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si, &siNew},
		Current:  snap.R(2),
	})

	chg := s.state.NewChange("revert", "revert a snap forward")
	ts, err := snapstate.RevertToRevision(s.state, "some-snap", snap.R(7))
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	c.Assert(s.fakeBackend.ops, HasLen, 4)
	expected := []fakeOp{
		fakeOp{
			op:   "unlink-snap",
			name: "/snap/some-snap/2",
		},
		fakeOp{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		fakeOp{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				Revision: snap.R(7),
			},
		},
		fakeOp{
			op:   "link-snap",
			name: "/snap/some-snap/7",
		},
	}
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify that the R(7) version is active now
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Current, Equals, snap.R(7))
	c.Assert(snapst.Sequence, HasLen, 2)

	c.Assert(snapst.Block(), HasLen, 0)
}

func (s *snapmgrTestSuite) TestRevertTotalUndoRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si, &si2},
		Current:  si2.Revision,
	})

	chg := s.state.NewChange("revert", "revert a snap")
	ts, err := snapstate.Revert(s.state, "some-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	tasks := ts.Tasks()
	last := tasks[len(tasks)-1]

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(last)
	chg.AddTask(terr)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := []fakeOp{
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/2",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				Revision: snap.R(1),
			},
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/1",
		},
		// undoing everything from here down...
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/1",
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/2",
		},
	}
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Current, Equals, si2.Revision)
}

func (s *snapmgrTestSuite) TestRevertUndoRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(2),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si, &si2},
		Current:  si2.Revision,
	})

	chg := s.state.NewChange("revert", "install a revert")
	ts, err := snapstate.Revert(s.state, "some-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.linkSnapFailTrigger = "/snap/some-snap/1"

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := []fakeOp{
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/2",
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				Revision: snap.R(1),
			},
		},
		{
			op:   "link-snap.failed",
			name: "/snap/some-snap/1",
		},
		// undo stuff here
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/1",
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(1),
		},
		{
			op:   "link-snap",
			name: "/snap/some-snap/2",
		},
	}

	// ensure all our tasks ran
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Current, Equals, snap.R(2))
}

func (s *snapmgrTestSuite) TestEnableDoesNotEnableAgain(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&si},
		Current:  snap.R(7),
		Active:   true,
	})

	ts, err := snapstate.Enable(s.state, "some-snap")
	c.Assert(err, ErrorMatches, `snap "some-snap" already enabled`)
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestEnableRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		Active:   false,
	})

	chg := s.state.NewChange("enable", "enable a snap")
	ts, err := snapstate.Enable(s.state, "some-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	c.Assert(s.fakeBackend.ops, HasLen, 2)
	expected := []fakeOp{
		fakeOp{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				Revision: snap.R(7),
			},
		},
		fakeOp{
			op:   "link-snap",
			name: "/snap/some-snap/7",
		},
	}
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
}

func (s *snapmgrTestSuite) TestDisableRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		Active:   true,
	})

	chg := s.state.NewChange("disable", "disable a snap")
	ts, err := snapstate.Disable(s.state, "some-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	c.Assert(s.fakeBackend.ops, HasLen, 1)
	expected := []fakeOp{
		fakeOp{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
	}
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, false)
}

func (s *snapmgrTestSuite) TestDisableDoesNotEnableAgain(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&si},
		Current:  snap.R(7),
		Active:   false,
	})

	ts, err := snapstate.Disable(s.state, "some-snap")
	c.Assert(err, ErrorMatches, `snap "some-snap" already disabled`)
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestUndoMountSnapFailsInCopyData(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", s.user.ID, 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.copySnapDataFailTrigger = "/snap/some-snap/11"

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := []fakeOp{
		{
			op:    "storesvc-snap",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:  "current",
			old: "<no-current>",
		},
		{
			op:   "open-snap-file",
			name: "downloaded-snap-path",
		},
		{
			op:    "setup-snap",
			name:  "downloaded-snap-path",
			revno: snap.R(11),
		},
		{
			op:   "copy-data.failed",
			name: "/snap/some-snap/11",
			old:  "<no-old>",
		},
		{
			op:    "undo-setup-snap",
			name:  "/snap/some-snap/11",
			stype: "app",
		},
	}
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)
}

type snapmgrQuerySuite struct {
	st *state.State
}

var _ = Suite(&snapmgrQuerySuite{})

func (s *snapmgrQuerySuite) SetUpTest(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	s.st = st

	dirs.SetRootDir(c.MkDir())

	// Write a snap.yaml with fake name
	sideInfo11 := &snap.SideInfo{RealName: "name1", Revision: snap.R(11), EditedSummary: "s11"}
	sideInfo12 := &snap.SideInfo{RealName: "name1", Revision: snap.R(12), EditedSummary: "s12"}
	snaptest.MockSnap(c, `
name: name0
version: 1.1
description: |
    Lots of text`, sideInfo11)
	snaptest.MockSnap(c, `
name: name0
version: 1.2
description: |
    Lots of text`, sideInfo12)
	snapstate.Set(st, "name1", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfo11, sideInfo12},
		Current:  sideInfo12.Revision,
		SnapType: "app",
	})

	// have also a snap being installed
	/*
		snapstate.Set(st, "installing", &snapstate.SnapState{
			Candidate: &snap.SideInfo{RealName: "installing", Revision: snap.R(1)},
		})
	*/
}

func (s *snapmgrQuerySuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *snapmgrQuerySuite) TestInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	info, err := snapstate.Info(st, "name1", snap.R(11))
	c.Assert(err, IsNil)

	c.Check(info.Name(), Equals, "name1")
	c.Check(info.Revision, Equals, snap.R(11))
	c.Check(info.Summary(), Equals, "s11")
	c.Check(info.Version, Equals, "1.1")
	c.Check(info.Description(), Equals, "Lots of text")
}

func (s *snapmgrQuerySuite) TestSnapStateCurrentInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(st, "name1", &snapst)
	c.Assert(err, IsNil)

	info, err := snapst.CurrentInfo()
	c.Assert(err, IsNil)

	c.Check(info.Name(), Equals, "name1")
	c.Check(info.Revision, Equals, snap.R(12))
	c.Check(info.Summary(), Equals, "s12")
	c.Check(info.Version, Equals, "1.2")
	c.Check(info.Description(), Equals, "Lots of text")
}

func (s *snapmgrQuerySuite) TestSnapStateCurrentInfoErrNoCurrent(c *C) {
	snapst := new(snapstate.SnapState)
	_, err := snapst.CurrentInfo()
	c.Assert(err, Equals, snapstate.ErrNoCurrent)

}

func (s *snapmgrQuerySuite) TestCurrentInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	info, err := snapstate.CurrentInfo(st, "name1")
	c.Assert(err, IsNil)

	c.Check(info.Name(), Equals, "name1")
	c.Check(info.Revision, Equals, snap.R(12))
}

func (s *snapmgrQuerySuite) TestCurrentInfoAbsent(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	_, err := snapstate.CurrentInfo(st, "absent")
	c.Assert(err, ErrorMatches, `cannot find snap "absent"`)
}

func (s *snapmgrQuerySuite) TestActiveInfos(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	infos, err := snapstate.ActiveInfos(st)
	c.Assert(err, IsNil)

	c.Check(infos, HasLen, 1)

	c.Check(infos[0].Name(), Equals, "name1")
	c.Check(infos[0].Revision, Equals, snap.R(12))
	c.Check(infos[0].Summary(), Equals, "s12")
	c.Check(infos[0].Version, Equals, "1.2")
	c.Check(infos[0].Description(), Equals, "Lots of text")
}

func (s *snapmgrQuerySuite) TestGadgetInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	_, err := snapstate.GadgetInfo(st)
	c.Assert(err, Equals, state.ErrNoState)

	sideInfoGadget := &snap.SideInfo{
		RealName: "gadget",
		Revision: snap.R(2),
	}
	snaptest.MockSnap(c, `
name: gadget
type: gadget
version: gadget
`, sideInfoGadget)
	snapstate.Set(st, "gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfoGadget},
		Current:  sideInfoGadget.Revision,
	})

	info, err := snapstate.GadgetInfo(st)
	c.Assert(err, IsNil)

	c.Check(info.Name(), Equals, "gadget")
	c.Check(info.Revision, Equals, snap.R(2))
	c.Check(info.Version, Equals, "gadget")
	c.Check(info.Type, Equals, snap.TypeGadget)
}

func (s *snapmgrQuerySuite) TestPreviousSideInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(st, "name1", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.CurrentSideInfo(), NotNil)
	c.Assert(snapst.CurrentSideInfo().Revision, Equals, snap.R(12))
	c.Assert(snapstate.PreviousSideInfo(&snapst), NotNil)
	c.Assert(snapstate.PreviousSideInfo(&snapst).Revision, Equals, snap.R(11))
}

func (s *snapmgrQuerySuite) TestPreviousSideInfoNoCurrent(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	snapst := &snapstate.SnapState{}
	c.Assert(snapstate.PreviousSideInfo(snapst), IsNil)
}

func (s *snapmgrQuerySuite) TestAll(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	snapStates, err := snapstate.All(st)
	c.Assert(err, IsNil)

	c.Check(snapStates, HasLen, 1)

	var snapst *snapstate.SnapState

	for name, sst := range snapStates {
		c.Assert(name, Equals, "name1")
		snapst = sst
	}

	c.Check(snapst.Active, Equals, true)
	c.Check(snapst.CurrentSideInfo(), NotNil)

	info12, err := snap.ReadInfo("name1", snapst.CurrentSideInfo())
	c.Assert(err, IsNil)

	c.Check(info12.Name(), Equals, "name1")
	c.Check(info12.Revision, Equals, snap.R(12))
	c.Check(info12.Summary(), Equals, "s12")
	c.Check(info12.Version, Equals, "1.2")
	c.Check(info12.Description(), Equals, "Lots of text")

	info11, err := snap.ReadInfo("name1", snapst.Sequence[0])
	c.Assert(err, IsNil)

	c.Check(info11.Name(), Equals, "name1")
	c.Check(info11.Revision, Equals, snap.R(11))
	c.Check(info11.Version, Equals, "1.1")
}

func (s *snapmgrQuerySuite) TestAllEmptyAndEmptyNormalisation(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	snapStates, err := snapstate.All(st)
	c.Assert(err, IsNil)
	c.Check(snapStates, HasLen, 0)

	snapstate.Set(st, "foo", nil)

	snapStates, err = snapstate.All(st)
	c.Assert(err, IsNil)
	c.Check(snapStates, HasLen, 0)

	snapstate.Set(st, "foo", &snapstate.SnapState{})

	snapStates, err = snapstate.All(st)
	c.Assert(err, IsNil)
	c.Check(snapStates, HasLen, 0)
}

func (s *snapmgrTestSuite) TestTrySetsTryMode(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// make mock try dir
	tryYaml := filepath.Join(c.MkDir(), "meta", "snap.yaml")
	err := os.MkdirAll(filepath.Dir(tryYaml), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(tryYaml, []byte("name: foo\nversion: 1.0"), 0644)
	c.Assert(err, IsNil)

	chg := s.state.NewChange("try", "try snap")
	ts, err := snapstate.TryPath(s.state, "foo", filepath.Dir(filepath.Dir(tryYaml)), 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// verify snap is in TryMode
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.TryMode(), Equals, true)
}

func (s *snapmgrTestSuite) TestTryUndoRemovesTryFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// simulate existing state for foo
	var snapst snapstate.SnapState
	snapst.Sequence = []*snap.SideInfo{
		{
			RealName: "foo",
			Revision: snap.R(23),
		},
	}
	snapst.Current = snap.R(23)
	snapstate.Set(s.state, "foo", &snapst)
	c.Check(snapst.TryMode(), Equals, false)

	chg := s.state.NewChange("try", "try snap")
	ts, err := snapstate.TryPath(s.state, "foo", c.MkDir(), 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	last := ts.Tasks()[len(ts.Tasks())-1]
	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(last)
	chg.AddTask(terr)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// verify snap is not in try mode, the state got undone
	err = snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.TryMode(), Equals, false)
}

type snapStateSuite struct{}

var _ = Suite(&snapStateSuite{})

func (s *snapStateSuite) TestSnapStateDevMode(c *C) {
	snapst := &snapstate.SnapState{}
	c.Check(snapst.DevMode(), Equals, false)
	snapst.Flags = snapstate.DevMode
	c.Check(snapst.DevMode(), Equals, true)
}

func (s *snapStateSuite) TestSnapStateModeSetters(c *C) {
	snapst := &snapstate.SnapState{}
	c.Check(snapst.DevMode(), Equals, false)
	c.Check(snapst.TryMode(), Equals, false)

	snapst.SetTryMode(true)
	c.Check(snapst.DevMode(), Equals, false)
	c.Check(snapst.TryMode(), Equals, true)

	snapst.SetDevMode(true)
	c.Check(snapst.DevMode(), Equals, true)
	c.Check(snapst.TryMode(), Equals, true)

	snapst.SetTryMode(false)
	c.Check(snapst.DevMode(), Equals, true)
	c.Check(snapst.TryMode(), Equals, false)

	snapst.SetDevMode(false)
	c.Check(snapst.DevMode(), Equals, false)
	c.Check(snapst.TryMode(), Equals, false)

	snapst.SetDevMode(true)
	c.Check(snapst.DevMode(), Equals, true)
	c.Check(snapst.TryMode(), Equals, false)
}

func (s *snapStateSuite) TestSnapStateType(c *C) {
	snapst := &snapstate.SnapState{}
	_, err := snapst.Type()
	c.Check(err, ErrorMatches, "snap type unset")

	snapst.SetType(snap.TypeKernel)
	typ, err := snapst.Type()
	c.Assert(err, IsNil)
	c.Check(typ, Equals, snap.TypeKernel)
}

func (s *snapStateSuite) TestCurrentSideInfoEmpty(c *C) {
	var snapst snapstate.SnapState
	c.Check(snapst.CurrentSideInfo(), IsNil)
	c.Check(snapst.Current.Unset(), Equals, true)
}

func (s *snapStateSuite) TestCurrentSideInfoSimple(c *C) {
	si1 := &snap.SideInfo{Revision: snap.R(1)}
	snapst := snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1},
		Current:  snap.R(1),
	}
	c.Check(snapst.CurrentSideInfo(), DeepEquals, si1)
}

func (s *snapStateSuite) TestCurrentSideInfoInOrder(c *C) {
	si1 := &snap.SideInfo{Revision: snap.R(1)}
	si2 := &snap.SideInfo{Revision: snap.R(2)}
	snapst := snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1, si2},
		Current:  snap.R(2),
	}
	c.Check(snapst.CurrentSideInfo(), DeepEquals, si2)
}

func (s *snapStateSuite) TestCurrentSideInfoOutOfOrder(c *C) {
	si1 := &snap.SideInfo{Revision: snap.R(1)}
	si2 := &snap.SideInfo{Revision: snap.R(2)}
	snapst := snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1, si2},
		Current:  snap.R(1),
	}
	c.Check(snapst.CurrentSideInfo(), DeepEquals, si1)
}

func (s *snapStateSuite) TestCurrentSideInfoInconsistent(c *C) {
	snapst := snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{Revision: snap.R(1)},
		},
	}
	c.Check(func() { snapst.CurrentSideInfo() }, PanicMatches, `snapst.Current and snapst.Sequence out of sync:.*`)
}

func (s *snapStateSuite) TestCurrentSideInfoInconsistentWithCurrent(c *C) {
	snapst := snapstate.SnapState{Current: snap.R(17)}
	c.Check(func() { snapst.CurrentSideInfo() }, PanicMatches, `cannot find snapst.Current in the snapst.Sequence`)
}

type snapSetupSuite struct{}

var _ = Suite(&snapSetupSuite{})

func (s *snapSetupSuite) TestDevMode(c *C) {
	ss := &snapstate.SnapSetup{}
	c.Check(ss.DevMode(), Equals, false)
	ss.Flags = snapstate.DevMode
	c.Check(ss.DevMode(), Equals, true)
}

type canRemoveSuite struct{}

var _ = Suite(&canRemoveSuite{})

func (s *canRemoveSuite) TestAppAreAlwaysOKToRemove(c *C) {
	info := &snap.Info{
		Type: snap.TypeApp,
	}
	info.RealName = "foo"

	c.Check(snapstate.CanRemove(info, false), Equals, true)
	c.Check(snapstate.CanRemove(info, true), Equals, true)
}

func (s *canRemoveSuite) TestActiveGadgetsAreNotOK(c *C) {
	info := &snap.Info{
		Type: snap.TypeGadget,
	}
	info.RealName = "foo"

	c.Check(snapstate.CanRemove(info, false), Equals, true)
	c.Check(snapstate.CanRemove(info, true), Equals, false)
}

func (s *canRemoveSuite) TestActiveOSAndKernelAreNotOK(c *C) {
	os := &snap.Info{
		Type: snap.TypeOS,
	}
	os.RealName = "os"
	kernel := &snap.Info{
		Type: snap.TypeKernel,
	}
	kernel.RealName = "krnl"

	c.Check(snapstate.CanRemove(os, false), Equals, true)
	c.Check(snapstate.CanRemove(os, true), Equals, false)

	c.Check(snapstate.CanRemove(kernel, false), Equals, true)
	c.Check(snapstate.CanRemove(kernel, true), Equals, false)
}

func (s *snapmgrTestSuite) TestUpdateTasksWithOldCurrent(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si1 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	si2 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)}
	si3 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)}
	si4 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{si1, si2, si3, si4},
		Current:  snap.R(2),
	})

	// run the update
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", s.user.ID, 0)
	c.Assert(err, IsNil)

	// and ensure that it will remove the revisions after "current"
	// (si3, si4)
	var ss snapstate.SnapSetup
	tasks := ts.Tasks()
	c.Check(tasks[len(tasks)-1].Kind(), Equals, "discard-snap")
	c.Check(tasks[len(tasks)-2].Kind(), Equals, "clear-snap")
	err = tasks[len(tasks)-2].Get("snap-setup", &ss)
	c.Assert(err, IsNil)
	c.Check(ss.Revision(), Equals, si4.Revision)

	c.Check(tasks[len(tasks)-3].Kind(), Equals, "discard-snap")
	c.Check(tasks[len(tasks)-4].Kind(), Equals, "clear-snap")
	err = tasks[len(tasks)-4].Get("snap-setup", &ss)
	c.Assert(err, IsNil)
	c.Check(ss.Revision(), Equals, si3.Revision)

	// ensure only si3, si4 are removed
	c.Check(tasks[len(tasks)-5].Kind(), Not(Equals), "discard-snap")
}

func (s *snapmgrTestSuite) TestUpdateCanDoBackwardsNotQuiteYet(c *C) {
	si7 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	si11 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si7, &si11},
		Current:  si11.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", "channel-for-7", s.user.ID, 0)
	c.Assert(err, ErrorMatches, `revision 7 of snap "some-snap" already installed`)
}

func (s *snapmgrTestSuite) TestSnapStateNoLocalRevision(c *C) {
	si7 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(-7),
	}
	si11 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(-11),
	}
	snapst := &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&si7, &si11},
		Current:  si7.Revision,
	}
	c.Assert(snapst.LocalRevision(), Equals, snap.R(-11))
}

func (s *snapmgrTestSuite) TestSnapStateLocalRevision(c *C) {
	si7 := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}
	snapst := &snapstate.SnapState{
		Sequence: []*snap.SideInfo{&si7},
		Current:  si7.Revision,
	}
	c.Assert(snapst.LocalRevision().Unset(), Equals, true)
}
