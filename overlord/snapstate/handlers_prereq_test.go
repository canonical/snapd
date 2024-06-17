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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
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

	restoreInstallSize := snapstate.MockInstallSize(func(st *state.State, snaps []snapstate.MinimalInstallInfo, userID int, prqt snapstate.PrereqTracker) (uint64, error) {
		return 0, nil
	})
	s.AddCleanup(restoreInstallSize)

	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		return nil, nil
	})
	s.AddCleanup(restore)

	s.AddCleanup(osutil.MockMountInfo(``))

	s.AddCleanup(snapstate.MockEnsuredMountsUpdated(s.snapmgr, true))
}

func (s *prereqSuite) TestDoPrereqNothingToDo(c *C) {
	s.state.Lock()

	si1 := &snap.SideInfo{
		RealName: "core",
		Revision: snap.R(1),
	}
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
	})

	t := s.state.NewTask("prerequisites", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
	})
	s.state.NewChange("sample", "...").AddTask(t)
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
	chg := s.state.NewChange("sample", "...")
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
	lane := 0
	for _, t := range chg.Tasks() {
		if t.Kind() == "link-snap" {
			snapsup, err := snapstate.TaskSnapSetup(t)
			c.Assert(err, IsNil)
			linkedSnaps = append(linkedSnaps, snapsup.InstanceName())
		} else if t.Kind() == "prerequisites" {
			c.Assert(t.Lanes(), DeepEquals, []int{lane})
			lane++
		}
	}
	c.Assert(lane, Equals, 3)
	c.Check(linkedSnaps, DeepEquals, expectedLinkedSnaps)
}

func (s *prereqSuite) TestDoPrereqManyTransactional(c *C) {
	s.state.Lock()

	t := s.state.NewTask("prerequisites", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Base: "none",
		PrereqContentAttrs: map[string][]string{
			"prereq1": {"some-content"}, "prereq2": {"other-content"}},
		Flags: snapstate.Flags{Transaction: client.TransactionAllSnaps},
	})
	// Set lane to make sure new tasks will match this one
	lane := s.state.NewLane()
	t.JoinLane(lane)
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(t.Status(), Equals, state.DoneStatus)

	// check that the do-prereq task added all needed prereqs
	expectedLinkedSnaps := []string{"prereq1", "prereq2", "snapd"}
	linkedSnaps := make([]string, 0, len(expectedLinkedSnaps))
	for _, t := range chg.Tasks() {
		// Make sure that the Transactional flag has been applied,
		// so we have only one lane.
		c.Assert(t.Lanes(), DeepEquals, []int{lane})

		if t.Kind() == "link-snap" {
			snapsup, err := snapstate.TaskSnapSetup(t)
			c.Assert(err, IsNil)
			linkedSnaps = append(linkedSnaps, snapsup.InstanceName())
		}
	}
	c.Check(linkedSnaps, testutil.DeepUnsortedMatches, expectedLinkedSnaps)
}

func (s *prereqSuite) TestDoPrereqTransactionalFailTooManyLanes(c *C) {
	s.state.Lock()

	t := s.state.NewTask("prerequisites", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Base: "none",
		PrereqContentAttrs: map[string][]string{
			"prereq1": {"some-content"}},
		Flags: snapstate.Flags{Transaction: client.TransactionAllSnaps},
	})
	// There should be only one lane in a transactional change
	t.JoinLane(s.state.NewLane())
	t.JoinLane(s.state.NewLane())
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(t.Status(), Equals, state.ErrorStatus)
}

func (s *prereqSuite) TestDoPrereqTalksToStoreAndQueues(c *C) {
	s.state.Lock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1)},
		}),
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
	chg := s.state.NewChange("sample", "...")
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
			snapst.Sequence.Revisions = append(snapst.Sequence.Revisions, sequence.NewRevisionSideState(snapsup.SideInfo, nil))
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

	chg := s.state.NewChange("sample", "...")
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

	// validity check, exactly two calls to link-snap due to retry error on 1st call
	c.Check(calls, Equals, 2)

	c.Check(tCore.Status(), Equals, state.DoneStatus)
	c.Check(prereqTask.Status(), Equals, state.DoneStatus)

	// validity
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1)},
		}),
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
	chg := s.state.NewChange("sample", "...")
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
		s.state.NewChange("sample", "...").AddTask(t)
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
	s.state.NewChange("sample", "...").AddTask(t)
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
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
	s.state.NewChange("sample", "...").AddTask(t)
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
	s.state.NewChange("sample", "...").AddTask(t)
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
	chg := s.state.NewChange("sample", "...")
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
	chg := s.state.NewChange("sample", "...")
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
	chg := s.state.NewChange("sample", "...")
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
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n.*- test \(cannot install system snap "snapd": no snap revision available as specified\)`)
}

func (s *prereqSuite) TestPreReqContentAttrsNotSatisfied(c *C) {
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	s.AddCleanup(func() { snapstate.AutoAliases = nil })

	st := s.state
	st.Lock()

	// mock the snap which is the default-provider
	mockInstalledSnap(c, st, `name: some-snap`, false)

	t := s.state.NewTask("prerequisites", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Channel:            "beta",
		Base:               "core",
		PrereqContentAttrs: map[string][]string{"some-snap": {"this-does-not-match"}},
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)
	st.Unlock()

	s.se.Ensure()
	s.se.Wait()

	st.Lock()
	defer st.Unlock()

	// As we are not seeding, expect no messages being logged for the task. Instead
	// the update should have added a lot of tasks, so the resulting change should
	// contain the update for some-snap.
	c.Check(chg.Err(), IsNil)
	c.Check(len(chg.Tasks()) > 1, Equals, true)

	// Expect the initial prerequisites task to have completed
	c.Check(chg.Tasks()[0].Kind(), Equals, "prerequisites")
	c.Check(chg.Tasks()[0].Status(), Equals, state.DoneStatus)
	c.Check(chg.Tasks()[0].Log(), HasLen, 0)
}

func (s *prereqSuite) TestPreReqContentAttrsNotSatisfiedSeeding(c *C) {
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	s.AddCleanup(func() { snapstate.AutoAliases = nil })

	// mock that we are not seeded
	r := snapstatetest.MockDeviceModelAndMode(DefaultModel(), "install")
	defer r()

	st := s.state
	st.Lock()
	st.Set("seeded", false)

	// mock the snap which is the default-provider
	mockInstalledSnap(c, st, `name: some-snap`, false)

	t := s.state.NewTask("prerequisites", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Channel:            "beta",
		Base:               "core",
		PrereqContentAttrs: map[string][]string{"some-snap": {"this-does-not-match"}},
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)
	st.Unlock()

	s.se.Ensure()
	s.se.Wait()

	st.Lock()
	defer st.Unlock()

	// We expect the change to complete without error, even if updating a snap is not
	// allowed during seeding. Instead what we expect, is a task log message noting that
	// updating the snap was not allowed due to the seeding stage.
	c.Check(chg.Err(), IsNil)
	c.Assert(chg.Tasks(), HasLen, 1)
	c.Assert(chg.Tasks()[0].Log(), HasLen, 1)
	c.Check(chg.Tasks()[0].Log()[0], testutil.Contains, `cannot update "some-snap" during seeding, will not have required content "this-does-not-match": too early for operation, device not yet seeded or device model not acknowledged`)
}

func (s *prereqSuite) TestDoPrereqSkipDuringRemodel(c *C) {
	s.state.Lock()

	restore := snapstatetest.MockDeviceContext(&snapstatetest.TrivialDeviceContext{
		Remodeling: true,
	})
	defer restore()

	// replace the store here so we can force an error if we actually call
	// InstallWithDeviceContext. if we do not do this, and we fail to properly
	// handle the remodel case, InstallWithDeviceContext will return a
	// ChangeConflictError, which is then ignored, making this test invalid
	snapstate.ReplaceStore(s.state, storetest.Store{})

	// install snapd so that prerequisites handler won't try to install it
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{
				RealName: "snapd",
				Revision: snap.R(1),
			},
		}),
		Current: snap.R(1),
	})

	prereqTask := s.state.NewTask("prerequisites", "test")
	prereqTask.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
		},
		Base:    "core18",
		Channel: "stable",
	})

	linkSnapTask := s.state.NewTask("link-snap", "Doing a fake link-snap")
	linkSnapTask.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "core18",
			Revision: snap.R(1),
		},
		Type:    snap.TypeBase,
		Channel: "stable",
	})

	// this simulates the link-snap task being blocked on the prerequisites
	// task, like during a remodel
	linkSnapTask.WaitFor(prereqTask)

	// for this test, we don't care about what link-snap does
	s.runner.AddHandler("link-snap", func(task *state.Task, _ *tomb.Tomb) error {
		return nil
	}, nil)

	chg := s.state.NewChange("do-prereqs", "...")
	chg.AddTask(prereqTask)
	chg.AddTask(linkSnapTask)

	s.state.Unlock()

	// ensure and wait twice since the link-snap task depends on the
	// prerequisites task
	s.se.Ensure()
	s.se.Wait()
	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(prereqTask.Status(), Equals, state.DoneStatus)
	c.Check(linkSnapTask.Status(), Equals, state.DoneStatus)
	c.Check(chg.Err(), IsNil)
	c.Check(chg.Status(), Equals, state.DoneStatus)

	// core18 should not be installed, despite the prerequisites task being
	// finished successfully. this is because, during a remodel, we do not wait
	// for (or attempt) prerequisites installs.
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "core18", &snapst)
	c.Check(err, testutil.ErrorIs, state.ErrNoState)
}
