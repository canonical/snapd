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
	"errors"
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
	"github.com/snapcore/snapd/snap/uctrack"
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

func (s *snapmgrTestSuite) TestUpdateSnapdExplicitTrackingChannelIsProhibited(c *C) {
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

	// The requested channel repeats the tracking track, so the pinned revision
	// is not consulted.
	_, err := snapstate.Update(s.state, "snapd", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `cannot use requested track "latest": resolved track is "18"`)
	c.Check(errors.Is(err, uctrack.ErrRequestedChannelProhibited), Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateSnapdValidationSetPin(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	vsets := snapdValidationSetsPinning(c, "42")
	restoreVsets := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		return vsets, nil
	})
	defer restoreVsets()
	defer func() { s.fakeStore.revisionNotAvailableOnChannel = nil }()

	// The pin is a revision. Channel policy runs first, then the pin is
	// requested on the channel that policy produced.
	for _, t := range []struct {
		label       string
		channel     string
		revision    snap.Revision
		refreshAll  bool
		unavailable bool
		refreshSent bool
		action      string
		setup       string
		err         string
		prohibited  bool
	}{
		{label: "follow", refreshSent: true, action: "18/stable", setup: "18/stable"},
		{label: "honor edge", channel: "18/edge", refreshSent: true, action: "18/edge", setup: "18/edge"},
		{label: "unavailable", unavailable: true, refreshSent: true, action: "18/stable", err: "no snap revision available as specified"},
		{label: "revision conflicts", channel: "18/edge", revision: snap.R(7), err: `cannot update snap "snapd" to revision 7 without --ignore-validation, revision 42 is required by validation sets: 16/foo/bar/1`},
		{label: "prohibited channel", channel: "stable", revision: snap.R(7), err: `cannot use requested track "latest": resolved track is "18"`, prohibited: true},
		{label: "refresh all", refreshAll: true, refreshSent: true, action: "18/stable", setup: "18/stable"},
	} {
		s.fakeBackend.ops = nil
		s.fakeStore.revisionNotAvailableOnChannel = nil
		restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
		s.mockSnapdUbuntuCoreTracks(ucTracks18)
		s.setSnapdTracking("latest/stable")
		if t.unavailable {
			s.fakeStore.revisionNotAvailableOnChannel = map[string]bool{t.action: true}
		}

		var (
			ts  *state.TaskSet
			tss []*state.TaskSet
			err error
		)
		if t.refreshAll {
			_, tss, err = snapstate.UpdateMany(context.Background(), s.state, nil, nil, s.user.ID, nil)
		} else {
			var opts *snapstate.RevisionOptions
			if t.channel != "" || !t.revision.Unset() {
				opts = &snapstate.RevisionOptions{Channel: t.channel, Revision: t.revision}
			}
			ts, err = snapstate.Update(s.state, "snapd", opts, s.user.ID, snapstate.Flags{})
		}
		restore()

		actions := s.snapActions("snapd")
		c.Assert(actions, Not(HasLen), 0, Commentf(t.label))
		assertLatestStablePrecursor(c, actions[0], t.label)
		refresh := actions[1:]
		if !t.refreshSent {
			c.Assert(refresh, HasLen, 0, Commentf(t.label))
		} else {
			c.Assert(refresh, HasLen, 1, Commentf(t.label))
			c.Check(refresh[0].Action, Equals, "refresh", Commentf(t.label))
			c.Check(refresh[0].Channel, Equals, t.action, Commentf(t.label))
			c.Check(refresh[0].Revision, Equals, snap.R(42), Commentf(t.label))
		}

		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err, Commentf(t.label))
			if t.prohibited {
				c.Check(errors.Is(err, uctrack.ErrRequestedChannelProhibited), Equals, true, Commentf(t.label))
			}
			continue
		}

		c.Assert(err, IsNil, Commentf(t.label))
		var sup snapstate.SnapSetup
		if t.refreshAll {
			sup = snapdSetupFromTaskSets(c, tss)
		} else {
			sup = snapSetupFromTasks(c, ts)
		}
		c.Check(sup.Channel, Equals, t.setup, Commentf(t.label))
		c.Check(sup.Revision(), Equals, snap.R(42), Commentf(t.label))
	}
}

func snapdSetupFromTaskSets(c *C, tss []*state.TaskSet) snapstate.SnapSetup {
	for _, ts := range tss {
		t := ts.MaybeEdge(snapstate.SnapSetupEdge)
		if t == nil {
			continue
		}
		var sup snapstate.SnapSetup
		if t.Get("snap-setup", &sup) != nil {
			continue
		}
		if sup.InstanceName() == "snapd" {
			return sup
		}
	}
	c.Fatal("no snapd snap setup in task sets")
	return snapstate.SnapSetup{}
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

func (s *snapmgrTestSuite) TestUpdateSnapdRevisionPermutations(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	defer func() {
		s.fakeStore.redirectForAction = nil
		s.fakeStore.effectiveChannelForAction = nil
		s.fakeStore.revisionNotAvailableOnChannel = nil
	}()

	// Channel rules run first. The revision is then requested on that channel.
	// A prohibited channel is rejected before the refresh is sent.
	for _, t := range []struct {
		label       string
		model       *asserts.Model
		tracking    string
		channel     string
		revision    snap.Revision
		refresh     bool
		action      string
		setup       string
		err         string
		prohibited  bool
		unavailable bool
		precursor   bool
		redirect    string
		effective   string
	}{
		{label: "follow", model: ModelWithBase("core18"), tracking: "latest/stable", revision: snap.R(42), refresh: true, action: "18/stable", setup: "18/stable", precursor: true},
		{label: "stay", model: ModelWithBase("core18"), tracking: "18/stable", revision: snap.R(42), refresh: true, action: "18/stable", setup: "18/stable", precursor: true},
		{label: "honor stable", model: ModelWithBase("core18"), tracking: "latest/stable", channel: "18/stable", revision: snap.R(42), refresh: true, action: "18/stable", setup: "18/stable", precursor: true},
		{label: "honor edge", model: ModelWithBase("core18"), tracking: "latest/stable", channel: "18/edge", revision: snap.R(42), refresh: true, action: "18/edge", setup: "18/edge", precursor: true},
		{label: "honor branch", model: ModelWithBase("core18"), tracking: "latest/stable", channel: "18/edge/hotfix", revision: snap.R(42), refresh: true, action: "18/edge/hotfix", setup: "18/edge/hotfix", precursor: true},
		{label: "stay honor edge", model: ModelWithBase("core18"), tracking: "18/stable", channel: "18/edge", revision: snap.R(42), refresh: true, action: "18/edge", setup: "18/edge", precursor: true},
		{label: "reject latest/stable", model: ModelWithBase("core18"), tracking: "latest/stable", channel: "latest/stable", revision: snap.R(42), err: `cannot use requested track "latest": resolved track is "18"`, prohibited: true, precursor: true},
		{label: "reject stable", model: ModelWithBase("core18"), tracking: "latest/stable", channel: "stable", revision: snap.R(42), err: `cannot use requested track "latest": resolved track is "18"`, prohibited: true, precursor: true},
		{label: "reject latest/edge", model: ModelWithBase("core18"), tracking: "latest/stable", channel: "latest/edge", revision: snap.R(42), err: `cannot use requested track "latest": resolved track is "18"`, prohibited: true, precursor: true},
		{label: "reject 20", model: ModelWithBase("core18"), tracking: "latest/stable", channel: "20/stable", revision: snap.R(42), err: `cannot use requested track "20": resolved track is "18"`, prohibited: true, precursor: true},
		{label: "reject other", model: ModelWithBase("core18"), tracking: "latest/stable", channel: "other/stable", revision: snap.R(42), err: `cannot use requested track "other": resolved track is "18"`, prohibited: true, precursor: true},
		{label: "stay rejects stable", model: ModelWithBase("core18"), tracking: "18/stable", channel: "stable", revision: snap.R(42), err: `cannot use requested track "latest": resolved track is "18"`, prohibited: true, precursor: true},
		{label: "stay rejects edge", model: ModelWithBase("core18"), tracking: "18/stable", channel: "edge", revision: snap.R(42), err: `cannot use requested track "latest": resolved track is "18"`, prohibited: true, precursor: true},
		{label: "unavailable", model: ModelWithBase("core18"), tracking: "latest/stable", revision: snap.R(42), refresh: true, action: "18/stable", err: "no snap revision available as specified", unavailable: true, precursor: true},
		{label: "unavailable on honored channel", model: ModelWithBase("core18"), tracking: "latest/stable", channel: "18/edge", revision: snap.R(42), refresh: true, action: "18/edge", err: "no snap revision available as specified", unavailable: true, precursor: true},
		{label: "installed revision on channel", model: ModelWithBase("core18"), tracking: "latest/stable", revision: snap.R(1), refresh: true, action: "18/stable", setup: "18/stable", precursor: true},
		{label: "installed revision unavailable", model: ModelWithBase("core18"), tracking: "latest/stable", revision: snap.R(1), refresh: true, action: "18/stable", err: "no snap revision available as specified", unavailable: true, precursor: true},
		{label: "redirect ignored", model: ModelWithBase("core18"), tracking: "latest/stable", revision: snap.R(42), refresh: true, action: "18/stable", setup: "18/stable", precursor: true, redirect: "24/stable"},
		{label: "effective matches", model: ModelWithBase("core18"), tracking: "latest/stable", revision: snap.R(42), refresh: true, action: "18/stable", setup: "18/stable", precursor: true, effective: "18/stable"},
		{label: "effective other risk", model: ModelWithBase("core18"), tracking: "latest/stable", revision: snap.R(42), refresh: true, action: "18/stable", err: `cannot follow Ubuntu Core track "18/stable": store reports effective channel "18/edge"`, precursor: true, effective: "18/edge"},
		{label: "effective other track", model: ModelWithBase("core18"), tracking: "latest/stable", revision: snap.R(42), refresh: true, action: "18/stable", err: `cannot follow Ubuntu Core track "18/stable": store reports effective channel "24/stable"`, precursor: true, effective: "24/stable"},
		{label: "fips honor", model: ModelWithBase("core18"), tracking: "fips-updates/stable", channel: "18-fips/edge", revision: snap.R(42), refresh: true, action: "18-fips/edge", setup: "18-fips/edge", precursor: true},
		{label: "fips reject", model: ModelWithBase("core18"), tracking: "fips-updates/stable", channel: "18/stable", revision: snap.R(42), err: `cannot use requested track "18": resolved track is "18-fips"`, prohibited: true, precursor: true},
		{label: "unknown track", model: ModelWithBase("core18"), tracking: "20/stable", revision: snap.R(42), refresh: true, action: "", setup: "20/stable", precursor: true},
		{label: "unknown track channel", model: ModelWithBase("core18"), tracking: "20/stable", channel: "latest/edge", revision: snap.R(42), refresh: true, action: "latest/edge", setup: "latest/edge", precursor: true},
		{label: "classic", model: ClassicModel(), tracking: "latest/stable", revision: snap.R(42), refresh: true, action: "", setup: "latest/stable"},
		{label: "classic channel", model: ClassicModel(), tracking: "latest/stable", channel: "latest/edge", revision: snap.R(42), refresh: true, action: "latest/edge", setup: "latest/edge"},
	} {
		s.fakeBackend.ops = nil
		s.fakeStore.revisionNotAvailableOnChannel = nil
		s.fakeStore.redirectForAction = nil
		s.fakeStore.effectiveChannelForAction = nil
		restore := snapstatetest.MockDeviceModel(t.model)
		s.mockSnapdUbuntuCoreTracks(ucTracks18)
		s.setSnapdTracking(t.tracking)
		if t.unavailable {
			s.fakeStore.revisionNotAvailableOnChannel = map[string]bool{t.action: true}
		}
		actionChannel, redirect, effective := t.action, t.redirect, t.effective
		s.fakeStore.redirectForAction = func(action *store.SnapAction) string {
			if action.Action == "refresh" && action.Channel == actionChannel {
				return redirect
			}
			return ""
		}
		s.fakeStore.effectiveChannelForAction = func(action *store.SnapAction) string {
			if action.Action == "refresh" && action.Channel == actionChannel {
				return effective
			}
			return ""
		}

		opts := &snapstate.RevisionOptions{Revision: t.revision}
		if t.channel != "" {
			opts.Channel = t.channel
		}
		ts, err := snapstate.Update(s.state, "snapd", opts, s.user.ID, snapstate.Flags{})
		restore()

		actions := s.snapActions("snapd")
		refresh := actions
		if t.precursor {
			c.Assert(actions, Not(HasLen), 0, Commentf(t.label))
			assertLatestStablePrecursor(c, actions[0], t.label)
			refresh = actions[1:]
		}
		if !t.refresh {
			c.Assert(refresh, HasLen, 0, Commentf(t.label))
		} else {
			c.Assert(refresh, HasLen, 1, Commentf(t.label))
			c.Check(refresh[0].Action, Equals, "refresh", Commentf(t.label))
			c.Check(refresh[0].Channel, Equals, t.action, Commentf(t.label))
			c.Check(refresh[0].Revision, Equals, t.revision, Commentf(t.label))
		}

		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err, Commentf(t.label))
			if t.prohibited {
				c.Check(errors.Is(err, uctrack.ErrRequestedChannelProhibited), Equals, true, Commentf(t.label))
			}
			continue
		}

		c.Assert(err, IsNil, Commentf(t.label))
		sup := snapSetupFromTasks(c, ts)
		c.Check(sup.Channel, Equals, t.setup, Commentf(t.label))
		c.Check(sup.Revision(), Equals, t.revision, Commentf(t.label))
	}
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

func (s *snapmgrTestSuite) TestUpdateSnapdIgnoresStoreRedirect(c *C) {
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
	defer func() { s.fakeStore.redirectForAction = nil }()

	ts, err := snapstate.Update(s.state, "snapd", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	actions := s.snapActions("snapd")
	c.Assert(actions, HasLen, 2)
	assertLatestStablePrecursor(c, actions[0], "redirect ignored")
	c.Check(actions[1].Action, Equals, "refresh")
	c.Check(actions[1].Channel, Equals, "18/stable")
	c.Check(snapSetupFromTasks(c, ts).Channel, Equals, "18/stable")
}

func (s *snapmgrTestSuite) TestUpdateSnapdEffectiveChannelMustMatchRequest(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	defer func() {
		s.fakeStore.redirectForAction = nil
		s.fakeStore.effectiveChannelForAction = nil
	}()

	for _, t := range []struct {
		label     string
		tracking  string
		channel   string
		redirect  string
		effective string
		sent      string
		err       string
	}{
		{label: "redirect ignored", tracking: "latest/stable", redirect: "18/edge", sent: "18/stable"},
		{label: "effective matches", tracking: "latest/stable", redirect: "24/stable", effective: "18/stable", sent: "18/stable"},
		{label: "honored channel", tracking: "latest/stable", channel: "18/edge", redirect: "18/beta", effective: "18/edge", sent: "18/edge"},
		{label: "effective other risk", tracking: "latest/stable", effective: "18/edge", sent: "18/stable", err: `cannot follow Ubuntu Core track "18/stable": store reports effective channel "18/edge"`},
		{label: "effective other track", tracking: "latest/stable", effective: "24/stable", sent: "18/stable", err: `cannot follow Ubuntu Core track "18/stable": store reports effective channel "24/stable"`},
		{label: "effective latest", tracking: "latest/stable", effective: "latest/stable", sent: "18/stable", err: `cannot follow Ubuntu Core track "18/stable": store reports effective channel "latest/stable"`},
	} {
		s.fakeBackend.ops = nil
		restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
		s.mockSnapdUbuntuCoreTracks(ucTracks18)
		s.setSnapdTracking(t.tracking)
		sent, redirect, effective := t.sent, t.redirect, t.effective
		s.fakeStore.redirectForAction = func(action *store.SnapAction) string {
			if action.Action == "refresh" && action.Channel == sent {
				return redirect
			}
			return ""
		}
		s.fakeStore.effectiveChannelForAction = func(action *store.SnapAction) string {
			if action.Action == "refresh" && action.Channel == sent {
				return effective
			}
			return ""
		}

		var opts *snapstate.RevisionOptions
		if t.channel != "" {
			opts = &snapstate.RevisionOptions{Channel: t.channel}
		}
		ts, err := snapstate.Update(s.state, "snapd", opts, s.user.ID, snapstate.Flags{})
		restore()

		actions := s.snapActions("snapd")
		c.Assert(actions, HasLen, 2, Commentf(t.label))
		assertLatestStablePrecursor(c, actions[0], t.label)
		c.Check(actions[1].Action, Equals, "refresh", Commentf(t.label))
		c.Check(actions[1].Channel, Equals, t.sent, Commentf(t.label))

		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err, Commentf(t.label))
			continue
		}
		c.Assert(err, IsNil, Commentf(t.label))
		c.Check(snapSetupFromTasks(c, ts).Channel, Equals, t.sent, Commentf(t.label))
	}
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

func (s *snapmgrTestSuite) TestUpdateSnapdChannelPermutations(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// Channel only: no revision and no validation sets. An omitted track means
	// latest, so a risk-only channel is not inherited from the tracking track.
	for _, t := range []struct {
		label     string
		model     *asserts.Model
		tracking  string
		channel   string
		want      string
		err       string
		precursor bool
	}{
		{label: "follow latest/stable", model: ModelWithBase("core18"), tracking: "latest/stable", want: "18/stable", precursor: true},
		{label: "follow latest/edge", model: ModelWithBase("core18"), tracking: "latest/edge", want: "18/edge", precursor: true},
		{label: "follow branch", model: ModelWithBase("core18"), tracking: "latest/stable/mybranch", want: "18/stable/mybranch", precursor: true},
		{label: "stay on 18", model: ModelWithBase("core18"), tracking: "18/stable", want: "18/stable", precursor: true},
		{label: "stay switch risk", model: ModelWithBase("core18"), tracking: "18/stable", channel: "18/edge", want: "18/edge", precursor: true},
		{label: "stay rejects latest/edge", model: ModelWithBase("core18"), tracking: "18/stable", channel: "latest/edge", err: `cannot use requested track "latest": resolved track is "18"`, precursor: true},
		{label: "stay rejects stable", model: ModelWithBase("core18"), tracking: "18/stable", channel: "stable", err: `cannot use requested track "latest": resolved track is "18"`, precursor: true},
		{label: "honor 18/stable", model: ModelWithBase("core18"), tracking: "latest/stable", channel: "18/stable", want: "18/stable", precursor: true},
		{label: "honor 18/edge", model: ModelWithBase("core18"), tracking: "latest/stable", channel: "18/edge", want: "18/edge", precursor: true},
		{label: "honor branch", model: ModelWithBase("core18"), tracking: "latest/stable", channel: "18/edge/hotfix", want: "18/edge/hotfix", precursor: true},
		{label: "reject latest/stable", model: ModelWithBase("core18"), tracking: "latest/stable", channel: "latest/stable", err: `cannot use requested track "latest": resolved track is "18"`, precursor: true},
		{label: "reject stable", model: ModelWithBase("core18"), tracking: "latest/stable", channel: "stable", err: `cannot use requested track "latest": resolved track is "18"`, precursor: true},
		{label: "reject latest/edge", model: ModelWithBase("core18"), tracking: "latest/stable", channel: "latest/edge", err: `cannot use requested track "latest": resolved track is "18"`, precursor: true},
		{label: "reject fips-updates", model: ModelWithBase("core18"), tracking: "latest/stable", channel: "fips-updates/stable", err: `cannot use requested track "fips-updates": resolved track is "18"`, precursor: true},
		{label: "reject 20", model: ModelWithBase("core18"), tracking: "latest/stable", channel: "20/stable", err: `cannot use requested track "20": resolved track is "18"`, precursor: true},
		{label: "reject other", model: ModelWithBase("core18"), tracking: "latest/stable", channel: "other/stable", err: `cannot use requested track "other": resolved track is "18"`, precursor: true},
		{label: "fips honor", model: ModelWithBase("core18"), tracking: "fips-updates/stable", channel: "18-fips/edge", want: "18-fips/edge", precursor: true},
		{label: "fips reject 18", model: ModelWithBase("core18"), tracking: "fips-updates/stable", channel: "18/stable", err: `cannot use requested track "18": resolved track is "18-fips"`, precursor: true},
		{label: "fips reject same", model: ModelWithBase("core18"), tracking: "fips-updates/stable", channel: "fips-updates/stable", err: `cannot use requested track "fips-updates": resolved track is "18-fips"`, precursor: true},
		{label: "unknown track no channel", model: ModelWithBase("core18"), tracking: "20/stable", want: "20/stable", precursor: true},
		{label: "unknown track channel", model: ModelWithBase("core18"), tracking: "20/stable", channel: "latest/edge", want: "latest/edge", precursor: true},
		{label: "classic no channel", model: ClassicModel(), tracking: "latest/stable", want: "latest/stable"},
		{label: "classic channel", model: ClassicModel(), tracking: "latest/stable", channel: "latest/stable", want: "latest/stable"},
	} {
		s.fakeBackend.ops = nil
		restore := snapstatetest.MockDeviceModel(t.model)
		s.mockSnapdUbuntuCoreTracks(ucTracks18)
		s.setSnapdTracking(t.tracking)

		var opts *snapstate.RevisionOptions
		if t.channel != "" {
			opts = &snapstate.RevisionOptions{Channel: t.channel}
		}
		ts, err := snapstate.Update(s.state, "snapd", opts, s.user.ID, snapstate.Flags{})
		restore()

		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err, Commentf(t.label))
			c.Check(errors.Is(err, uctrack.ErrRequestedChannelProhibited), Equals, true, Commentf(t.label))
			actions := s.snapActions("snapd")
			c.Assert(actions, HasLen, 1, Commentf(t.label))
			assertLatestStablePrecursor(c, actions[0], t.label)
			continue
		}

		c.Assert(err, IsNil, Commentf(t.label))
		actions := s.snapActions("snapd")
		refresh := actions
		if t.precursor {
			c.Assert(actions, HasLen, 2, Commentf(t.label))
			assertLatestStablePrecursor(c, actions[0], t.label)
			refresh = actions[1:]
		} else {
			c.Assert(actions, HasLen, 1, Commentf(t.label))
		}
		c.Check(refresh[0].Action, Equals, "refresh", Commentf(t.label))
		c.Check(refresh[0].Channel, Equals, t.want, Commentf(t.label))
		c.Check(refresh[0].Revision.Unset(), Equals, true, Commentf(t.label))
		c.Check(snapSetupFromTasks(c, ts).Channel, Equals, t.want, Commentf(t.label))
	}
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
