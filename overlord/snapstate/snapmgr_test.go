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
	"github.com/ubuntu-core/snappy/overlord/snapstate"
	"github.com/ubuntu-core/snappy/overlord/state"
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
	}
	s.state = state.New(nil)

	var err error
	s.snapmgr, err = snapstate.Manager(s.state)
	c.Assert(err, IsNil)

	snapstate.SetSnapManagerBackend(s.snapmgr, s.fakeBackend)
	snapstate.SetSnapstateBackend(s.fakeBackend)
}

func (s *snapmgrTestSuite) TestInstallTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", 0)
	c.Assert(err, IsNil)

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

func (s *snapmgrTestSuite) TestRemoveTasks(c *C) {
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
	c.Assert(ts.Tasks()[i].Kind(), Equals, "remove-snap-files")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "remove-snap-data")
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
	c.Assert(s.fakeBackend.ops, HasLen, 6)
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
			op:   "setup-snap",
			name: "downloaded-snap-path",
		},
		fakeOp{
			op:   "copy-data",
			name: "/snap/some-snap/1.0",
		},
		fakeOp{
			op:   "setup-snap-security",
			name: "/snap/some-snap/1.0",
		},
		fakeOp{
			op:   "link-snap",
			name: "/snap/some-snap/1.0",
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
		Version:   "1.0",

		SnapPath: "downloaded-snap-path",

		OldName:    "an-active-snap",
		OldVersion: "1.64872",
	})
}

func (s *snapmgrTestSuite) TestInstallLocalIntegration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	mockSnap := filepath.Join(c.MkDir(), "mock.snap")
	err := ioutil.WriteFile(mockSnap, nil, 0644)
	c.Assert(err, IsNil)

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
	c.Check(s.fakeBackend.ops[0].name, Matches, `.*/mock.snap`)
}

func (s *snapmgrTestSuite) TestRemoveIntegration(c *C) {
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

	c.Assert(s.fakeBackend.ops, HasLen, 5)
	c.Assert(s.fakeBackend.ops, DeepEquals, []fakeOp{
		fakeOp{
			op:   "can-remove",
			name: "/snap/some-snap/1.64872",
		},
		fakeOp{
			op:   "unlink-snap",
			name: "/snap/some-snap/1.64872",
		},
		fakeOp{
			op:   "remove-snap-security",
			name: "/snap/some-snap/1.64872",
		},
		fakeOp{
			op:   "remove-snap-files",
			name: "/snap/some-snap/1.64872",
		},
		fakeOp{
			op:      "remove-snap-data",
			name:    "some-snap",
			version: "1.64872",
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
		Version:   "1.64872",
	})

}

func (s *snapmgrTestSuite) TestUpdateIntegration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("udpate", "update a snap")
	ts, err := snapstate.Update(s.state, "some-update-snap", "some-channel", 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle()
	defer s.snapmgr.Stop()
	s.state.Lock()

	c.Assert(s.fakeBackend.ops[0].op, Equals, "update")
	c.Assert(s.fakeBackend.ops[0].name, Equals, "some-update-snap")
	c.Assert(s.fakeBackend.ops[0].channel, Equals, "some-channel")
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
	c.Assert(s.fakeBackend.ops[0].version, Equals, "1.0")
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
	dname := filepath.Join(dirs.SnapSnapsDir, "name", "version", "meta")
	err := os.MkdirAll(dname, 0775)
	c.Assert(err, IsNil)
	fname := filepath.Join(dname, "snap.yaml")
	err = ioutil.WriteFile(fname, []byte(`
name: ignored
description: |
    Lots of text`), 0644)
	c.Assert(err, IsNil)

	snapInfo, err := snapstate.SnapInfo(s.state, "name", "version")
	c.Assert(err, IsNil)

	// Check that the name in the YAML is being ignored.
	c.Check(snapInfo.Name(), Equals, "name")
	// Check that other values are read from YAML
	c.Check(snapInfo.Description(), Equals, "Lots of text")
}
