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
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

func TestSnapManager(t *testing.T) { TestingT(t) }

type snapmgrTestSuite struct {
	state   *state.State
	snapmgr *snapstate.SnapManager

	fakeBackend *fakeSnappyBackend

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
	s.fakeBackend = &fakeSnappyBackend{
		fakeCurrentProgress: 75,
		fakeTotalProgress:   100,
	}
	s.state = state.New(nil)

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
	s.user, err = auth.NewUser(s.state, "username", "macaroon", []string{"discharge"})
	c.Assert(err, IsNil)
	s.state.Unlock()
}

func (s *snapmgrTestSuite) TearDownTest(c *C) {
	s.reset()
}

func verifyInstallUpdateTasks(c *C, curActive bool, ts *state.TaskSet, st *state.State) {
	i := 0
	n := 5
	if curActive {
		n++
	}
	c.Assert(ts.Tasks(), HasLen, n)
	// all tasks are accounted
	c.Assert(st.NumTask(), Equals, n)
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
}

func (s *snapmgrTestSuite) TestInstallTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", 0, 0)
	c.Assert(err, IsNil)
	verifyInstallUpdateTasks(c, false, ts, s.state)
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
		Sequence: []*snap.SideInfo{{OfficialName: "some-snap", Revision: snap.R(11)}},
	})

	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", s.user.ID, 0)
	c.Assert(err, IsNil)
	verifyInstallUpdateTasks(c, true, ts, s.state)

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
		Sequence: []*snap.SideInfo{{OfficialName: "some-snap", Revision: snap.R(11)}},
	})

	ts, err := snapstate.Update(s.state, "some-snap", "", s.user.ID, 0)
	c.Assert(err, IsNil)

	var ss snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &ss)
	c.Assert(err, IsNil)

	c.Check(ss.Channel, Equals, "edge")
}

func (s *snapmgrTestSuite) TestUpdateConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{OfficialName: "some-snap"}},
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
			{OfficialName: "foo"},
		},
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
		Sequence: []*snap.SideInfo{{OfficialName: "some-snap"}},
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
	c.Assert(s.fakeBackend.ops, DeepEquals, []fakeOp{
		fakeOp{
			op:       "download",
			macaroon: s.user.Macaroon,
			name:     "some-snap",
			channel:  "some-channel",
		},
		fakeOp{
			op:  "current",
			old: "<no-current>",
		},
		fakeOp{
			op:   "open-snap-file",
			name: "downloaded-snap-path",
		},
		fakeOp{
			op:    "setup-snap",
			name:  "downloaded-snap-path",
			revno: snap.R(11),
		},
		fakeOp{
			op:   "copy-data",
			name: "/snap/some-snap/11",
			old:  "<no-old>",
		},
		fakeOp{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		fakeOp{
			op: "candidate",
			sinfo: snap.SideInfo{
				OfficialName: "some-snap",
				Channel:      "some-channel",
				SnapID:       "snapIDsnapidsnapidsnapidsnapidsn",
				Revision:     snap.R(11),
			},
		},
		fakeOp{
			op:   "link-snap",
			name: "/snap/some-snap/11",
		},
	})

	// check progress
	task := ts.Tasks()[0]
	cur, total := task.Progress()
	c.Assert(cur, Equals, s.fakeBackend.fakeCurrentProgress)
	c.Assert(total, Equals, s.fakeBackend.fakeTotalProgress)

	// verify snap-setup in the task state
	var ss snapstate.SnapSetup
	err = task.Get("snap-setup", &ss)
	c.Assert(err, IsNil)
	c.Assert(ss, DeepEquals, snapstate.SnapSetup{
		Name:     "some-snap",
		Revision: snap.R(11),
		Channel:  "some-channel",
		UserID:   s.user.ID,
		SnapPath: "downloaded-snap-path",
	})

	// verify snaps in the system state
	var snaps map[string]*snapstate.SnapState
	err = s.state.Get("snaps", &snaps)
	c.Assert(err, IsNil)

	snapst := snaps["some-snap"]
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Channel, Equals, "some-channel")
	c.Assert(snapst.Candidate, IsNil)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		OfficialName: "some-snap",
		Channel:      "some-channel",
		SnapID:       "snapIDsnapidsnapidsnapidsnapidsn",
		Revision:     snap.R(11),
	})
}

func (s *snapmgrTestSuite) TestUpdateRunThrough(c *C) {
	si := snap.SideInfo{
		OfficialName: "some-snap",
		Revision:     snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
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
		fakeOp{
			op:       "download",
			macaroon: s.user.Macaroon,
			name:     "some-snap",
			channel:  "some-channel",
		},
		fakeOp{
			op:  "current",
			old: "/snap/some-snap/7",
		},
		fakeOp{
			op:   "open-snap-file",
			name: "downloaded-snap-path",
		},
		fakeOp{
			op:    "setup-snap",
			name:  "downloaded-snap-path",
			revno: snap.R(11),
		},
		fakeOp{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		fakeOp{
			op:   "copy-data",
			name: "/snap/some-snap/11",
			old:  "/snap/some-snap/7",
		},
		fakeOp{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		fakeOp{
			op: "candidate",
			sinfo: snap.SideInfo{
				OfficialName: "some-snap",
				SnapID:       "snapIDsnapidsnapidsnapidsnapidsn",
				Channel:      "some-channel",
				Revision:     snap.R(11),
			},
		},
		fakeOp{
			op:   "link-snap",
			name: "/snap/some-snap/11",
		},
	}

	// ensure all our tasks ran
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// check progress
	task := ts.Tasks()[0]
	cur, total := task.Progress()
	c.Assert(cur, Equals, s.fakeBackend.fakeCurrentProgress)
	c.Assert(total, Equals, s.fakeBackend.fakeTotalProgress)

	// verify snapSetup info
	var ss snapstate.SnapSetup
	err = task.Get("snap-setup", &ss)
	c.Assert(err, IsNil)
	c.Assert(ss, DeepEquals, snapstate.SnapSetup{
		Name:    "some-snap",
		Channel: "some-channel",
		Flags:   0,
		UserID:  s.user.ID,

		Revision: snap.R(11),

		SnapPath: "downloaded-snap-path",
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Candidate, IsNil)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		OfficialName: "some-snap",
		Channel:      "",
		Revision:     snap.R(7),
	})
	c.Assert(snapst.Sequence[1], DeepEquals, &snap.SideInfo{
		OfficialName: "some-snap",
		Channel:      "some-channel",
		SnapID:       "snapIDsnapidsnapidsnapidsnapidsn",
		Revision:     snap.R(11),
	})
}

func (s *snapmgrTestSuite) TestUpdateUndoRunThrough(c *C) {
	si := snap.SideInfo{
		OfficialName: "some-snap",
		Revision:     snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
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
			op:       "download",
			macaroon: s.user.Macaroon,
			name:     "some-snap",
			channel:  "some-channel",
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
				OfficialName: "some-snap",
				SnapID:       "snapIDsnapidsnapidsnapidsnapidsn",
				Channel:      "some-channel",
				Revision:     snap.R(11),
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
			op:   "undo-setup-snap",
			name: "/snap/some-snap/11",
		},
	}

	// ensure all our tasks ran
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Candidate, IsNil)
	c.Assert(snapst.Sequence, HasLen, 1)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		OfficialName: "some-snap",
		Channel:      "",
		Revision:     snap.R(7),
	})
}

func (s *snapmgrTestSuite) TestUpdateTotalUndoRunThrough(c *C) {
	si := snap.SideInfo{
		OfficialName: "some-snap",
		Revision:     snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "stable",
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
			op:       "download",
			macaroon: s.user.Macaroon,
			name:     "some-snap",
			channel:  "some-channel",
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
				OfficialName: "some-snap",
				SnapID:       "snapIDsnapidsnapidsnapidsnapidsn",
				Channel:      "some-channel",
				Revision:     snap.R(11),
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
			op:   "undo-setup-snap",
			name: "/snap/some-snap/11",
		},
	}

	// ensure all our tasks ran
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Channel, Equals, "stable")
	c.Assert(snapst.Candidate, IsNil)
	c.Assert(snapst.Sequence, HasLen, 1)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		OfficialName: "some-snap",
		Channel:      "",
		Revision:     snap.R(7),
	})
}

func (s *snapmgrTestSuite) TestUpdateSameRevisionRunThrough(c *C) {
	si := snap.SideInfo{
		OfficialName: "some-snap",
		Revision:     snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "channel-for-7", s.user.ID, 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	expected := []fakeOp{
		{
			op:       "download",
			macaroon: s.user.Macaroon,
			name:     "some-snap",
			channel:  "channel-for-7",
		},
	}

	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*revision 7 of snap "some-snap" already installed.*`)

	// ensure all our tasks ran
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Candidate, IsNil)
	c.Assert(snapst.Sequence, HasLen, 1)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		OfficialName: "some-snap",
		Channel:      "",
		Revision:     snap.R(7),
	})
}

func makeTestSnap(c *C, snapYamlContent string) (snapFilePath string) {
	tmpdir := c.MkDir()
	os.MkdirAll(filepath.Join(tmpdir, "meta"), 0755)
	snapYamlFn := filepath.Join(tmpdir, "meta", "snap.yaml")
	ioutil.WriteFile(snapYamlFn, []byte(snapYamlContent), 0644)
	err := osutil.ChDir(tmpdir, func() error {
		var err error
		snapFilePath, err = snaptest.BuildSquashfsSnap(tmpdir, "")
		c.Assert(err, IsNil)
		return err
	})
	c.Assert(err, IsNil)
	return filepath.Join(tmpdir, snapFilePath)

}

func (s *snapmgrTestSuite) TestInstallFirstLocalRunThrough(c *C) {
	// use the real thing for this one
	snapstate.MockOpenSnapFile(snapstate.OpenSnapFileImpl)

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
	c.Check(s.fakeBackend.ops[4].sinfo, DeepEquals, snap.SideInfo{Revision: snap.R(-1)})
	c.Check(s.fakeBackend.ops[5].op, Equals, "link-snap")
	c.Check(s.fakeBackend.ops[5].name, Equals, "/snap/mock/x1")

	// verify snapSetup info
	var ss snapstate.SnapSetup
	task := ts.Tasks()[0]
	err = task.Get("snap-setup", &ss)
	c.Assert(err, IsNil)
	c.Assert(ss, DeepEquals, snapstate.SnapSetup{
		Name:     "mock",
		Revision: snap.R(-1),
		SnapPath: mockSnap,
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "mock", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Candidate, IsNil)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		OfficialName: "",
		Channel:      "",
		Revision:     snap.R(-1),
	})
	c.Assert(snapst.LocalRevision, Equals, snap.R(-1))
}

func (s *snapmgrTestSuite) TestInstallSubsequentLocalRunThrough(c *C) {
	// use the real thing for this one
	snapstate.MockOpenSnapFile(snapstate.OpenSnapFileImpl)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "mock", &snapstate.SnapState{
		Active:        true,
		Sequence:      []*snap.SideInfo{{Revision: snap.R(-2)}},
		LocalRevision: snap.R(-2),
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
	c.Check(s.fakeBackend.ops[5].sinfo, DeepEquals, snap.SideInfo{Revision: snap.R(-3)})
	c.Check(s.fakeBackend.ops[6].op, Equals, "link-snap")
	c.Check(s.fakeBackend.ops[6].name, Equals, "/snap/mock/x3")

	// verify snapSetup info
	var ss snapstate.SnapSetup
	task := ts.Tasks()[0]
	err = task.Get("snap-setup", &ss)
	c.Assert(err, IsNil)
	c.Assert(ss, DeepEquals, snapstate.SnapSetup{
		Name:     "mock",
		Revision: snap.R(-3),
		SnapPath: mockSnap,
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "mock", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Candidate, IsNil)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Current(), DeepEquals, &snap.SideInfo{
		OfficialName: "",
		Channel:      "",
		Revision:     snap.R(-3),
	})
	c.Assert(snapst.LocalRevision, Equals, snap.R(-3))
}

func (s *snapmgrTestSuite) TestInstallOldSubsequentLocalRunThrough(c *C) {
	// use the real thing for this one
	snapstate.MockOpenSnapFile(snapstate.OpenSnapFileImpl)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "mock", &snapstate.SnapState{
		Active:        true,
		Sequence:      []*snap.SideInfo{{Revision: snap.R(100001)}},
		LocalRevision: snap.R(100001),
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
	c.Assert(snapst.Candidate, IsNil)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Current(), DeepEquals, &snap.SideInfo{
		OfficialName: "",
		Channel:      "",
		Revision:     snap.R(-1),
	})
	c.Assert(snapst.LocalRevision, Equals, snap.R(-1))
}

func (s *snapmgrTestSuite) TestRemoveRunThrough(c *C) {
	si := snap.SideInfo{
		OfficialName: "some-snap",
		Revision:     snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
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
		fakeOp{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		fakeOp{
			op:    "remove-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		fakeOp{
			op:   "remove-snap-data",
			name: "/snap/some-snap/7",
		},
		fakeOp{
			op:   "remove-snap-common-data",
			name: "/snap/some-snap/7",
		},
		fakeOp{
			op:   "remove-snap-files",
			name: "/snap/some-snap/7",
		},
		fakeOp{
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
				Name: "some-snap",
			}
		} else {
			expSnapSetup = &snapstate.SnapSetup{
				Name:     "some-snap",
				Revision: snap.R(7),
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
		OfficialName: "some-snap",
		Revision:     snap.R(3),
	}

	si5 := snap.SideInfo{
		OfficialName: "some-snap",
		Revision:     snap.R(5),
	}

	si7 := snap.SideInfo{
		OfficialName: "some-snap",
		Revision:     snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si5, &si3, &si7},
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
			op:   "remove-snap-files",
			name: "/snap/some-snap/7",
		},
		{
			op:   "remove-snap-data",
			name: "/snap/some-snap/3",
		},
		{
			op:   "remove-snap-files",
			name: "/snap/some-snap/3",
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
			op:   "remove-snap-files",
			name: "/snap/some-snap/5",
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
				Name: "some-snap",
			}
		} else {
			expSnapSetup = &snapstate.SnapSetup{
				Name:     "some-snap",
				Revision: revnos[whichRevno],
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
		OfficialName: "gadget",
		Revision:     snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "gadget", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
	})

	_, err := snapstate.Remove(s.state, "gadget")

	c.Check(err, ErrorMatches, `snap "gadget" is not removable`)
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
	sideInfo11 := &snap.SideInfo{OfficialName: "name1", Revision: snap.R(11), EditedSummary: "s11"}
	sideInfo12 := &snap.SideInfo{OfficialName: "name1", Revision: snap.R(12), EditedSummary: "s12"}
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
	})
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

func (s *snapmgrQuerySuite) TestCurrent(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	info, err := snapstate.Current(st, "name1")
	c.Assert(err, IsNil)

	c.Check(info.Name(), Equals, "name1")
	c.Check(info.Revision, Equals, snap.R(12))
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

	sideInfoGadget := &snap.SideInfo{Revision: snap.R(2)}
	snaptest.MockSnap(c, `
name: gadget
type: gadget
version: gadget
`, sideInfoGadget)
	snapstate.Set(st, "gadget", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfoGadget},
	})

	info, err := snapstate.GadgetInfo(st)
	c.Assert(err, IsNil)

	c.Check(info.Name(), Equals, "gadget")
	c.Check(info.Revision, Equals, snap.R(2))
	c.Check(info.Version, Equals, "gadget")
	c.Check(info.Type, Equals, snap.TypeGadget)
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
	c.Check(snapst.Current(), NotNil)

	info12, err := snap.ReadInfo("name1", snapst.Current())
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
			OfficialName: "foo",
			Revision:     snap.R(23),
		},
	}
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
	info.OfficialName = "foo"

	c.Check(snapstate.CanRemove(info, false), Equals, true)
	c.Check(snapstate.CanRemove(info, true), Equals, true)
}

func (s *canRemoveSuite) TestActiveGadgetsAreNotOK(c *C) {
	info := &snap.Info{
		Type: snap.TypeGadget,
	}
	info.OfficialName = "foo"

	c.Check(snapstate.CanRemove(info, false), Equals, true)
	c.Check(snapstate.CanRemove(info, true), Equals, false)
}

func (s *canRemoveSuite) TestActiveOSAndKernelAreNotOK(c *C) {
	os := &snap.Info{
		Type: snap.TypeOS,
	}
	os.OfficialName = "os"
	kernel := &snap.Info{
		Type: snap.TypeKernel,
	}
	kernel.OfficialName = "krnl"

	c.Check(snapstate.CanRemove(os, false), Equals, true)
	c.Check(snapstate.CanRemove(os, true), Equals, false)

	c.Check(snapstate.CanRemove(kernel, false), Equals, true)
	c.Check(snapstate.CanRemove(kernel, true), Equals, false)
}
