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
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
)

// Planner tests. snap/uctrack already covers Resolve. A refresh of an installed
// snapd snap reads ubuntu-core-tracks from latest/stable, then plans that
// request once on the mapped channel. An install has no tracking channel, so
// it keeps the channel it was given. Download fetches the channel it is given.

var ucTracks18 = snap.UbuntuCoreTracks{
	"18": {"latest": "18", "fips-updates": "18-fips"},
}

func (s *snapmgrTestSuite) mockSnapdUbuntuCoreTracks(tracks snap.UbuntuCoreTracks) {
	s.fakeStore.mutateSnapInfo = func(info *snap.Info) error {
		if info.SnapName().String() != "snapd" {
			return nil
		}
		info.UbuntuCoreTracks = tracks
		return nil
	}
}

func (s *snapmgrTestSuite) setSnapdTracking(channel string) {
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: channel,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "snapd",
			SnapID:   "snapd-snap-id",
			Revision: snap.R(1),
		}}),
		Current:  snap.R(1),
		SnapType: "snapd",
	})
}

func assertLatestStablePrecursor(c *C, action store.SnapAction, label string) {
	c.Check(action.Action, Equals, "install", Commentf(label))
	c.Check(action.Channel, Equals, "latest/stable", Commentf(label))
	c.Check(action.Revision.Unset(), Equals, true, Commentf(label))
	c.Check(action.CohortKey, Equals, "", Commentf(label))
}

// assertLatestStableQueryOmitsInstalledSnaps checks that the latest/stable
// metadata query did not send the installed snaps. A refresh still does.
func (s *snapmgrTestSuite) assertLatestStableQueryOmitsInstalledSnaps(c *C) {
	var cur []store.CurrentSnap
	var saw bool
	for _, op := range s.fakeBackend.ops {
		switch op.op {
		case "storesvc-snap-action":
			cur = op.curSnaps
		case "storesvc-snap-action:action":
			if op.action.InstanceName == "snapd" && op.action.Action == "install" && op.action.Channel == "latest/stable" {
				c.Check(cur, HasLen, 0)
				saw = true
			}
		}
	}
	c.Assert(saw, Equals, true)
}

func (s *snapmgrTestSuite) snapActions(name string) []store.SnapAction {
	var actions []store.SnapAction
	for _, op := range s.fakeBackend.ops {
		if op.op == "storesvc-snap-action:action" && op.action.InstanceName == name {
			actions = append(actions, op.action)
		}
	}
	return actions
}

func snapSetupFromTasks(c *C, ts *state.TaskSet) snapstate.SnapSetup {
	c.Assert(ts, NotNil)
	t := ts.MaybeEdge(snapstate.SnapSetupEdge)
	c.Assert(t, NotNil)
	var sup snapstate.SnapSetup
	c.Assert(t.Get("snap-setup", &sup), IsNil)
	return sup
}

func (s *snapmgrTestSuite) TestInstallSnapdKeepsRequestedChannel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	snapstate.Set(s.state, "snapd", nil)

	ts, err := snapstate.Install(context.Background(), s.state, "snapd", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	actions := s.snapActions("snapd")
	c.Assert(actions, HasLen, 1)
	c.Check(actions[0].Action, Equals, "install")
	c.Check(actions[0].Channel, Equals, "stable")
	c.Check(actions[0].Revision.Unset(), Equals, true)
	c.Check(snapSetupFromTasks(c, ts).Channel, Equals, "stable")
}

func (s *snapmgrTestSuite) TestInstallSnapdKeepsRequestedBranch(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	snapstate.Set(s.state, "snapd", nil)

	_, err := snapstate.Install(context.Background(), s.state, "snapd", &snapstate.RevisionOptions{Channel: "latest/edge/hotfix"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	actions := s.snapActions("snapd")
	c.Assert(actions, HasLen, 1)
	c.Check(actions[0].Action, Equals, "install")
	c.Check(actions[0].Channel, Equals, "latest/edge/hotfix")
}

func (s *snapmgrTestSuite) TestInstallSnapdAlreadyOnTrackPlansOnce(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	snapstate.Set(s.state, "snapd", nil)

	_, err := snapstate.Install(context.Background(), s.state, "snapd", &snapstate.RevisionOptions{Channel: "18/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	actions := s.snapActions("snapd")
	c.Assert(actions, HasLen, 1)
	c.Check(actions[0].Action, Equals, "install")
	c.Check(actions[0].Channel, Equals, "18/stable")
}

func (s *snapmgrTestSuite) TestInstallSnapdPinnedRevisionKeepsRequestedChannel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	snapstate.Set(s.state, "snapd", nil)

	ts, err := snapstate.Install(context.Background(), s.state, "snapd", &snapstate.RevisionOptions{
		Channel:  "stable",
		Revision: snap.R(42),
	}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	actions := s.snapActions("snapd")
	c.Assert(actions, HasLen, 1)
	c.Check(actions[0].Action, Equals, "install")
	c.Check(actions[0].Channel, Equals, "stable")
	c.Check(actions[0].Revision, Equals, snap.R(42))
	sup := snapSetupFromTasks(c, ts)
	c.Check(sup.Channel, Equals, "stable")
	c.Check(sup.Revision(), Equals, snap.R(42))
}

func (s *snapmgrTestSuite) TestInstallSnapdByRevisionLeavesChannelEmpty(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	snapstate.Set(s.state, "snapd", nil)

	ts, err := snapstate.Install(context.Background(), s.state, "snapd", &snapstate.RevisionOptions{
		Revision: snap.R(42),
	}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	actions := s.snapActions("snapd")
	c.Assert(actions, HasLen, 1)
	c.Check(actions[0].Action, Equals, "install")
	c.Check(actions[0].Channel, Equals, "")
	c.Check(actions[0].Revision, Equals, snap.R(42))
	// By-revision install tracks stable. Track policy must not turn that into
	// the Ubuntu Core track.
	sup := snapSetupFromTasks(c, ts)
	c.Check(sup.Channel, Equals, "stable")
	c.Check(sup.Revision(), Equals, snap.R(42))
}

func (s *snapmgrTestSuite) TestInstallSnapdKeepsFipsUpdatesChannel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	snapstate.Set(s.state, "snapd", nil)

	ts, err := snapstate.Install(context.Background(), s.state, "snapd", &snapstate.RevisionOptions{Channel: "fips-updates/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	actions := s.snapActions("snapd")
	c.Assert(actions, HasLen, 1)
	c.Check(actions[0].Action, Equals, "install")
	c.Check(actions[0].Channel, Equals, "fips-updates/stable")
	c.Check(snapSetupFromTasks(c, ts).Channel, Equals, "fips-updates/stable")
}

func (s *snapmgrTestSuite) TestInstallSnapdKeepsCohortAndRequestedChannel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	snapstate.Set(s.state, "snapd", nil)

	ts, err := snapstate.Install(context.Background(), s.state, "snapd", &snapstate.RevisionOptions{
		Channel:   "stable",
		CohortKey: "cohort-1",
	}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	actions := s.snapActions("snapd")
	c.Assert(actions, HasLen, 1)
	c.Check(actions[0].Action, Equals, "install")
	c.Check(actions[0].CohortKey, Equals, "cohort-1")
	c.Check(actions[0].Channel, Equals, "stable")
	sup := snapSetupFromTasks(c, ts)
	c.Check(sup.Channel, Equals, "stable")
	c.Check(sup.CohortKey, Equals, "cohort-1")
}

func (s *snapmgrTestSuite) TestInstallSnapdPinnedRevisionIgnoresMappedTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	snapstate.Set(s.state, "snapd", nil)
	s.fakeStore.revisionNotAvailableOnChannel = map[string]bool{
		"18/stable": true,
	}

	ts, err := snapstate.Install(context.Background(), s.state, "snapd", &snapstate.RevisionOptions{
		Channel:  "stable",
		Revision: snap.R(42),
	}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	actions := s.snapActions("snapd")
	c.Assert(actions, HasLen, 1)
	c.Check(actions[0].Channel, Equals, "stable")
	c.Check(actions[0].Revision, Equals, snap.R(42))
	c.Check(snapSetupFromTasks(c, ts).Channel, Equals, "stable")
}

func (s *snapmgrTestSuite) TestInstallSnapdValidationSetKeepsRequestedChannel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	snapstate.Set(s.state, "snapd", nil)

	vsets := snapdValidationSetsPinning(c, "42")
	restoreVsets := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		return vsets, nil
	})
	defer restoreVsets()

	ts, err := snapstate.Install(context.Background(), s.state, "snapd", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	actions := s.snapActions("snapd")
	c.Assert(actions, HasLen, 1)
	c.Check(actions[0].Action, Equals, "install")
	// The validation-set pin clears the action channel. There is no mapped
	// channel to restore, and tracking stays on the requested channel.
	c.Check(actions[0].Channel, Equals, "")
	c.Check(actions[0].Revision, Equals, snap.R(42))
	sup := snapSetupFromTasks(c, ts)
	c.Check(sup.Channel, Equals, "stable")
	c.Check(sup.Revision(), Equals, snap.R(42))
}

func (s *snapmgrTestSuite) TestInstallSnapdValidationSetIgnoresMappedTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	snapstate.Set(s.state, "snapd", nil)
	s.fakeStore.revisionNotAvailableOnChannel = map[string]bool{
		"18/stable": true,
	}

	vsets := snapdValidationSetsPinning(c, "42")
	restoreVsets := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		return vsets, nil
	})
	defer restoreVsets()

	ts, err := snapstate.Install(context.Background(), s.state, "snapd", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	actions := s.snapActions("snapd")
	c.Assert(actions, HasLen, 1)
	c.Check(actions[0].Channel, Equals, "")
	c.Check(actions[0].Revision, Equals, snap.R(42))
	c.Check(snapSetupFromTasks(c, ts).Channel, Equals, "stable")
}

func (s *snapmgrTestSuite) TestUpdateSnapdValidationSetRevisionQueriesMappedTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	s.setSnapdTracking("latest/stable")

	vsets := snapdValidationSetsPinning(c, "42")
	restoreVsets := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		return vsets, nil
	})
	defer restoreVsets()

	_, err := snapstate.Update(s.state, "snapd", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	actions := s.snapActions("snapd")
	c.Assert(actions, HasLen, 2)
	assertLatestStablePrecursor(c, actions[0], "validation set refresh")
	c.Check(actions[1].Action, Equals, "refresh")
	c.Check(actions[1].Channel, Equals, "18/stable")
	c.Check(actions[1].Revision, Equals, snap.R(42))
}

func (s *snapmgrTestSuite) TestUpdateSnapdRemapsTrackingChannel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	s.setSnapdTracking("latest/stable")

	ts, err := snapstate.Update(s.state, "snapd", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	actions := s.snapActions("snapd")
	c.Assert(actions, HasLen, 2)
	assertLatestStablePrecursor(c, actions[0], "tracked channel")
	s.assertLatestStableQueryOmitsInstalledSnaps(c)
	c.Check(actions[1].Action, Equals, "refresh")
	c.Check(actions[1].Channel, Equals, "18/stable")
	c.Check(snapSetupFromTasks(c, ts).Channel, Equals, "18/stable")
}

func (s *snapmgrTestSuite) TestUpdateSnapdByRevisionQueriesMappedTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	s.setSnapdTracking("latest/stable")

	ts, err := snapstate.Update(s.state, "snapd", &snapstate.RevisionOptions{
		Revision: snap.R(42),
	}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	actions := s.snapActions("snapd")
	c.Assert(actions, HasLen, 2)
	assertLatestStablePrecursor(c, actions[0], "revision refresh")
	c.Check(actions[1].Action, Equals, "refresh")
	// No channel was requested, so the tracking channel is resolved and the
	// revision is requested on that track.
	c.Check(actions[1].Channel, Equals, "18/stable")
	c.Check(actions[1].Revision, Equals, snap.R(42))
	sup := snapSetupFromTasks(c, ts)
	c.Check(sup.Channel, Equals, "18/stable")
	c.Check(sup.Revision(), Equals, snap.R(42))
}

func (s *snapmgrTestSuite) TestUpdateSnapdNoUpdateOnMappedTrackSwitchesChannel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	s.setSnapdTracking("latest/stable")
	s.fakeStore.noUpdateForChannel = func(channel string) bool {
		return channel == "18/stable"
	}

	ts, err := snapstate.Update(s.state, "snapd", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	actions := s.snapActions("snapd")
	c.Assert(actions, HasLen, 2)
	assertLatestStablePrecursor(c, actions[0], "no update")
	c.Check(actions[1].Action, Equals, "refresh")
	c.Check(actions[1].Channel, Equals, "18/stable")

	c.Assert(ts.Tasks(), HasLen, 1)
	c.Check(ts.Tasks()[0].Kind(), Equals, "switch-snap-channel")
	sup := snapSetupFromTasks(c, ts)
	c.Check(sup.Channel, Equals, "18/stable")
	c.Check(sup.Revision(), Equals, snap.R(1))
}

func (s *snapmgrTestSuite) TestUpdateSnapdStoreRedirectToOtherTrackFails(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	s.setSnapdTracking("latest/stable")
	s.fakeStore.redirectForAction = func(action *store.SnapAction) string {
		if strings.HasPrefix(action.Channel, "18/") {
			return "24/stable"
		}
		return ""
	}

	_, err := snapstate.Update(s.state, "snapd", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `cannot follow Ubuntu Core track "18/stable": store redirected to "24/stable"`)
}

func (s *snapmgrTestSuite) TestInstallSnapdDoesNotFollowMappedTrackRedirect(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	snapstate.Set(s.state, "snapd", nil)
	s.fakeStore.redirectForAction = func(action *store.SnapAction) string {
		if strings.HasPrefix(action.Channel, "18/") {
			return "24/stable"
		}
		return ""
	}

	ts, err := snapstate.Install(context.Background(), s.state, "snapd", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(snapSetupFromTasks(c, ts).Channel, Equals, "stable")
}

func (s *snapmgrTestSuite) TestInstallSnapdKeepsChannelWhenStoreWouldRedirect(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	snapstate.Set(s.state, "snapd", nil)
	s.fakeStore.redirectForAction = func(action *store.SnapAction) string {
		if strings.HasPrefix(action.Channel, "18/") {
			return "18/edge"
		}
		return ""
	}

	ts, err := snapstate.Install(context.Background(), s.state, "snapd", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(snapSetupFromTasks(c, ts).Channel, Equals, "stable")
}

type snapdChannelPassThroughCase struct {
	label     string
	model     *asserts.Model
	tracks    snap.UbuntuCoreTracks
	channel   string
	want      string
	precursor bool
}

func (s *snapmgrTestSuite) assertSnapdChannelPassThrough(c *C, refresh bool, cases []snapdChannelPassThroughCase) {
	action := "install"
	if refresh {
		action = "refresh"
	}
	for _, t := range cases {
		s.fakeBackend.ops = nil
		restore := snapstatetest.MockDeviceModel(t.model)
		s.mockSnapdUbuntuCoreTracks(t.tracks)

		var ts *state.TaskSet
		var err error
		if refresh {
			s.setSnapdTracking("latest/stable")
			_, err = snapstate.Update(s.state, "snapd", &snapstate.RevisionOptions{Channel: t.channel}, s.user.ID, snapstate.Flags{})
		} else {
			snapstate.Set(s.state, "snapd", nil)
			ts, err = snapstate.Install(context.Background(), s.state, "snapd", &snapstate.RevisionOptions{Channel: t.channel}, s.user.ID, snapstate.Flags{})
		}
		restore()
		c.Assert(err, IsNil, Commentf(t.label))

		actions := s.snapActions("snapd")
		planning := actions
		if t.precursor {
			c.Assert(actions, HasLen, 2, Commentf(t.label))
			assertLatestStablePrecursor(c, actions[0], t.label)
			planning = actions[1:]
		} else {
			c.Assert(actions, HasLen, 1, Commentf(t.label))
		}
		c.Check(planning[0].Action, Equals, action, Commentf(t.label))
		c.Check(planning[0].Channel, Equals, t.want, Commentf(t.label))
		if !refresh {
			c.Check(snapSetupFromTasks(c, ts).Channel, Equals, t.want, Commentf(t.label))
		}
	}
}

func (s *snapmgrTestSuite) TestUpdateSnapdChannelPassThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.assertSnapdChannelPassThrough(c, true, []snapdChannelPassThroughCase{
		{label: "classic", model: ClassicModel(), tracks: ucTracks18, channel: "latest/stable", want: "latest/stable"},
		{label: "missing map", model: ModelWithBase("core18"), channel: "latest/stable", want: "latest/stable", precursor: true},
	})
}

func (s *snapmgrTestSuite) TestUpdateSnapdRequestedTrackDiffersFromResolved(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	s.setSnapdTracking("latest/stable")

	_, err := snapstate.Update(s.state, "snapd", &snapstate.RevisionOptions{Channel: "20/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `cannot use requested track "20": resolved track is "18"`)
}

func (s *snapmgrTestSuite) TestInstallSnapdChannelPassThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.assertSnapdChannelPassThrough(c, false, []snapdChannelPassThroughCase{
		{label: "classic", model: ClassicModel(), tracks: ucTracks18, channel: "stable", want: "stable"},
		{label: "missing map", model: ModelWithBase("core18"), channel: "stable", want: "stable"},
		{label: "unknown track", model: ModelWithBase("core18"), tracks: ucTracks18, channel: "20/stable", want: "20/stable"},
	})
}

func (s *snapmgrTestSuite) TestUpdateSnapdDoesNotRemapOtherSnaps(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	s.setSnapdTracking("latest/stable")
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "stable",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "some-snap",
			SnapID:   "some-snap-id",
			Revision: snap.R(1),
		}}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	_, _, err := snapstate.UpdateMany(context.Background(), s.state, []string{"snapd", "some-snap"}, nil, s.user.ID, nil)
	c.Assert(err, IsNil)

	snapdActions := s.snapActions("snapd")
	c.Assert(snapdActions, HasLen, 2)
	assertLatestStablePrecursor(c, snapdActions[0], "batch")
	c.Check(snapdActions[1].Action, Equals, "refresh")
	c.Check(snapdActions[1].Channel, Equals, "18/stable")

	other := s.snapActions("some-snap")
	c.Assert(other, HasLen, 1)
	c.Check(other[0].Channel, Not(Equals), "18/stable")
}

func (s *snapmgrTestSuite) TestDownloadSnapdDoesNotRemap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)

	ts, _, err := snapstate.Download(context.Background(), s.state, "snapd", nil, c.MkDir(), snapstate.RevisionOptions{Channel: "stable"}, snapstate.Options{})
	c.Assert(err, IsNil)

	actions := s.snapActions("snapd")
	c.Assert(actions, HasLen, 1)
	c.Check(actions[0].Action, Equals, "download")
	c.Check(actions[0].Channel, Equals, "stable")
	c.Check(snapSetupFromTasks(c, ts).Channel, Equals, "stable")
}

func (s *snapmgrTestSuite) TestPathInstallSnapdDoesNotRemap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	s.mockSnapdUbuntuCoreTracks(ucTracks18)
	snapstate.Set(s.state, "snapd", nil)

	path := makeTestSnap(c, `name: snapd
version: 1.0
type: snapd
`)
	ts, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "snapd"}, path, "", "latest/stable", snapstate.Flags{}, nil)
	c.Assert(err, IsNil)
	c.Check(s.snapActions("snapd"), HasLen, 0)
	c.Check(snapSetupFromTasks(c, ts).Channel, Equals, "latest/stable")
}

func snapdValidationSetsPinning(c *C, revision string) *snapasserts.ValidationSets {
	headers := map[string]any{
		"type":         "validation-set",
		"timestamp":    time.Now().Format(time.RFC3339),
		"authority-id": "foo",
		"series":       "16",
		"account-id":   "foo",
		"name":         "bar",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "snapd",
				"id":       snaptest.AssertedSnapID("snapd"),
				"presence": "required",
				"revision": revision,
			},
		},
	}
	signing := assertstest.NewStoreStack("can0nical", nil)
	a, err := signing.Sign(asserts.ValidationSetType, headers, nil, "")
	c.Assert(err, IsNil)

	vsets := snapasserts.NewValidationSets()
	c.Assert(vsets.Add(a.(*asserts.ValidationSet)), IsNil)
	c.Assert(vsets.Conflict(), IsNil)
	return vsets
}
