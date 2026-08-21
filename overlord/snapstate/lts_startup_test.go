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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/ltstrack"
	"github.com/snapcore/snapd/testutil"
)

func (s *snapmgrTestSuite) setupLTSJumpMocks(c *C) {
	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	s.AddCleanup(restore)
	restore = ltstrack.MockSnapdLTSTrackMap(map[int]map[string]string{
		18: {"latest": "18"},
	})
	s.AddCleanup(restore)
}

type unawareSnapdRefreshGraph struct {
	chg      *state.Change
	download *state.Task
	link     *state.Task
	auto     *state.Task
	other    *state.Task
}

func makeUnawareSnapdRefresh(st *state.State, explicitChannel bool, withOtherSnap bool) unawareSnapdRefreshGraph {
	si1 := &snap.SideInfo{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)}
	si2 := &snap.SideInfo{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(2)}
	snapstate.Set(st, "snapd", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1, si2}),
		Current:         snap.R(2),
		SnapType:        "snapd",
		TrackingChannel: "latest/stable",
	})

	chg := st.NewChange("refresh", "refresh snapd")
	download := st.NewTask("download-snap", "Download snap \"snapd\" (11) from channel \"latest/stable\"")
	download.Set("snap-setup", &snapstate.SnapSetup{
		Channel:         "latest/stable",
		ExplicitChannel: explicitChannel,
		Type:            snap.TypeSnapd,
		Version:         "snapdVer",
		SideInfo:        &snap.SideInfo{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(11)},
	})
	mount := st.NewTask("mount-snap", "Mount snap \"snapd\" (11)")
	mount.Set("snap-setup-task", download.ID())
	mount.WaitFor(download)
	link := st.NewTask("link-snap", "Make snap \"snapd\" (11) available to the system")
	link.Set("snap-setup-task", download.ID())
	link.WaitFor(mount)
	auto := st.NewTask("auto-connect", "Automatically connect eligible plugs and slots of snap \"snapd\"")
	auto.Set("snap-setup-task", download.ID())
	auto.WaitFor(link)

	chg.AddAll(state.NewTaskSet(download, mount, link, auto))
	download.SetStatus(state.DoneStatus)
	mount.SetStatus(state.DoneStatus)
	link.SetStatus(state.DoneStatus)

	g := unawareSnapdRefreshGraph{
		chg:      chg,
		download: download,
		link:     link,
		auto:     auto,
	}
	if withOtherSnap {
		other := st.NewTask("download-snap", "Download snap \"foo\" (2)")
		other.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{RealName: "foo", SnapID: "foo-id", Revision: snap.R(2)},
		})
		other.WaitFor(auto)
		chg.AddTask(other)
		g.other = other
	}
	return g
}

func firstDoStatusDownload(chg *state.Change) *state.Task {
	for _, t := range chg.Tasks() {
		if t.Kind() == "download-snap" && t.Status() == state.DoStatus {
			return t
		}
	}
	return nil
}

func (s *snapmgrTestSuite) TestLTSStartUpInjectsHopAndRetargetsSuffix(c *C) {
	s.setupLTSJumpMocks(c)

	s.state.Lock()
	defer s.state.Unlock()

	g := makeUnawareSnapdRefresh(s.state, false, false)
	origSup, err := snapstate.TaskSnapSetup(g.download)
	c.Assert(err, IsNil)

	err = s.snapmgr.MaybeInjectSnapdLTSHop()
	c.Assert(err, IsNil)

	var injected bool
	c.Assert(g.chg.Get(snapstate.SnapdLTSInjectedChangeAttr, &injected), IsNil)
	c.Check(injected, Equals, true)

	afterSup, err := snapstate.TaskSnapSetup(g.download)
	c.Assert(err, IsNil)
	c.Check(afterSup.Channel, Equals, origSup.Channel)
	c.Check(afterSup.Revision(), Equals, origSup.Revision())
	c.Check(afterSup.ExplicitChannel, Equals, false)

	injectedDownload := firstDoStatusDownload(g.chg)
	c.Assert(injectedDownload, NotNil)
	hopSup, err := snapstate.TaskSnapSetup(injectedDownload)
	c.Assert(err, IsNil)
	c.Check(hopSup.Channel, Equals, "18/stable")
	c.Check(hopSup.Type, Equals, snap.TypeSnapd)

	var setupTaskID string
	c.Assert(g.auto.Get("snap-setup-task", &setupTaskID), IsNil)
	c.Check(setupTaskID, Equals, injectedDownload.ID())

	waitIDs := make([]string, 0, len(g.auto.WaitTasks()))
	for _, t := range g.auto.WaitTasks() {
		waitIDs = append(waitIDs, t.ID())
	}
	c.Check(waitIDs, testutil.Contains, injectedDownload.ID())
	c.Check(waitIDs, testutil.Contains, g.link.ID())
}

func (s *snapmgrTestSuite) TestLTSStartUpInjectIdempotent(c *C) {
	s.setupLTSJumpMocks(c)

	s.state.Lock()
	defer s.state.Unlock()

	g := makeUnawareSnapdRefresh(s.state, false, false)
	c.Assert(s.snapmgr.MaybeInjectSnapdLTSHop(), IsNil)
	nAfterFirst := len(g.chg.Tasks())

	c.Assert(s.snapmgr.MaybeInjectSnapdLTSHop(), IsNil)
	c.Check(g.chg.Tasks(), HasLen, nAfterFirst)
}

func (s *snapmgrTestSuite) TestLTSStartUpSkipsExplicitChannel(c *C) {
	s.setupLTSJumpMocks(c)

	s.state.Lock()
	defer s.state.Unlock()

	g := makeUnawareSnapdRefresh(s.state, true, false)
	c.Assert(s.snapmgr.MaybeInjectSnapdLTSHop(), IsNil)

	var injected bool
	err := g.chg.Get(snapstate.SnapdLTSInjectedChangeAttr, &injected)
	c.Check(err, testutil.ErrorIs, state.ErrNoState)
	c.Check(firstDoStatusDownload(g.chg), IsNil)

	var setupTaskID string
	c.Assert(g.auto.Get("snap-setup-task", &setupTaskID), IsNil)
	c.Check(setupTaskID, Equals, g.download.ID())
}

func (s *snapmgrTestSuite) TestLTSStartUpMixedRefreshOtherSnapStillWaitsOnSuffix(c *C) {
	s.setupLTSJumpMocks(c)

	s.state.Lock()
	defer s.state.Unlock()

	g := makeUnawareSnapdRefresh(s.state, false, true)
	c.Assert(s.snapmgr.MaybeInjectSnapdLTSHop(), IsNil)

	c.Assert(g.other, NotNil)
	c.Check(g.other.WaitTasks(), DeepEquals, []*state.Task{g.auto})
}

func (s *snapmgrTestSuite) TestLTSStartUpRetainDiscardsUnawareRevsNotLatest(c *C) {
	s.setupLTSJumpMocks(c)

	s.state.Lock()
	defer s.state.Unlock()

	g := makeUnawareSnapdRefresh(s.state, false, false)
	c.Assert(s.snapmgr.MaybeInjectSnapdLTSHop(), IsNil)

	injectedDownload := firstDoStatusDownload(g.chg)
	c.Assert(injectedDownload, NotNil)

	var discarded []snap.Revision
	for _, t := range g.chg.Tasks() {
		if t.Kind() != "discard-snap" || t.Status() != state.DoStatus {
			continue
		}
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)
		if snapsup.InstanceName() != "snapd" {
			continue
		}
		discarded = append(discarded, snapsup.Revision())
	}
	c.Assert(discarded, HasLen, 1)
	c.Check(discarded[0], Equals, snap.R(1))
}

func (s *snapmgrTestSuite) TestLTSStartUpRestrictsOtherChangesUntilRestart(c *C) {
	s.setupLTSJumpMocks(c)

	s.state.Lock()
	makeUnawareSnapdRefresh(s.state, false, false)
	c.Assert(s.snapmgr.MaybeInjectSnapdLTSHop(), IsNil)

	otherChg := s.state.NewChange("other", "other change")
	blocked := s.state.NewTask("other-kind", "should not run on the vehicle")
	otherChg.AddTask(blocked)
	s.state.Unlock()

	runner := s.o.TaskRunner()
	for i := 0; i < 3; i++ {
		c.Assert(runner.Ensure(), IsNil)
		runner.Wait()
	}

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(blocked.Status(), Equals, state.DoStatus)
}

func (s *snapmgrTestSuite) TestLTSStartUpQuietDeviceDoesNotRestrict(c *C) {
	s.setupLTSJumpMocks(c)

	s.state.Lock()
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)}}),
		Current:         snap.R(1),
		SnapType:        "snapd",
		TrackingChannel: "latest/stable",
	})
	c.Assert(s.snapmgr.MaybeInjectSnapdLTSHop(), IsNil)

	otherChg := s.state.NewChange("other", "other change")
	nop := s.state.NewTask("nop", "runs on a quiet device")
	otherChg.AddTask(nop)
	s.state.Unlock()

	runner := s.o.TaskRunner()
	for i := 0; i < 5; i++ {
		c.Assert(runner.Ensure(), IsNil)
		runner.Wait()
	}

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(nop.Status(), Equals, state.DoneStatus)
}

func (s *snapmgrTestSuite) TestShouldSkipStatePatchesTransitionOnCore(c *C) {
	restore := release.MockOnClassic(false)
	s.AddCleanup(restore)
	restore = ltstrack.MockSnapdLTSTrackMap(map[int]map[string]string{
		18: {"latest": "18"},
	})
	s.AddCleanup(restore)
	restore = snapstatetest.ReplaceDeviceCtxHook(nil)
	s.AddCleanup(restore)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)}}),
		Current:         snap.R(1),
		SnapType:        "snapd",
		TrackingChannel: "latest/stable",
	})
	c.Check(snapstate.ShouldSkipStatePatches(s.state), Equals, true)
}

func (s *snapmgrTestSuite) TestShouldSkipStatePatchesAlreadyOnLTS(c *C) {
	s.setupLTSJumpMocks(c)
	restore := release.MockOnClassic(false)
	s.AddCleanup(restore)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)}}),
		Current:         snap.R(1),
		SnapType:        "snapd",
		TrackingChannel: "18/stable",
	})
	c.Check(snapstate.ShouldSkipStatePatches(s.state), Equals, false)
}

func (s *snapmgrTestSuite) TestShouldSkipStatePatchesExplicitChannel(c *C) {
	s.setupLTSJumpMocks(c)
	restore := release.MockOnClassic(false)
	s.AddCleanup(restore)

	s.state.Lock()
	defer s.state.Unlock()

	makeUnawareSnapdRefresh(s.state, true, false)
	c.Check(snapstate.ShouldSkipStatePatches(s.state), Equals, false)
}

func (s *snapmgrTestSuite) TestShouldSkipStatePatchesOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	s.AddCleanup(restore)
	restore = ltstrack.MockSnapdLTSTrackMap(map[int]map[string]string{
		18: {"latest": "18"},
	})
	s.AddCleanup(restore)
	restore = snapstatetest.ReplaceDeviceCtxHook(nil)
	s.AddCleanup(restore)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)}}),
		Current:         snap.R(1),
		SnapType:        "snapd",
		TrackingChannel: "latest/stable",
	})
	c.Check(snapstate.ShouldSkipStatePatches(s.state), Equals, false)
}

func (s *snapmgrTestSuite) TestLTSStartUpFailedHopDoesNotUndoVehicleLink(c *C) {
	s.setupLTSJumpMocks(c)

	s.state.Lock()
	defer s.state.Unlock()

	g := makeUnawareSnapdRefresh(s.state, false, false)
	c.Assert(s.snapmgr.MaybeInjectSnapdLTSHop(), IsNil)

	injectedDownload := firstDoStatusDownload(g.chg)
	c.Assert(injectedDownload, NotNil)
	c.Assert(injectedDownload.Lanes(), Not(DeepEquals), g.link.Lanes())

	var preserve bool
	c.Assert(g.link.Get(snapstate.SnapdLTSVehicleLinkAttr, &preserve), IsNil)
	c.Check(preserve, Equals, true)

	g.chg.AbortLanes(injectedDownload.Lanes())
	c.Check(g.link.Status(), Equals, state.DoneStatus)
	c.Check(g.auto.Status(), Equals, state.HoldStatus)

	afterSup, err := snapstate.TaskSnapSetup(g.download)
	c.Assert(err, IsNil)
	c.Check(afterSup.Channel, Equals, "latest/stable")
	c.Check(afterSup.Revision(), Equals, snap.R(11))

	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, "snapd", &snapst), IsNil)
	c.Check(snapst.Current, Equals, snap.R(2))
}

func (s *snapmgrTestSuite) TestLTSStartUpFailedHopDoesNotUndoVehicleLinkSharedLane(c *C) {
	s.setupLTSJumpMocks(c)

	s.state.Lock()
	defer s.state.Unlock()

	g := makeUnawareSnapdRefresh(s.state, false, true)
	lane := s.state.NewLane()
	for _, t := range g.chg.Tasks() {
		t.JoinLane(lane)
	}
	c.Assert(s.snapmgr.MaybeInjectSnapdLTSHop(), IsNil)

	injectedDownload := firstDoStatusDownload(g.chg)
	c.Assert(injectedDownload, NotNil)
	g.chg.AbortLanes(injectedDownload.Lanes())
	c.Check(g.auto.Status(), Equals, state.HoldStatus)
	c.Check(g.other.Status(), Equals, state.HoldStatus)

	afterSup, err := snapstate.TaskSnapSetup(g.download)
	c.Assert(err, IsNil)
	c.Check(afterSup.Channel, Equals, "latest/stable")

	var preserve bool
	c.Assert(g.link.Get(snapstate.SnapdLTSVehicleLinkAttr, &preserve), IsNil)
	c.Check(preserve, Equals, true)

	s.state.Unlock()
	c.Assert(s.snapmgr.UndoLinkSnap(g.link), IsNil)
	s.state.Lock()

	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, "snapd", &snapst), IsNil)
	c.Check(snapst.Current, Equals, snap.R(2))
}

func (s *snapmgrTestSuite) TestLTSStartUpSuffixWaitsForSecondRestart(c *C) {
	s.setupLTSJumpMocks(c)

	s.state.Lock()
	defer s.state.Unlock()

	g := makeUnawareSnapdRefresh(s.state, false, false)
	c.Assert(s.snapmgr.MaybeInjectSnapdLTSHop(), IsNil)

	var finish bool
	c.Assert(g.auto.Get("finish-restart", &finish), IsNil)
	c.Check(finish, Equals, true)

	var ltsLink *state.Task
	for _, t := range g.chg.Tasks() {
		if t.Kind() == "link-snap" && t.Status() == state.DoStatus {
			ltsLink = t
			break
		}
	}
	c.Assert(ltsLink, NotNil)
	for _, h := range ltsLink.HaltTasks() {
		c.Assert(h.Get("finish-restart", &finish), IsNil)
		c.Check(finish, Equals, true)
	}

	snapsup, err := snapstate.TaskSnapSetup(g.auto)
	c.Assert(err, IsNil)
	restart.MockPending(s.state, restart.RestartDaemon)
	err = snapstate.FinishRestart(g.auto, snapsup, snapstate.FinishRestartOptions{})
	c.Check(err, FitsTypeOf, &state.Retry{})
}

func (s *snapmgrTestSuite) TestEnsureSnapdLTSTrackTransitionQuietDevice(c *C) {
	s.setupLTSJumpMocks(c)

	s.state.Lock()
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)}}),
		Current:         snap.R(1),
		SnapType:        "snapd",
		TrackingChannel: "latest/stable",
	})
	n := len(s.state.Changes())
	s.state.Unlock()

	c.Assert(s.snapmgr.EnsureSnapdLTSTrackTransition(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(s.state.Changes(), HasLen, n+1)
	var chg *state.Change
	for _, cchg := range s.state.Changes() {
		if cchg.Kind() == "snapd-lts-track-transition" {
			chg = cchg
			break
		}
	}
	c.Assert(chg, NotNil)
	c.Check(chg.Summary(), testutil.Contains, "18/stable")
	found := false
	for _, t := range chg.Tasks() {
		if t.Kind() != "download-snap" {
			continue
		}
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)
		if snapsup.InstanceName() != "snapd" {
			continue
		}
		c.Check(snapsup.Channel, Equals, "18/stable")
		c.Check(snapsup.ExplicitChannel, Equals, false)
		found = true
	}
	c.Check(found, Equals, true)
}

func (s *snapmgrTestSuite) TestEnsureSnapdLTSTrackTransitionSkipsInFlight(c *C) {
	s.setupLTSJumpMocks(c)

	s.state.Lock()
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)}}),
		Current:         snap.R(1),
		SnapType:        "snapd",
		TrackingChannel: "latest/stable",
	})
	other := s.state.NewChange("other", "in flight")
	other.AddTask(s.state.NewTask("nop", "nop"))
	n := len(s.state.Changes())
	s.state.Unlock()

	c.Assert(s.snapmgr.EnsureSnapdLTSTrackTransition(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(s.state.Changes(), HasLen, n)
}

func (s *snapmgrTestSuite) TestEnsureSnapdLTSTrackTransitionSkipsVehicle(c *C) {
	s.setupLTSJumpMocks(c)

	s.state.Lock()
	makeUnawareSnapdRefresh(s.state, false, false)
	n := len(s.state.Changes())
	s.state.Unlock()

	c.Assert(s.snapmgr.EnsureSnapdLTSTrackTransition(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(s.state.Changes(), HasLen, n)
}

func (s *snapmgrTestSuite) TestEnsureSnapdLTSTrackTransitionAlreadyOnLTS(c *C) {
	s.setupLTSJumpMocks(c)

	s.state.Lock()
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)}}),
		Current:         snap.R(1),
		SnapType:        "snapd",
		TrackingChannel: "18/stable",
	})
	n := len(s.state.Changes())
	s.state.Unlock()

	c.Assert(s.snapmgr.EnsureSnapdLTSTrackTransition(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(s.state.Changes(), HasLen, n)
}
