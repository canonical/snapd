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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

type linkSnapSuite struct {
	state   *state.State
	snapmgr *snapstate.SnapManager

	fakeBackend *fakeSnappyBackend

	stateBackend *witnessRestartReqStateBackend

	reset func()
}

var _ = Suite(&linkSnapSuite{})

type witnessRestartReqStateBackend struct {
	restartRequested bool
}

func (b *witnessRestartReqStateBackend) Checkpoint([]byte) error {
	return nil
}

func (b *witnessRestartReqStateBackend) RequestRestart(t state.RestartType) {
	b.restartRequested = true
}

func (b *witnessRestartReqStateBackend) EnsureBefore(time.Duration) {}

func (s *linkSnapSuite) SetUpTest(c *C) {
	s.stateBackend = &witnessRestartReqStateBackend{}
	s.fakeBackend = &fakeSnappyBackend{}
	s.state = state.New(s.stateBackend)

	var err error
	s.snapmgr, err = snapstate.Manager(s.state)
	c.Assert(err, IsNil)
	s.snapmgr.AddForeignTaskHandlers(s.fakeBackend)

	snapstate.SetSnapManagerBackend(s.snapmgr, s.fakeBackend)

	s.reset = snapstate.MockReadInfo(s.fakeBackend.ReadInfo)
}

func (s *linkSnapSuite) TearDownTest(c *C) {
	s.reset()
}

func (s *linkSnapSuite) TestDoLinkSnapSuccess(c *C) {
	s.state.Lock()
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Channel: "beta",
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

	typ, err := snapst.Type()
	c.Check(err, IsNil)
	c.Check(typ, Equals, snap.TypeApp)

	c.Check(snapst.Active, Equals, true)
	c.Check(snapst.Sequence, HasLen, 1)
	c.Check(snapst.Current, Equals, snap.R(33))
	c.Check(snapst.Channel, Equals, "beta")
	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(s.stateBackend.restartRequested, Equals, false)
}

func (s *linkSnapSuite) TestDoUndoLinkSnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	si := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(33),
	}
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Channel:  "beta",
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
	c.Assert(err, Equals, state.ErrNoState)
	c.Check(t.Status(), Equals, state.UndoneStatus)
}

func (s *linkSnapSuite) TestDoLinkSnapTryToCleanupOnError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	si := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(35),
	}
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Channel:  "beta",
	})

	s.fakeBackend.linkSnapFailTrigger = "/snap/foo/35"
	s.state.NewChange("dummy", "...").AddTask(t)
	s.state.Unlock()

	s.snapmgr.Ensure()
	s.snapmgr.Wait()

	s.state.Lock()

	// state as expected
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, Equals, state.ErrNoState)

	// tried to cleanup
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:    "candidate",
			sinfo: *si,
		},
		{
			op:   "link-snap.failed",
			name: "/snap/foo/35",
		},
		{
			op:   "unlink-snap",
			name: "/snap/foo/35",
		},
	})
}

func (s *linkSnapSuite) TestDoLinkSnapSuccessCoreRestarts(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	si := &snap.SideInfo{
		RealName: "core",
		Revision: snap.R(33),
	}
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
	})
	s.state.NewChange("dummy", "...").AddTask(t)

	s.state.Unlock()

	s.snapmgr.Ensure()
	s.snapmgr.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "core", &snapst)
	c.Assert(err, IsNil)

	typ, err := snapst.Type()
	c.Check(err, IsNil)
	c.Check(typ, Equals, snap.TypeOS)

	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(s.stateBackend.restartRequested, Equals, true)
	c.Check(t.Log(), HasLen, 1)
	c.Check(t.Log()[0], Matches, `.*INFO Requested daemon restart\.`)
}

func (s *linkSnapSuite) TestDoUndoLinkSnapSequenceDidNotHaveCandidate(c *C) {
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
		Sequence: []*snap.SideInfo{si1},
		Current:  si1.Revision,
	})
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si2,
		Channel:  "beta",
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
	c.Check(snapst.Active, Equals, false)
	c.Check(snapst.Sequence, HasLen, 1)
	c.Check(snapst.Current, Equals, snap.R(1))
	c.Check(t.Status(), Equals, state.UndoneStatus)
}

func (s *linkSnapSuite) TestDoUndoLinkSnapSequenceHadCandidate(c *C) {
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
		Sequence: []*snap.SideInfo{si1, si2},
		Current:  si2.Revision,
	})
	t := s.state.NewTask("link-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si1,
		Channel:  "beta",
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
	c.Check(snapst.Active, Equals, false)
	c.Check(snapst.Sequence, HasLen, 2)
	c.Check(snapst.Current, Equals, snap.R(2))
	c.Check(t.Status(), Equals, state.UndoneStatus)
}
