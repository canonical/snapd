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

		activeSnaps: make(map[string]*snap.Info),
	}
	s.state = state.New(nil)

	var err error
	s.snapmgr, err = snapstate.Manager(s.state)
	c.Assert(err, IsNil)
	s.snapmgr.AddForeignTaskHandlers()
	snapstate.SetSnapManagerBackend(s.snapmgr, s.fakeBackend)
	snapstate.SetSnapstateBackend(s.fakeBackend)
}

func verifyInstallUpdateTasks(c *C, ts *state.TaskSet) {
	i := 0
	c.Assert(ts.Tasks(), HasLen, 5)
	c.Assert(ts.Tasks()[i].Kind(), Equals, "download-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "mount-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "copy-snap-data")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "setup-snap-security")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "link-snap")
}

func (s *snapmgrTestSuite) TestInstallTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", 0)
	c.Assert(err, IsNil)
	verifyInstallUpdateTasks(c, ts)
}

func (s *snapmgrTestSuite) TestUpdateTasks(c *C) {
	s.fakeBackend.activeSnaps["some-snap"] = &snap.Info{
		SuggestedName: "some-snap",
	}

	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", 0)
	c.Assert(err, IsNil)
	verifyInstallUpdateTasks(c, ts)
}

func (s *snapmgrTestSuite) TestRemoveTasks(c *C) {
	s.fakeBackend.activeSnaps["foo"] = &snap.Info{
		SuggestedName: "foo",
	}

	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Remove(s.state, "foo", 0)
	c.Assert(err, IsNil)

	i := 0
	c.Assert(ts.Tasks(), HasLen, 4)
	c.Assert(ts.Tasks()[i].Kind(), Equals, "unlink-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "remove-snap-security")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "remove-snap-data")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "remove-snap-files")
}

func (s *snapmgrTestSuite) TestInstallIntegration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Install(s.state, "some-snap.mvo", "some-channel", 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle()
	defer s.snapmgr.Stop()
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(s.fakeBackend.ops, DeepEquals, []fakeOp{
		fakeOp{
			op:      "download",
			name:    "some-snap.mvo",
			channel: "some-channel",
		},
		fakeOp{
			op:   "check-snap",
			name: "downloaded-snap-path",
		},
		fakeOp{
			op:    "setup-snap",
			name:  "downloaded-snap-path",
			revno: 11,
		},
		fakeOp{
			op:   "copy-data",
			name: "/snap/some-snap/11",
		},
		fakeOp{
			op: "candidate",
			sinfo: snap.SideInfo{
				OfficialName: "some-snap",
				Channel:      "some-channel",
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
		Name:      "some-snap",
		Revision:  11,
		Developer: "mvo",
		Channel:   "some-channel",
		SnapPath:  "downloaded-snap-path",
	})

	// verify snaps in the system state
	var snaps map[string]*snapstate.SnapStateForTests
	err = s.state.Get("snaps", &snaps)
	c.Assert(err, IsNil)

	snapst := snaps["some-snap"]
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Candidate, IsNil)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		OfficialName: "some-snap",
		Channel:      "some-channel",
		Revision:     11,
	})
}

func (s *snapmgrTestSuite) TestUpdateIntegration(c *C) {
	s.fakeBackend.activeSnaps["some-snap"] = &snap.Info{
		SideInfo: snap.SideInfo{
			OfficialName: "some-snap",
			Revision:     7,
		},
	}

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap.mvo", "some-channel", snappy.DoInstallGC)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle()
	defer s.snapmgr.Stop()
	s.state.Lock()

	// ensure all our tasks ran
	c.Assert(s.fakeBackend.ops, DeepEquals, []fakeOp{
		fakeOp{
			op:      "download",
			name:    "some-snap.mvo",
			channel: "some-channel",
		},
		fakeOp{
			op:    "check-snap",
			name:  "downloaded-snap-path",
			flags: int(snappy.DoInstallGC),
		},
		fakeOp{
			op:    "setup-snap",
			name:  "downloaded-snap-path",
			flags: int(snappy.DoInstallGC),
			revno: 11,
		},
		fakeOp{
			op:    "copy-data",
			name:  "/snap/some-snap/11",
			flags: int(snappy.DoInstallGC),
		},
		fakeOp{
			op: "candidate",
			sinfo: snap.SideInfo{
				OfficialName: "some-snap",
				Channel:      "some-channel",
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

	// verify snapSetup info
	var ss snapstate.SnapSetup
	err = task.Get("snap-setup", &ss)
	c.Assert(err, IsNil)
	c.Assert(ss, DeepEquals, snapstate.SnapSetup{
		Name:      "some-snap",
		Developer: "mvo",
		Channel:   "some-channel",
		Flags:     int(snappy.DoInstallGC),

		Revision: 11,

		SnapPath: "downloaded-snap-path",
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

func (s *snapmgrTestSuite) TestInstallLocalIntegration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := makeTestSnap(c, `name: mock
version: 1.0`)
	chg := s.state.NewChange("install", "install a local snap")
	ts, err := snapstate.Install(s.state, mockSnap, "", 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle()
	defer s.snapmgr.Stop()
	s.state.Lock()

	// ensure only local install was run, i.e. first action is check-snap
	c.Assert(s.fakeBackend.ops, HasLen, 5)
	c.Check(s.fakeBackend.ops[0].op, Equals, "check-snap")
	c.Check(s.fakeBackend.ops[0].name, Matches, `.*/mock_1.0_all.snap`)

	c.Check(s.fakeBackend.ops[3].op, Equals, "candidate")
	c.Check(s.fakeBackend.ops[3].sinfo, DeepEquals, snap.SideInfo{})

	// verify snapSetup info
	var ss snapstate.SnapSetup
	task := ts.Tasks()[0]
	err = task.Get("snap-setup", &ss)
	c.Assert(err, IsNil)
	c.Assert(ss, DeepEquals, snapstate.SnapSetup{
		Name:     "mock",
		Revision: 0,
		SnapPath: mockSnap,
	})
}

func (s *snapmgrTestSuite) TestRemoveIntegration(c *C) {
	s.fakeBackend.activeSnaps["some-snap"] = &snap.Info{
		SideInfo: snap.SideInfo{
			OfficialName: "some-name",
			Developer:    "mvo",
			Revision:     7,
		},
	}

	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-snap.mvo", 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle()
	defer s.snapmgr.Stop()
	s.state.Lock()

	c.Assert(s.fakeBackend.ops, HasLen, 4)
	c.Assert(s.fakeBackend.ops, DeepEquals, []fakeOp{
		fakeOp{
			op:   "can-remove",
			name: "/snap/some-snap/7",
		},
		fakeOp{
			op:   "unlink-snap",
			name: "/snap/some-snap/7",
		},
		fakeOp{
			op:    "remove-snap-data",
			name:  "some-snap",
			revno: 7,
		},
		fakeOp{
			op:   "remove-snap-files",
			name: "/snap/some-snap/7",
		},
	})

	// verify snapSetup info
	task := ts.Tasks()[0]
	var ss snapstate.SnapSetup
	err = task.Get("snap-setup", &ss)
	c.Assert(err, IsNil)
	c.Assert(ss, DeepEquals, snapstate.SnapSetup{
		Name:      "some-snap",
		Developer: "mvo",
		Revision:  7,
	})

}

func (s *snapmgrTestSuite) TestRollbackIntegration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("rollback", "rollback a snap")
	ts, err := snapstate.Rollback(s.state, "some-snap-to-rollback", "1.0")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle()
	defer s.snapmgr.Stop()
	s.state.Lock()

	c.Assert(s.fakeBackend.ops[0].op, Equals, "rollback")
	c.Assert(s.fakeBackend.ops[0].name, Equals, "some-snap-to-rollback")
	c.Assert(s.fakeBackend.ops[0].rollback, Equals, "1.0")
}

func (s *snapmgrTestSuite) TestActivate(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("setActive", "make snap active")
	ts, err := snapstate.Activate(s.state, "some-snap-to-activate")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle()
	defer s.snapmgr.Stop()
	s.state.Lock()

	c.Assert(s.fakeBackend.ops[0].op, Equals, "activate")
	c.Assert(s.fakeBackend.ops[0].name, Equals, "some-snap-to-activate")
	c.Assert(s.fakeBackend.ops[0].active, Equals, true)
}

func (s *snapmgrTestSuite) TestSetInactive(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("set-inactive", "make snap inactive")
	ts, err := snapstate.Deactivate(s.state, "some-snap-to-inactivate")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle()
	defer s.snapmgr.Stop()
	s.state.Lock()

	c.Assert(s.fakeBackend.ops[0].op, Equals, "activate")
	c.Assert(s.fakeBackend.ops[0].name, Equals, "some-snap-to-inactivate")
	c.Assert(s.fakeBackend.ops[0].active, Equals, false)
}

func (s *snapmgrTestSuite) TestSnapInfo(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// Write a snap.yaml with fake name
	dname := filepath.Join(dirs.SnapSnapsDir, "name", "11", "meta")
	err := os.MkdirAll(dname, 0775)
	c.Assert(err, IsNil)
	fname := filepath.Join(dname, "snap.yaml")
	err = ioutil.WriteFile(fname, []byte(`
name: ignored
version: 1.2
description: |
    Lots of text`), 0644)
	c.Assert(err, IsNil)

	snapInfo, err := snapstate.SnapInfo(s.state, "name", 11)
	c.Assert(err, IsNil)

	// TODO: This test is not faking the manifest so SideInfo is not present.
	// The test and the actual implementation need to be improved so that this
	// is not so hacky and that the manifest can go away.
	c.Check(snapInfo.Name(), Equals, "ignored")
	// Check that other values are read from YAML
	c.Check(snapInfo.Description(), Equals, "Lots of text")
	c.Check(snapInfo.Version, Equals, "1.2")
}
