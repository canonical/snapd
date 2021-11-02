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

	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

type prereqSuite struct {
	baseHandlerSuite

	fakeStore *fakeStore
}

var _ = Suite(&prereqSuite{})

func (s *prereqSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)

	s.fakeStore = &fakeStore{
		state:       s.state,
		fakeBackend: s.fakeBackend,
	}
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.ReplaceStore(s.state, s.fakeStore)

	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.state, repo)

	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "privacy-key")
	s.AddCleanup(snapstatetest.MockDeviceModel(DefaultModel()))

	restoreCheckFreeSpace := snapstate.MockOsutilCheckFreeSpace(func(string, uint64) error { return nil })
	s.AddCleanup(restoreCheckFreeSpace)

	restoreInstallSize := snapstate.MockInstallSize(func(st *state.State, snaps []snapstate.MinimalInstallInfo, userID int) (uint64, error) {
		return 0, nil
	})
	s.AddCleanup(restoreInstallSize)

	restore := snapstate.MockEnforcedValidationSets(func(st *state.State) (*snapasserts.ValidationSets, error) {
		return nil, nil
	})
	s.AddCleanup(restore)
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

func (s *prereqSuite) TestDoPrereqWithBaseNone(c *C) {
	s.state.Lock()

	t := s.state.NewTask("prerequisites", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Base:               "none",
		PrereqContentAttrs: map[string][]string{"prereq1": {"some-content"}},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(t.Status(), Equals, state.DoneStatus)

	// check that the do-prereq task added all needed prereqs
	expectedLinkedSnaps := []string{"prereq1", "snapd"}
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

func (s *prereqSuite) TestDoPrereqTalksToStoreAndQueues(c *C) {
	s.state.Lock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "os",
	})

	t := s.state.NewTask("prerequisites", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Channel:            "beta",
		Base:               "some-base",
		PrereqContentAttrs: map[string][]string{"prereq1": {"some-content"}, "prereq2": {"other-content"}},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(s.fakeBackend.ops, testutil.DeepUnsortedMatches, fakeOps{
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
	c.Check(linkedSnaps, testutil.DeepUnsortedMatches, expectedLinkedSnaps)
}

func (s *prereqSuite) TestDoPrereqRetryWhenBaseInFlight(c *C) {
	restore := snapstate.MockPrerequisitesRetryTimeout(1 * time.Millisecond)
	defer restore()

	var prereqTask *state.Task

	calls := 0
	s.runner.AddHandler("link-snap",
		func(task *state.Task, _ *tomb.Tomb) error {
			st := task.State()
			st.Lock()
			defer st.Unlock()

			calls += 1
			if calls == 1 {
				// retry again later, this forces taskrunner
				// to pick prequisites task.
				return &state.Retry{After: 1 * time.Millisecond}
			}

			// setup everything as if the snap is installed

			snapsup, _ := snapstate.TaskSnapSetup(task)
			var snapst snapstate.SnapState
			snapstate.Get(st, snapsup.InstanceName(), &snapst)
			snapst.Current = snapsup.Revision()
			snapst.Sequence = append(snapst.Sequence, snapsup.SideInfo)
			snapstate.Set(st, snapsup.InstanceName(), &snapst)

			// check that prerequisites task is not done yet, it must wait for core.
			// This check guarantees that prerequisites task found link-snap snap
			// task in flight, and returned a retry error, resulting in DoingStatus.
			c.Check(prereqTask.Status(), Equals, state.DoingStatus)

			return nil
		}, nil)
	s.state.Lock()
	tCore := s.state.NewTask("link-snap", "Pretend core gets installed")
	tCore.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "core",
			Revision: snap.R(11),
		},
	})

	// pretend foo gets installed and needs core (which is in progress)
	prereqTask = s.state.NewTask("prerequisites", "foo")
	prereqTask.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
		},
	})

	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(prereqTask)
	chg.AddTask(tCore)

	// NOTE: tasks are iterated on in undefined order, we have fixed the
	// link-snap handler to return a 'fake' retry what results
	// 'prerequisites' task handler observing the state of the world we
	// want, even if 'link-snap' ran first

	// wait, we will hit prereq-retry-timeout eventually
	// (this can take a while on very slow machines)
	for i := 0; i < 500; i++ {
		time.Sleep(1 * time.Millisecond)
		s.state.Unlock()
		s.se.Ensure()
		s.se.Wait()
		s.state.Lock()
		if prereqTask.Status() == state.DoneStatus {
			break
		}
	}

	// sanity check, exactly two calls to link-snap due to retry error on 1st call
	c.Check(calls, Equals, 2)

	c.Check(tCore.Status(), Equals, state.DoneStatus)
	c.Check(prereqTask.Status(), Equals, state.DoneStatus)

	// sanity
	c.Check(chg.Status(), Equals, state.DoneStatus)
}

func (s *prereqSuite) TestDoPrereqChannelEnvvars(c *C) {
	os.Setenv("SNAPD_BASES_CHANNEL", "edge")
	defer os.Unsetenv("SNAPD_BASES_CHANNEL")
	os.Setenv("SNAPD_PREREQS_CHANNEL", "candidate")
	defer os.Unsetenv("SNAPD_PREREQS_CHANNEL")
	s.state.Lock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "os",
	})

	t := s.state.NewTask("prerequisites", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Channel:            "beta",
		Base:               "some-base",
		PrereqContentAttrs: map[string][]string{"prereq1": {"some-content"}, "prereq2": {"other-content"}},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(s.fakeBackend.ops, testutil.DeepUnsortedMatches, fakeOps{
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
		// type is normally set from snap info at install time
		Type: snap.TypeSnapd,
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

func (s *prereqSuite) TestDoPrereqCore16wCoreNothingToDo(c *C) {
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
		Base: "core16",
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

func (s *prereqSuite) testDoPrereqNoCorePullsInSnaps(c *C, base string) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()

	t := s.state.NewTask("prerequisites", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Base: base,
	})
	s.state.NewChange("dummy", "...").AddTask(t)
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
				InstanceName: base,
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
				InstanceName: "snapd",
				Channel:      "stable",
			},
			revno: snap.R(11),
		},
	})

	c.Check(t.Change().Err(), IsNil)
	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (s *prereqSuite) TestDoPrereqCore16noCore(c *C) {
	s.testDoPrereqNoCorePullsInSnaps(c, "core16")
}

func (s *prereqSuite) TestDoPrereqCore18NoCorePullsInSnapd(c *C) {
	s.testDoPrereqNoCorePullsInSnaps(c, "core18")
}

func (s *prereqSuite) TestDoPrereqOtherBaseNoCorePullsInSnapd(c *C) {
	s.testDoPrereqNoCorePullsInSnaps(c, "some-base")
}

func (s *prereqSuite) TestDoPrereqBaseIsNotBase(c *C) {
	s.state.Lock()

	t := s.state.NewTask("prerequisites", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Channel:            "beta",
		Base:               "app-snap",
		PrereqContentAttrs: map[string][]string{"prereq1": {"some-content"}},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*- test \(cannot install snap base "app-snap": unexpected snap type "app", instead of 'base'\)`)
}

func (s *prereqSuite) TestDoPrereqBaseNoRevision(c *C) {
	os.Setenv("SNAPD_BASES_CHANNEL", "channel-no-revision")
	defer os.Unsetenv("SNAPD_BASES_CHANNEL")

	s.state.Lock()

	t := s.state.NewTask("prerequisites", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Channel:            "beta",
		Base:               "some-base",
		PrereqContentAttrs: map[string][]string{"prereq1": {"some-content"}},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*- test \(cannot install snap base "some-base": no snap revision available as specified\)`)
}

func (s *prereqSuite) TestDoPrereqNoRevision(c *C) {
	os.Setenv("SNAPD_PREREQS_CHANNEL", "channel-no-revision")
	defer os.Unsetenv("SNAPD_PREREQS_CHANNEL")

	s.state.Lock()

	t := s.state.NewTask("prerequisites", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Channel:            "beta",
		PrereqContentAttrs: map[string][]string{"prereq1": {"some-content"}},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*- test \(cannot install prerequisite "prereq1": no snap revision available as specified\)`)
}

func (s *prereqSuite) TestDoPrereqSnapdNoRevision(c *C) {
	os.Setenv("SNAPD_SNAPD_CHANNEL", "channel-no-revision")
	defer os.Unsetenv("SNAPD_SNAPD_CHANNEL")

	s.state.Lock()

	t := s.state.NewTask("prerequisites", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Base:    "core18",
		Channel: "beta",
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*- test \(cannot install system snap "snapd": no snap revision available as specified\)`)
}
