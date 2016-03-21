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
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/overlord/snapstate"
	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snappy"
)

func TestSnapManager(t *testing.T) { TestingT(t) }

type snapmgrTestSuite struct {
	state   *state.State
	snapmgr *snapstate.SnapManager

	fakeBackend *fakeSnappyBackend
}

type fakeSnappyBackend struct {
	name    string
	channel string
	op      string
}

func (f *fakeSnappyBackend) Install(name, channel string, flags snappy.InstallFlags, p progress.Meter) (string, error) {
	f.op = "install"
	f.name = name
	f.channel = channel
	return "", nil
}

func (f *fakeSnappyBackend) Remove(name string, flags snappy.RemoveFlags, p progress.Meter) error {
	f.op = "remove"
	f.name = name
	return nil
}

var _ = Suite(&snapmgrTestSuite{})

func (s *snapmgrTestSuite) SetUpTest(c *C) {
	s.fakeBackend = &fakeSnappyBackend{}
	s.state = state.New(nil)

	var err error
	s.snapmgr, err = snapstate.Manager(s.state)
	c.Assert(err, IsNil)

	snapstate.SetSnapManagerBackend(s.snapmgr, s.fakeBackend)
}

func (s *snapmgrTestSuite) TestInstallAddsTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Install(s.state, "some-snap", "some-channel")
	c.Assert(err, IsNil)

	chg := s.state.NewChange("install", "installing foo")
	chg.AddTasks(ts)
	c.Assert(chg.Tasks(), HasLen, 1)
	c.Assert(chg.Tasks()[0].Kind(), Equals, "install-snap")
}

func (s *snapmgrTestSuite) TestRemoveAddsTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts, err := snapstate.Remove(s.state, "foo")
	c.Assert(err, IsNil)

	chg := s.state.NewChange("remove", "removing foo")
	chg.AddTasks(ts)
	c.Assert(chg.Tasks(), HasLen, 1)
	c.Assert(chg.Tasks()[0].Kind(), Equals, "remove-snap")
}

func (s *snapmgrTestSuite) TestInstallIntegration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Install(s.state, "some-snap", "some-channel")
	c.Assert(err, IsNil)
	chg.AddTasks(ts)

	s.state.Unlock()
	s.snapmgr.Ensure()
	s.snapmgr.Wait()
	defer s.snapmgr.Stop()

	s.state.Lock()

	c.Assert(s.fakeBackend.op, Equals, "install")
	c.Assert(s.fakeBackend.name, Equals, "some-snap")
	c.Assert(s.fakeBackend.channel, Equals, "some-channel")
}

func (s *snapmgrTestSuite) TestRemoveIntegration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-remove-snap")
	c.Assert(err, IsNil)
	chg.AddTasks(ts)

	s.state.Unlock()
	s.snapmgr.Ensure()
	s.snapmgr.Wait()
	defer s.snapmgr.Stop()
	s.state.Lock()

	c.Assert(s.fakeBackend.op, Equals, "remove")
	c.Assert(s.fakeBackend.name, Equals, "some-remove-snap")
}
