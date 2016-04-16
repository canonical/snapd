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

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/overlord/auth"
	"github.com/ubuntu-core/snappy/overlord/snapstate"
	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snappy"
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
	s.snapmgr.AddForeignTaskHandlers()

	// XXX: have just one, reset!
	snapstate.SetSnapManagerBackend(s.snapmgr, s.fakeBackend)
	snapstate.SetSnapstateBackend(s.fakeBackend)

	s.reset = snapstate.MockReadInfo(s.fakeBackend.ReadInfo)

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
	c.Assert(st.Tasks(), HasLen, n)
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

	_, err := snapstate.Install(s.state, "some-snap", "some-channel", 0, 0)
	c.Assert(err, IsNil)
	_, err = snapstate.Install(s.state, "some-snap", "some-channel", 0, 0)
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestInstallPathConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	_, err := snapstate.Install(s.state, "some-snap", "some-channel", 0, 0)
	c.Assert(err, IsNil)

	mockSnap := makeTestSnap(c, "name: some-snap\nversion: 1.0")
	_, err = snapstate.InstallPath(s.state, mockSnap, "", 0)
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestUpdateTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Channel:  "edge",
		Sequence: []*snap.SideInfo{{OfficialName: "some-snap", Revision: 11}},
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
		Sequence: []*snap.SideInfo{{OfficialName: "some-snap", Revision: 11}},
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

	_, err := snapstate.Update(s.state, "some-snap", "some-channel", s.user.ID, 0)
	c.Assert(err, IsNil)
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
			{OfficialName: "foo"},
		},
	})

	ts, err := snapstate.Remove(s.state, "foo", 0)
	c.Assert(err, IsNil)

	i := 0
	c.Assert(ts.Tasks(), HasLen, 4)
	// all tasks are accounted
	c.Assert(s.state.Tasks(), HasLen, 4)
	c.Assert(ts.Tasks()[i].Kind(), Equals, "unlink-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "remove-profiles")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "clear-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "discard-snap")
}

func (s *snapmgrTestSuite) TestRemoveLast(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{OfficialName: "foo"}},
	})

	ts, err := snapstate.Remove(s.state, "foo", 0)
	c.Assert(err, IsNil)

	i := 0
	c.Assert(ts.Tasks(), HasLen, 5)
	// all tasks are accounted
	c.Assert(s.state.Tasks(), HasLen, 5)
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

	_, err := snapstate.Remove(s.state, "some-snap", 0)
	c.Assert(err, IsNil)
	_, err = snapstate.Remove(s.state, "some-snap", 0)
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestInstallIntegration(c *C) {
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
			op:   "check-snap",
			name: "downloaded-snap-path",
			old:  "<no-current>",
		},
		fakeOp{
			op:    "setup-snap",
			name:  "downloaded-snap-path",
			revno: 11,
		},
		fakeOp{
			op:   "copy-data",
			name: "/snap/some-snap/11",
			old:  "<no-old>",
		},
		fakeOp{
			op: "candidate",
			sinfo: snap.SideInfo{
				OfficialName: "some-snap",
				Channel:      "some-channel",
				SnapID:       "snapIDsnapidsnapidsnapidsnapidsn",
				Revision:     11,
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
		Revision: 11,
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
		Revision:     11,
	})
}

func (s *snapmgrTestSuite) TestUpdateIntegration(c *C) {
	si := snap.SideInfo{
		OfficialName: "some-snap",
		Revision:     7,
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", s.user.ID, snappy.DoInstallGC)
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
			op:    "check-snap",
			name:  "downloaded-snap-path",
			flags: int(snappy.DoInstallGC),
			old:   "/snap/some-snap/7",
		},
		fakeOp{
			op:    "setup-snap",
			name:  "downloaded-snap-path",
			flags: int(snappy.DoInstallGC),
			revno: 11,
		},
		fakeOp{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		fakeOp{
			op:    "copy-data",
			name:  "/snap/some-snap/11",
			flags: int(snappy.DoInstallGC),
			old:   "/snap/some-snap/7",
		},
		fakeOp{
			op: "candidate",
			sinfo: snap.SideInfo{
				OfficialName: "some-snap",
				SnapID:       "snapIDsnapidsnapidsnapidsnapidsn",
				Channel:      "some-channel",
				Revision:     11,
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
		Flags:   int(snappy.DoInstallGC),
		UserID:  s.user.ID,

		Revision: 11,

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
		Revision:     7,
	})
	c.Assert(snapst.Sequence[1], DeepEquals, &snap.SideInfo{
		OfficialName: "some-snap",
		Channel:      "some-channel",
		SnapID:       "snapIDsnapidsnapidsnapidsnapidsn",
		Revision:     11,
	})
}

func (s *snapmgrTestSuite) TestUpdateUndoIntegration(c *C) {
	si := snap.SideInfo{
		OfficialName: "some-snap",
		Revision:     7,
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", s.user.ID, snappy.DoInstallGC)
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
			op:    "check-snap",
			name:  "downloaded-snap-path",
			flags: int(snappy.DoInstallGC),
			old:   "/snap/some-snap/7",
		},
		{
			op:    "setup-snap",
			name:  "downloaded-snap-path",
			flags: int(snappy.DoInstallGC),
			revno: 11,
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		{
			op:    "copy-data",
			name:  "/snap/some-snap/11",
			flags: int(snappy.DoInstallGC),
			old:   "/snap/some-snap/7",
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				OfficialName: "some-snap",
				SnapID:       "snapIDsnapidsnapidsnapidsnapidsn",
				Channel:      "some-channel",
				Revision:     11,
			},
		},
		{
			op:   "link-snap.failed",
			name: "/snap/some-snap/11",
		},
		// no unlink-snap here is expected!
		{
			op:   "undo-copy-snap-data",
			name: "/snap/some-snap/11",
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
		Revision:     7,
	})
}

func (s *snapmgrTestSuite) TestUpdateTotalUndoIntegration(c *C) {
	si := snap.SideInfo{
		OfficialName: "some-snap",
		Revision:     7,
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Channel:  "stable",
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", s.user.ID, snappy.DoInstallGC)
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
			op:    "check-snap",
			name:  "downloaded-snap-path",
			flags: int(snappy.DoInstallGC),
			old:   "/snap/some-snap/7",
		},
		{
			op:    "setup-snap",
			name:  "downloaded-snap-path",
			flags: int(snappy.DoInstallGC),
			revno: 11,
		},
		{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		{
			op:    "copy-data",
			name:  "/snap/some-snap/11",
			flags: int(snappy.DoInstallGC),
			old:   "/snap/some-snap/7",
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				OfficialName: "some-snap",
				SnapID:       "snapIDsnapidsnapidsnapidsnapidsn",
				Channel:      "some-channel",
				Revision:     11,
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
			op:   "undo-copy-snap-data",
			name: "/snap/some-snap/11",
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
		Revision:     7,
	})
}

func (s *snapmgrTestSuite) TestUpdateSameRevisionIntegration(c *C) {
	si := snap.SideInfo{
		OfficialName: "some-snap",
		Revision:     7,
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", "channel-for-7", s.user.ID, snappy.DoInstallGC)
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
		Revision:     7,
	})
}

func makeTestSnap(c *C, snapYamlContent string) (snapFilePath string) {
	tmpdir := c.MkDir()
	os.MkdirAll(filepath.Join(tmpdir, "meta"), 0755)
	snapYamlFn := filepath.Join(tmpdir, "meta", "snap.yaml")
	ioutil.WriteFile(snapYamlFn, []byte(snapYamlContent), 0644)
	err := osutil.ChDir(tmpdir, func() error {
		var err error
		snapFilePath, err = snappy.BuildSquashfsSnap(tmpdir, "")
		c.Assert(err, IsNil)
		return err
	})
	c.Assert(err, IsNil)
	return filepath.Join(tmpdir, snapFilePath)

}

func (s *snapmgrTestSuite) TestInstallFirstLocalIntegration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := makeTestSnap(c, `name: mock
version: 1.0`)
	chg := s.state.NewChange("install", "install a local snap")
	ts, err := snapstate.InstallPath(s.state, mockSnap, "", 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// ensure only local install was run, i.e. first action is check-snap
	c.Assert(s.fakeBackend.ops, HasLen, 5)
	c.Check(s.fakeBackend.ops[0].op, Equals, "check-snap")
	c.Check(s.fakeBackend.ops[0].name, Matches, `.*/mock_1.0_all.snap`)

	c.Check(s.fakeBackend.ops[3].op, Equals, "candidate")
	c.Check(s.fakeBackend.ops[3].sinfo, DeepEquals, snap.SideInfo{Revision: 100001})
	c.Check(s.fakeBackend.ops[4].op, Equals, "link-snap")
	c.Check(s.fakeBackend.ops[4].name, Equals, "/snap/mock/100001")

	// verify snapSetup info
	var ss snapstate.SnapSetup
	task := ts.Tasks()[0]
	err = task.Get("snap-setup", &ss)
	c.Assert(err, IsNil)
	c.Assert(ss, DeepEquals, snapstate.SnapSetup{
		Name:     "mock",
		Revision: 100001,
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
		Revision:     100001,
	})
	c.Assert(snapst.LocalRevision, Equals, 100001)
}

func (s *snapmgrTestSuite) TestInstallSubequentLocalIntegration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "mock", &snapstate.SnapState{
		Active:        true,
		Sequence:      []*snap.SideInfo{{Revision: 100002}},
		LocalRevision: 100002,
	})

	mockSnap := makeTestSnap(c, `name: mock
version: 1.0`)
	chg := s.state.NewChange("install", "install a local snap")
	ts, err := snapstate.InstallPath(s.state, mockSnap, "", 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	// ensure only local install was run, i.e. first action is check-snap
	c.Assert(s.fakeBackend.ops, HasLen, 6)
	c.Check(s.fakeBackend.ops[0].op, Equals, "check-snap")
	c.Check(s.fakeBackend.ops[0].name, Matches, `.*/mock_1.0_all.snap`)

	c.Check(s.fakeBackend.ops[2].op, Equals, "unlink-snap")
	c.Check(s.fakeBackend.ops[2].name, Equals, "/snap/mock/100002")

	c.Check(s.fakeBackend.ops[3].op, Equals, "copy-data")
	c.Check(s.fakeBackend.ops[3].name, Equals, "/snap/mock/100003")
	c.Check(s.fakeBackend.ops[3].old, Equals, "/snap/mock/100002")

	c.Check(s.fakeBackend.ops[4].op, Equals, "candidate")
	c.Check(s.fakeBackend.ops[4].sinfo, DeepEquals, snap.SideInfo{Revision: 100003})
	c.Check(s.fakeBackend.ops[5].op, Equals, "link-snap")
	c.Check(s.fakeBackend.ops[5].name, Equals, "/snap/mock/100003")

	// verify snapSetup info
	var ss snapstate.SnapSetup
	task := ts.Tasks()[0]
	err = task.Get("snap-setup", &ss)
	c.Assert(err, IsNil)
	c.Assert(ss, DeepEquals, snapstate.SnapSetup{
		Name:     "mock",
		Revision: 100003,
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
		Revision:     100003,
	})
	c.Assert(snapst.LocalRevision, Equals, 100003)
}

func (s *snapmgrTestSuite) TestRemoveIntegration(c *C) {
	si := snap.SideInfo{
		OfficialName: "some-snap",
		Revision:     7,
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
	})

	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap", 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle()
	s.state.Lock()

	c.Assert(s.fakeBackend.ops, HasLen, 4)
	expected := []fakeOp{
		fakeOp{
			op:     "can-remove",
			name:   "/snap/some-snap/7",
			active: true,
		},
		fakeOp{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		fakeOp{
			op:   "remove-snap-data",
			name: "/snap/some-snap/7",
		},
		fakeOp{
			op:   "remove-snap-files",
			name: "/snap/some-snap/7",
		},
	}
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	tasks := ts.Tasks()
	// snap-setup is in discard-snap above discard-conns.
	task := tasks[len(tasks)-2]
	var ss snapstate.SnapSetup
	err = task.Get("snap-setup", &ss)
	c.Assert(err, IsNil)
	c.Assert(ss, DeepEquals, snapstate.SnapSetup{
		Name:     "some-snap",
		Revision: 7,
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, Equals, state.ErrNoState)
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
	dname := filepath.Join(snap.MountDir("name1", 11), "meta")
	err := os.MkdirAll(dname, 0775)
	c.Assert(err, IsNil)
	fname := filepath.Join(dname, "snap.yaml")
	err = ioutil.WriteFile(fname, []byte(`
name: name0
version: 1.1
description: |
    Lots of text`), 0644)
	c.Assert(err, IsNil)

	dname = filepath.Join(snap.MountDir("name1", 12), "meta")
	err = os.MkdirAll(dname, 0775)
	c.Assert(err, IsNil)
	fname = filepath.Join(dname, "snap.yaml")
	err = ioutil.WriteFile(fname, []byte(`
name: name0
version: 1.2
description: |
    Lots of text`), 0644)
	c.Assert(err, IsNil)

	snapstate.Set(st, "name1", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{OfficialName: "name1", Revision: 11, EditedSummary: "s11"},
			{OfficialName: "name1", Revision: 12, EditedSummary: "s12"},
		},
	})
}

func (s *snapmgrQuerySuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *snapmgrQuerySuite) TestInfo(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	info, err := snapstate.Info(st, "name1", 11)
	c.Assert(err, IsNil)

	c.Check(info.Name(), Equals, "name1")
	c.Check(info.Revision, Equals, 11)
	c.Check(info.Summary(), Equals, "s11")
	c.Check(info.Version, Equals, "1.1")
	c.Check(info.Description(), Equals, "Lots of text")
}

func (s *snapmgrQuerySuite) TestActiveInfos(c *C) {
	st := s.st
	st.Lock()
	defer st.Unlock()

	infos, err := snapstate.ActiveInfos(st)
	c.Assert(err, IsNil)

	c.Check(infos, HasLen, 1)

	c.Check(infos[0].Name(), Equals, "name1")
	c.Check(infos[0].Revision, Equals, 12)
	c.Check(infos[0].Summary(), Equals, "s12")
	c.Check(infos[0].Version, Equals, "1.2")
	c.Check(infos[0].Description(), Equals, "Lots of text")
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
	c.Check(info12.Revision, Equals, 12)
	c.Check(info12.Summary(), Equals, "s12")
	c.Check(info12.Version, Equals, "1.2")
	c.Check(info12.Description(), Equals, "Lots of text")

	info11, err := snap.ReadInfo("name1", snapst.Sequence[0])
	c.Assert(err, IsNil)

	c.Check(info11.Name(), Equals, "name1")
	c.Check(info11.Revision, Equals, 11)
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

type snapStateSuite struct{}

var _ = Suite(&snapStateSuite{})

func (s *snapStateSuite) TestSnapStateDevMode(c *C) {
	snapst := &snapstate.SnapState{}
	c.Check(snapst.DevMode(), Equals, false)
	snapst.Flags = snapstate.DevMode
	c.Check(snapst.DevMode(), Equals, true)
}

type snapSetupSuite struct{}

var _ = Suite(&snapSetupSuite{})

func (s *snapSetupSuite) TestDevMode(c *C) {
	ss := &snapstate.SnapSetup{}
	c.Check(ss.DevMode(), Equals, false)
	ss.Flags = int(snappy.DeveloperMode)
	c.Check(ss.DevMode(), Equals, true)
}
