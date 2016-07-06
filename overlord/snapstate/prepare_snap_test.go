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

type prepareSnapSuite struct {
	state   *state.State
	snapmgr *snapstate.SnapManager

	fakeBackend *fakeSnappyBackend

	reset func()
}

var _ = Suite(&prepareSnapSuite{})

func (s *prepareSnapSuite) SetUpTest(c *C) {
	s.fakeBackend = &fakeSnappyBackend{}
	s.state = state.New(nil)

	var err error
	s.snapmgr, err = snapstate.Manager(s.state)
	c.Assert(err, IsNil)
	s.snapmgr.AddForeignTaskHandlers(s.fakeBackend)

	snapstate.SetSnapManagerBackend(s.snapmgr, s.fakeBackend)

	s.reset = snapstate.MockReadInfo(s.fakeBackend.ReadInfo)
}

func (s *prepareSnapSuite) TearDownTest(c *C) {
	s.reset()
}

func (s *prepareSnapSuite) TestDoPrepareSnapSimple(c *C) {
	s.state.Lock()
	t := s.state.NewTask("prepare-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		Name: "foo",
	})
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()

	s.snapmgr.Ensure()
	s.snapmgr.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Candidate, DeepEquals, &snap.SideInfo{
		Revision: snap.R(-1),
	})
	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (s *prepareSnapSuite) TestDoPrepareSnapSetsCandidate(c *C) {
	s.state.Lock()

	si1 := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(1),
	}
	si2 := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(2),
	}
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1, si2},
		Active:   true,
		Current:  si2.Revision,
	})

	t := s.state.NewTask("prepare-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		Name:     "foo",
		Revision: snap.R(1),
	})
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()

	s.snapmgr.Ensure()
	s.snapmgr.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Candidate, DeepEquals, si1)
	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (s *prepareSnapSuite) TestDoUndoPrepareSnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si1 := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(1),
	}
	si2 := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(2),
	}

	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Sequence:  []*snap.SideInfo{si1, si2},
		Active:    true,
		Candidate: si2,
	})
	t := s.state.NewTask("prepare-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		Name:     "foo",
		Revision: snap.R(1),
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

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Active, Equals, true)
	c.Check(snapst.Candidate, IsNil)
	c.Check(t.Status(), Equals, state.UndoneStatus)
}
