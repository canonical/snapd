// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"context"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/ltschannel"
	"github.com/snapcore/snapd/snap/snaptest"
)

func (s *snapmgrTestSuite) TestSnapdRefreshTaskSetHasCheckLTSChannelAtEnd(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	si := snap.SideInfo{
		RealName: "snapd",
		SnapID:   "snapd-snap-id",
		Revision: snap.R(1),
		Channel:  "latest/stable",
	}
	snaptest.MockSnap(c, "name: snapd\ntype: snapd", &si)
	s.fakeStore.refreshRevnos["snapd"] = snap.R(2)

	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:         si.Revision,
		SnapType:        "snapd",
		TrackingChannel: "latest/stable",
	})

	_, tss, err := snapstate.UpdateMany(context.Background(), s.state, []string{"snapd"}, nil, 0, &snapstate.Flags{IgnoreRunning: true})
	c.Assert(err, IsNil)

	var snapdTS *state.TaskSet
	for _, ts := range tss {
		for _, t := range ts.Tasks() {
			snapsup, err := snapstate.TaskSnapSetup(t)
			if err != nil {
				continue
			}
			if snapsup.Type == snap.TypeSnapd {
				snapdTS = ts
				break
			}
		}
	}
	c.Assert(snapdTS, NotNil)

	kinds := taskKinds(snapdTS.Tasks())
	c.Assert(kinds[len(kinds)-1], Equals, "check-lts-channel")

	healthIdx := -1
	checkIdx := -1
	for i, k := range kinds {
		switch k {
		case "run-hook[check-health]":
			healthIdx = i
		case "check-lts-channel":
			checkIdx = i
		}
	}
	c.Assert(healthIdx, Not(Equals), -1)
	c.Assert(checkIdx, Not(Equals), -1)
	c.Assert(checkIdx, Equals, healthIdx+1)
}

type ltsChannelHandlerSuite struct {
	baseHandlerSuite
}

var _ = Suite(&ltsChannelHandlerSuite{})

func (s *ltsChannelHandlerSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)

	s.state.Lock()
	defer s.state.Unlock()

	_, err := restart.Manager(s.state, "boot-id-1", nil)
	c.Assert(err, IsNil)
}

func (s *ltsChannelHandlerSuite) TestDoCheckLTSChannelInjectionBlocksUntilDone(c *C) {
	restoreModel := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restoreModel()

	restoreTracks := ltschannel.MockSnapdLTSTrackMap(map[int][]string{18: {"18"}})
	defer restoreTracks()

	var switchTask, refreshTask *state.Task
	restoreUpdate := snapstate.MockLtsChannelUpdateMany(func(_ context.Context, st *state.State, names []string, revOpts []*snapstate.RevisionOptions, _ int, _ snapstate.UpdateFilter, _ *snapstate.Flags, _ string) ([]string, *snapstate.UpdateTaskSets, error) {
		c.Assert(names, DeepEquals, []string{"snapd"})
		c.Assert(revOpts, HasLen, 1)
		c.Assert(revOpts[0].Channel, Equals, "18/stable")

		st.Lock()
		defer st.Unlock()

		switchTask = st.NewTask("switch-snap", "switch snapd to LTS channel")
		refreshTask = st.NewTask("download-snap", "refresh snapd on LTS channel")
		refreshTask.WaitFor(switchTask)
		ts := state.NewTaskSet(switchTask, refreshTask)
		return []string{"snapd"}, &snapstate.UpdateTaskSets{Refresh: []*state.TaskSet{ts}}, nil
	})
	defer restoreUpdate()

	s.state.Lock()

	si := snap.SideInfo{
		RealName: "snapd",
		SnapID:   "snapd-snap-id",
		Revision: snap.R(1),
		Channel:  "latest/stable",
	}
	snaptest.MockSnap(c, "name: snapd\ntype: snapd", &si)
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:         si.Revision,
		SnapType:        "snapd",
		TrackingChannel: "latest/stable",
	})

	chg := s.state.NewChange("refresh", "refresh snapd")
	prepare := s.state.NewTask("prepare-snap", "prepare snapd")
	snapsup := snapstate.SnapSetup{
		SideInfo: &si,
		Channel:  si.Channel,
		Type:     snap.TypeSnapd,
	}
	prepare.Set("snap-setup", &snapsup)

	check := s.state.NewTask("check-lts-channel", "check LTS channel")
	check.Set("snap-setup-task", prepare.ID())
	chg.AddTask(prepare)
	chg.AddTask(check)

	restart.MockPending(s.state, restart.RestartUnset)

	check.SetStatus(state.DoingStatus)
	s.state.Unlock()

	err := s.snapmgr.DoCheckLTSChannel(check, nil)
	c.Assert(err, FitsTypeOf, &state.Retry{})

	s.state.Lock()
	c.Assert(switchTask, NotNil)
	c.Assert(refreshTask, NotNil)
	c.Assert(refreshTask.WaitTasks(), HasLen, 1)
	c.Assert(refreshTask.WaitTasks()[0].ID(), Equals, switchTask.ID())
	c.Assert(switchTask.WaitTasks(), HasLen, 0)
	s.state.Unlock()

	err = s.snapmgr.DoCheckLTSChannel(check, nil)
	c.Assert(err, FitsTypeOf, &state.Retry{})

	s.state.Lock()
	switchTask.SetStatus(state.DoneStatus)
	refreshTask.SetStatus(state.DoneStatus)
	s.state.Unlock()

	err = s.snapmgr.DoCheckLTSChannel(check, nil)
	c.Assert(err, IsNil)

	s.state.Lock()
}
