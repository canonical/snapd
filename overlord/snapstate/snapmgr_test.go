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
	ver     string
	channel string
	flags   int
	active  bool
	op      string

	fakeCurrentProgress int
	fakeTotalProgress   int
}

func (f *fakeSnappyBackend) Install(name, channel string, flags snappy.InstallFlags, p progress.Meter) (string, error) {
	f.op = "install"
	f.name = name
	f.channel = channel
	p.SetTotal(float64(f.fakeTotalProgress))
	p.Set(float64(f.fakeCurrentProgress))
	return "", nil
}

func (f *fakeSnappyBackend) Update(name, channel string, flags snappy.InstallFlags, p progress.Meter) error {
	f.op = "update"
	f.name = name
	f.channel = channel
	return nil
}

func (f *fakeSnappyBackend) Remove(name string, flags snappy.RemoveFlags, p progress.Meter) error {
	f.op = "remove"
	f.name = name
	return nil
}

func (f *fakeSnappyBackend) Purge(name string, flags snappy.PurgeFlags, p progress.Meter) error {
	f.op = "purge"
	f.name = name
	return nil
}

func (f *fakeSnappyBackend) Rollback(name, ver string, p progress.Meter) (string, error) {
	f.op = "rollback"
	f.name = name
	f.ver = ver
	return "", nil
}

func (f *fakeSnappyBackend) SetActive(name string, active bool, p progress.Meter) error {
	f.op = "set-active"
	f.name = name
	f.active = active
	return nil
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

	c.Assert(ts.Tasks(), HasLen, 1)
	c.Assert(ts.Tasks()[0].Kind(), Equals, "install-snap")
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
	s.snapmgr.Ensure()
	s.snapmgr.Wait()
	defer s.snapmgr.Stop()

	s.state.Lock()

	c.Assert(s.fakeBackend.op, Equals, "install")
	c.Assert(s.fakeBackend.name, Equals, "some-snap")
	c.Assert(s.fakeBackend.channel, Equals, "some-channel")

	// check progress
	task := ts.Tasks()[0]
	cur, total := task.Progress()
	c.Assert(cur, Equals, s.fakeBackend.fakeCurrentProgress)
	c.Assert(total, Equals, s.fakeBackend.fakeTotalProgress)
}

func (s *snapmgrTestSuite) TestRemoveIntegration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("remove", "remove a snap")
	ts, err := snapstate.Remove(s.state, "some-remove-snap", 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.snapmgr.Ensure()
	s.snapmgr.Wait()
	defer s.snapmgr.Stop()
	s.state.Lock()

	c.Assert(s.fakeBackend.op, Equals, "remove")
	c.Assert(s.fakeBackend.name, Equals, "some-remove-snap")
}

func (s *snapmgrTestSuite) TestUpdateIntegration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("udpate", "update a snap")
	ts, err := snapstate.Update(s.state, "some-update-snap", "some-channel", 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.snapmgr.Ensure()
	s.snapmgr.Wait()
	defer s.snapmgr.Stop()
	s.state.Lock()

	c.Assert(s.fakeBackend.op, Equals, "update")
	c.Assert(s.fakeBackend.name, Equals, "some-update-snap")
	c.Assert(s.fakeBackend.channel, Equals, "some-channel")
}

func (s *snapmgrTestSuite) TestPurgeIntegration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("purge", "purge a snap")
	ts, err := snapstate.Purge(s.state, "some-snap-to-purge", 0)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.snapmgr.Ensure()
	s.snapmgr.Wait()
	defer s.snapmgr.Stop()
	s.state.Lock()

	c.Assert(s.fakeBackend.op, Equals, "purge")
	c.Assert(s.fakeBackend.name, Equals, "some-snap-to-purge")
}

func (s *snapmgrTestSuite) TestRollbackIntegration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("rollback", "rollback a snap")
	ts, err := snapstate.Rollback(s.state, "some-snap-to-rollback", "1.0")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.snapmgr.Ensure()
	s.snapmgr.Wait()
	defer s.snapmgr.Stop()
	s.state.Lock()

	c.Assert(s.fakeBackend.op, Equals, "rollback")
	c.Assert(s.fakeBackend.name, Equals, "some-snap-to-rollback")
	c.Assert(s.fakeBackend.ver, Equals, "1.0")
}

func (s *snapmgrTestSuite) TestSetActive(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("setActive", "make snap active")
	ts, err := snapstate.SetActive(s.state, "some-snap-to-activate", true)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.snapmgr.Ensure()
	s.snapmgr.Wait()
	defer s.snapmgr.Stop()
	s.state.Lock()

	c.Assert(s.fakeBackend.op, Equals, "set-active")
	c.Assert(s.fakeBackend.name, Equals, "some-snap-to-activate")
	c.Assert(s.fakeBackend.active, Equals, true)
}

func (s *snapmgrTestSuite) TestSetInactive(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("set-inactive", "make snap inactive")
	ts, err := snapstate.SetActive(s.state, "some-snap-to-inactivate", false)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.snapmgr.Ensure()
	s.snapmgr.Wait()
	defer s.snapmgr.Stop()
	s.state.Lock()

	c.Assert(s.fakeBackend.op, Equals, "set-active")
	c.Assert(s.fakeBackend.name, Equals, "some-snap-to-inactivate")
	c.Assert(s.fakeBackend.active, Equals, false)
}
