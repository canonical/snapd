// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type prereqSuite struct {
	state     *state.State
	snapmgr   *snapstate.SnapManager
	fakeStore *fakeStore

	fakeBackend *fakeSnappyBackend

	reset func()
}

var _ = Suite(&prereqSuite{})

func (s *prereqSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.fakeBackend = &fakeSnappyBackend{}
	s.state = state.New(nil)
	s.state.Lock()
	defer s.state.Unlock()

	s.fakeStore = &fakeStore{
		state:       s.state,
		fakeBackend: s.fakeBackend,
	}
	snapstate.ReplaceStore(s.state, s.fakeStore)

	var err error
	s.snapmgr, err = snapstate.Manager(s.state)
	c.Assert(err, IsNil)
	s.snapmgr.AddForeignTaskHandlers(s.fakeBackend)
	snapstate.SetSnapManagerBackend(s.snapmgr, s.fakeBackend)

	s.reset = snapstate.MockReadInfo(s.fakeBackend.ReadInfo)
}

func (s *prereqSuite) TearDownTest(c *C) {
	s.reset()
}

func (s *prereqSuite) TestDoPrereqNothingToDo(c *C) {
	s.state.Lock()

	si1 := &snap.SideInfo{
		RealName: "core",
		Revision: snap.R(1),
	}
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si1},
		Current:  si1.Revision,
	})

	t := s.state.NewTask("prerequisites", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
	})
	s.state.NewChange("dummy", "...").AddTask(t)
	s.state.Unlock()

	s.snapmgr.Ensure()
	s.snapmgr.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(s.fakeBackend.ops, HasLen, 0)
	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (s *prereqSuite) TestDoPrereqTalksToStoreAndQueues(c *C) {
	s.state.Lock()
	t := s.state.NewTask("prerequisites", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Channel: "beta",
		Base:    "some-base",
		Prereq:  []string{"prereq1", "prereq2"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)
	s.state.Unlock()

	s.snapmgr.Ensure()
	s.snapmgr.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:    "storesvc-snap",
			name:  "prereq1",
			revno: snap.R(11),
		},
		{
			op:    "storesvc-snap",
			name:  "prereq2",
			revno: snap.R(11),
		},
		{
			op:    "storesvc-snap",
			name:  "some-base",
			revno: snap.R(11),
		},
	})
	c.Check(t.Status(), Equals, state.DoneStatus)

	// check that the do-prereq task added all needed prereqs
	expectedLinkedSnaps := []string{"prereq1", "prereq2", "some-base"}
	linkedSnaps := make([]string, 0, len(expectedLinkedSnaps))
	for _, t := range chg.Tasks() {
		if t.Kind() == "link-snap" {
			snapsup, err := snapstate.TaskSnapSetup(t)
			c.Assert(err, IsNil)
			linkedSnaps = append(linkedSnaps, snapsup.Name())
		}
	}
	c.Check(linkedSnaps, DeepEquals, expectedLinkedSnaps)
}

func (s *prereqSuite) TestDoPrereqRetryWhenBaseInFlight(c *C) {
	restore := snapstate.MockPrerequisitesRetryTimeout(5 * time.Millisecond)
	defer restore()

	s.state.Lock()
	tCore := s.state.NewTask("link-snap", "Pretend core gets installed")
	tCore.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "core",
			Revision: snap.R(11),
		},
	})

	// pretend foo gets installed and needs core (which is in progress)
	t := s.state.NewTask("prerequisites", "foo")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
		},
	})

	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)
	chg.AddTask(tCore)

	s.state.Unlock()
	s.snapmgr.Ensure()
	s.snapmgr.Wait()
	s.state.Lock()

	// check that t is not done yet, it must wait for core
	c.Check(t.Status(), Equals, state.DoingStatus)
	c.Check(tCore.Status(), Equals, state.DoneStatus)

	s.state.Unlock()
	// wait the prereq-retry-timeout
	time.Sleep(10 * time.Millisecond)
	s.snapmgr.Ensure()
	s.snapmgr.Wait()
	s.state.Lock()
	defer s.state.Unlock()

	c.Check(t.Status(), Equals, state.DoneStatus)
}
