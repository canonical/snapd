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
	"fmt"
	"os"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

type prereqSuite struct {
	baseHandlerSuite

	fakeStore *fakeStore
}

var _ = Suite(&prereqSuite{})

func (s *prereqSuite) SetUpTest(c *C) {
	s.setup(c, nil)

	s.fakeStore = &fakeStore{
		state:       s.state,
		fakeBackend: s.fakeBackend,
	}
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.ReplaceStore(s.state, s.fakeStore)

	s.state.Set("refresh-privacy-key", "privacy-key")
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

	s.se.Ensure()
	s.se.Wait()

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

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op: "storesvc-snap-action",
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "install",
				InstanceName: "prereq1",
				Channel:      "stable",
			},
			revno: snap.R(11),
		},
		{
			op: "storesvc-snap-action",
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "install",
				InstanceName: "prereq2",
				Channel:      "stable",
			},
			revno: snap.R(11),
		},
		{
			op: "storesvc-snap-action",
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "install",
				InstanceName: "some-base",
				Channel:      "stable",
			},
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
			linkedSnaps = append(linkedSnaps, snapsup.InstanceName())
		}
	}
	c.Check(linkedSnaps, DeepEquals, expectedLinkedSnaps)
}

func (s *prereqSuite) TestDoPrereqRetryWhenBaseInFlight(c *C) {
	restore := snapstate.MockPrerequisitesRetryTimeout(5 * time.Millisecond)
	defer restore()

	calls := 0
	s.runner.AddHandler("link-snap",
		func(task *state.Task, _ *tomb.Tomb) error {
			if calls == 0 {
				// retry again later, this forces ordering of
				// tasks, so that the prerequisites tasks ends
				// up waiting for this one
				calls += 1
				return &state.Retry{After: 1 * time.Millisecond}
			}

			// setup everything as if the snap is installed
			st := task.State()
			st.Lock()
			defer st.Unlock()
			snapsup, _ := snapstate.TaskSnapSetup(task)
			var snapst snapstate.SnapState
			snapstate.Get(st, snapsup.InstanceName(), &snapst)
			snapst.Current = snapsup.Revision()
			snapst.Sequence = append(snapst.Sequence, snapsup.SideInfo)
			snapstate.Set(st, snapsup.InstanceName(), &snapst)
			return nil
		},
		func(*state.Task, *tomb.Tomb) error {
			return nil
		})
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

	// NOTE: tasks are iterated on in undefined order, we have fixed the
	// link-snap handler to return a 'fake' retry what results
	// 'prerequisites' task handler observing the state of the world we
	// want, even if 'link-snap' ran first

	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Millisecond)
		s.state.Unlock()
		s.se.Ensure()
		s.se.Wait()
		s.state.Lock()
		if tCore.Status() == state.DoneStatus {
			break
		}
	}

	// check that t is not done yet, it must wait for core
	c.Check(t.Status(), Equals, state.DoingStatus)
	c.Check(tCore.Status(), Equals, state.DoneStatus)

	// wait, we will hit prereq-retry-timeout eventually
	// (this can take a while on very slow machines)
	for i := 0; i < 50; i++ {
		time.Sleep(10 * time.Millisecond)
		s.state.Unlock()
		s.se.Ensure()
		s.se.Wait()
		s.state.Lock()
		if t.Status() == state.DoneStatus {
			break
		}
	}
	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (s *prereqSuite) TestDoPrereqChannelEnvvars(c *C) {
	os.Setenv("SNAPD_BASES_CHANNEL", "edge")
	defer os.Unsetenv("SNAPD_BASES_CHANNEL")
	os.Setenv("SNAPD_PREREQS_CHANNEL", "candidate")
	defer os.Unsetenv("SNAPD_PREREQS_CHANNEL")
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

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op: "storesvc-snap-action",
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "install",
				InstanceName: "prereq1",
				Channel:      "candidate",
			},
			revno: snap.R(11),
		},
		{
			op: "storesvc-snap-action",
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "install",
				InstanceName: "prereq2",
				Channel:      "candidate",
			},
			revno: snap.R(11),
		},
		{
			op: "storesvc-snap-action",
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "install",
				InstanceName: "some-base",
				Channel:      "edge",
			},
			revno: snap.R(11),
		},
	})
	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (s *prereqSuite) TestDoPrereqNothingToDoForBase(c *C) {
	for _, typ := range []snap.Type{
		snap.TypeOS,
		snap.TypeGadget,
		snap.TypeKernel,
		snap.TypeBase,
	} {

		s.state.Lock()
		t := s.state.NewTask("prerequisites", "test")
		t.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: fmt.Sprintf("foo-%s", typ),
				Revision: snap.R(1),
			},
			Type: typ,
		})
		s.state.NewChange("dummy", "...").AddTask(t)
		s.state.Unlock()

		s.se.Ensure()
		s.se.Wait()

		s.state.Lock()
		c.Assert(s.fakeBackend.ops, HasLen, 0)
		c.Check(t.Status(), Equals, state.DoneStatus)
		s.state.Unlock()
	}
}

func (s *prereqSuite) TestDoPrereqNothingToDoForSnapdSnap(c *C) {
	s.state.Lock()
	t := s.state.NewTask("prerequisites", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "snapd",
			Revision: snap.R(1),
		},
	})
	s.state.NewChange("dummy", "...").AddTask(t)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	c.Assert(s.fakeBackend.ops, HasLen, 0)
	c.Check(t.Status(), Equals, state.DoneStatus)
	s.state.Unlock()
}
