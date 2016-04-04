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
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

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
}

func (s *snapmgrTestSuite) TestInstallTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", 0)
	c.Assert(err, IsNil)

	i := 0
	c.Assert(ts.Tasks(), HasLen, 6)
	c.Assert(ts.Tasks()[i].Kind(), Equals, "download-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "check-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "mount-snap")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "generate-security")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "copy-snap-data")
	i++
	c.Assert(ts.Tasks()[i].Kind(), Equals, "finalize-snap-install")
}

func (s *snapmgrTestSuite) TestRemoveTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Remove(s.state, "foo", 0)
	c.Assert(err, IsNil)

	c.Assert(ts.Tasks(), HasLen, 1)
	c.Assert(ts.Tasks()[0].Kind(), Equals, "remove-snap")
}

func (s *snapmgrTestSuite) TestInstallIntegration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Install(s.state, "some-snap", "some-channel", 0)
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
			name:    "some-snap",
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
			op:   "generate-security-profile",
			name: "some-inst-path",
		},
		fakeOp{
			op:   "copy-data",
			name: "some-inst-path",
		},
		fakeOp{
			op:   "finalize-snap",
			name: "some-inst-path",
		},
	})

	// check progress
	task := ts.Tasks()[0]
	cur, total := task.Progress()
	c.Assert(cur, Equals, s.fakeBackend.fakeCurrentProgress)
	c.Assert(total, Equals, s.fakeBackend.fakeTotalProgress)
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
	ts, err := snapstate.Remove(s.state, "some-remove-snap", 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle()
	defer s.snapmgr.Stop()
	s.state.Lock()

	c.Assert(s.fakeBackend.ops[0].op, Equals, "remove")
	c.Assert(s.fakeBackend.ops[0].name, Equals, "some-remove-snap")
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
	c.Assert(s.fakeBackend.ops[0].ver, Equals, "1.0")
}

func (s *snapmgrTestSuite) TestActivate(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("setActive", "make snap active")
	ts, err := snapstate.Activate(s.state, "some-snap-to-activate", true)
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
	ts, err := snapstate.Activate(s.state, "some-snap-to-inactivate", false)
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
