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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

var uc18SnapdTracks = map[string]map[string]map[string]string{
	"ubuntu-core": {
		"18": {
			"latest":       "18",
			"fips-updates": "18-fips",
		},
	},
}

func (s *targetTestSuite) prepareSnapdUCTracks(c *C, model *asserts.Model, tracks map[string]map[string]map[string]string) {
	s.AddCleanup(release.MockOnClassic(false))
	if model != nil {
		s.AddCleanup(snapstatetest.MockDeviceModel(model))
	}
	s.fakeStore.mutateSnapInfo = func(info *snap.Info) error {
		if info.SnapType == snap.TypeSnapd {
			info.TrackRedirects = tracks
		}
		return nil
	}
	s.AddCleanup(func() {
		s.fakeStore.mutateSnapInfo = nil
		s.fakeStore.redirectChannel = nil
		s.fakeStore.noUpdateOnChannel = nil
		s.fakeStore.revisionNotAvailableOnChannel = nil
	})
}

const snapdUCTrackYaml = `name: snapd
type: snapd
version: 1.0
`

func snapdActionChannels(ops fakeOps) []string {
	var chans []string
	for _, op := range ops {
		if op.op == "storesvc-snap-action:action" && op.action.InstanceName == "snapd" {
			chans = append(chans, op.action.Channel)
		}
	}
	return chans
}

func snapdActionRevisions(ops fakeOps) []snap.Revision {
	var revs []snap.Revision
	for _, op := range ops {
		if op.op == "storesvc-snap-action:action" && op.action.InstanceName == "snapd" {
			revs = append(revs, op.action.Revision)
		}
	}
	return revs
}

func snapdActionCohorts(ops fakeOps) []string {
	var keys []string
	for _, op := range ops {
		if op.op == "storesvc-snap-action:action" && op.action.InstanceName == "snapd" {
			keys = append(keys, op.action.CohortKey)
		}
	}
	return keys
}

func (s *targetTestSuite) TestInstallSnapdRemapsUCTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", nil)
	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel: "stable",
		},
	})
	info, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)
	c.Assert(info, NotNil)

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "18/stable")
	c.Check(snapdActionChannels(s.fakeBackend.ops), DeepEquals, []string{"stable", "18/stable"})
}

func (s *targetTestSuite) TestInstallSnapdRemapsFipsUpdatesTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", nil)
	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel: "fips-updates/stable",
		},
	})
	_, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "18-fips/stable")
	c.Check(snapdActionChannels(s.fakeBackend.ops), DeepEquals, []string{"fips-updates/stable", "18-fips/stable"})
}

func (s *targetTestSuite) TestInstallSnapdNameBasedAPIRemaps(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", nil)
	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)

	ts, err := snapstate.Install(context.Background(), s.state, "snapd", &snapstate.RevisionOptions{Channel: "stable"}, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "18/stable")
	c.Check(snapdActionChannels(s.fakeBackend.ops), DeepEquals, []string{"stable", "18/stable"})
}

func (s *targetTestSuite) TestInstallSnapdExplicitLatestRemaps(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", nil)
	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel: "latest/stable",
		},
	})
	_, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "18/stable")
	c.Check(snapdActionChannels(s.fakeBackend.ops), DeepEquals, []string{"latest/stable", "18/stable"})
}

func (s *targetTestSuite) TestInstallSnapdRefusesUnknownTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", nil)
	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel: "20/stable",
		},
	})
	_, _, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, ErrorMatches, `cannot install snapd: channel "20/stable" is not a UC track for boot base 18`)
}

func (s *targetTestSuite) TestInstallSnapdRefusesLegacyTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", nil)
	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel: "2.0/stable",
		},
	})
	_, _, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, ErrorMatches, `cannot install snapd: channel "2.0/stable" is not a UC track for boot base 18`)
}

func (s *targetTestSuite) TestInstallSnapdRevisionQueriesMappedTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", nil)
	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel:  "stable",
			Revision: snap.R(7),
		},
	})
	_, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "18/stable")
	c.Check(snapsup.Revision(), Equals, snap.R(7))
	c.Check(snapdActionChannels(s.fakeBackend.ops), DeepEquals, []string{"stable", "18/stable"})
	c.Check(snapdActionRevisions(s.fakeBackend.ops), DeepEquals, []snap.Revision{snap.R(7), snap.R(7)})
}

func (s *targetTestSuite) TestInstallSnapdRevisionNotOnMappedTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", nil)
	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)
	s.fakeStore.revisionNotAvailableOnChannel = map[string]bool{
		"18/stable": true,
	}

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel:  "stable",
			Revision: snap.R(7),
		},
	})
	_, _, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, ErrorMatches, `no snap revision available as specified`)
}

func (s *targetTestSuite) TestInstallSnapdKeepsCohortOnRemap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", nil)
	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel:   "stable",
			CohortKey: "cohort-1",
		},
	})
	_, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "18/stable")
	c.Check(snapsup.CohortKey, Equals, "cohort-1")
	c.Check(snapdActionCohorts(s.fakeBackend.ops), DeepEquals, []string{"cohort-1", "cohort-1"})
}

func (s *targetTestSuite) TestPathInstallSnapdDoesNotRemap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", nil)
	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)
	path := makeTestSnap(c, snapdUCTrackYaml)

	ts, err := snapstate.InstallPath(s.state, &snap.SideInfo{RealName: "snapd"}, path, "", "latest/stable", snapstate.Flags{}, nil)
	c.Assert(err, IsNil)

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "latest/stable")
	c.Check(snapdActionChannels(s.fakeBackend.ops), HasLen, 0)
}

func (s *targetTestSuite) TestInstallSnapdNoRemapOnClassic(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", nil)
	s.prepareSnapdUCTracks(c, ClassicModel(), uc18SnapdTracks)

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel: "stable",
		},
	})
	_, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "stable")
	c.Check(snapdActionChannels(s.fakeBackend.ops), DeepEquals, []string{"stable"})
}

func (s *targetTestSuite) TestInstallSnapdNoRemapOnHybridClassic(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", nil)
	s.prepareSnapdUCTracks(c, MakeModelClassicWithModes("pc", nil), uc18SnapdTracks)

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel: "stable",
		},
	})
	_, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "stable")
	c.Check(snapdActionChannels(s.fakeBackend.ops), DeepEquals, []string{"stable"})
}

func (s *targetTestSuite) TestInstallSnapdNoRemapOnUC16(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", nil)
	// DefaultModel is UC16 (no base).
	s.prepareSnapdUCTracks(c, DefaultModel(), uc18SnapdTracks)

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel: "stable",
		},
	})
	_, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "stable")
	c.Check(snapdActionChannels(s.fakeBackend.ops), DeepEquals, []string{"stable"})
}

func (s *targetTestSuite) TestInstallSnapdNoRemapWhenMapMissing(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", nil)
	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), nil)

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel: "stable",
		},
	})
	_, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "stable")
	c.Check(snapdActionChannels(s.fakeBackend.ops), DeepEquals, []string{"stable"})
}

func (s *targetTestSuite) TestInstallSnapdNoRemapWhenAlreadyOnTargetTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", nil)
	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel: "18/stable",
		},
	})
	_, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "18/stable")
	c.Check(snapdActionChannels(s.fakeBackend.ops), DeepEquals, []string{"18/stable"})
}

func (s *targetTestSuite) TestInstallSnapdRejectsRedirectOffMappedTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", nil)
	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)
	s.fakeStore.redirectChannel = map[string]string{
		"18/stable": "2.0/stable",
	}

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel: "stable",
		},
	})
	_, _, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, ErrorMatches, `cannot install snapd: store redirected from "18/stable" to "2.0/stable"`)
}

func (s *targetTestSuite) TestInstallSnapdIgnoresSameTrackRedirect(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", nil)
	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)
	s.fakeStore.redirectChannel = map[string]string{
		"18/stable": "18/edge",
	}

	goal := snapstate.StoreInstallGoal(snapstate.StoreSnap{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel: "stable",
		},
	})
	_, ts, err := snapstate.InstallOne(context.Background(), s.state, goal, snapstate.Options{})
	c.Assert(err, IsNil)

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "18/stable")
}

func (s *targetTestSuite) TestUpdateSnapdRemapsUCTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/stable",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "snapd",
	})

	goal := snapstate.StoreUpdateGoal(snapstate.StoreUpdate{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel: "stable",
		},
	})
	ts, err := snapstate.UpdateOne(context.Background(), s.state, goal, nil, snapstate.Options{})
	c.Assert(err, IsNil)

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "18/stable")
	c.Check(snapdActionChannels(s.fakeBackend.ops), DeepEquals, []string{"latest/stable", "18/stable"})
}

func (s *targetTestSuite) TestUpdateSnapdLocalOnlySwitchesChannel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)
	s.fakeStore.noUpdateOnChannel = map[string]bool{
		"18/stable": true,
	}

	si := &snap.SideInfo{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)}
	snaptest.MockSnapCurrent(c, snapdUCTrackYaml, si)
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/stable",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:         snap.R(1),
		SnapType:        "snapd",
	})

	goal := snapstate.StoreUpdateGoal(snapstate.StoreUpdate{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel: "stable",
		},
	})
	ts, err := snapstate.UpdateOne(context.Background(), s.state, goal, nil, snapstate.Options{})
	c.Assert(err, IsNil)

	c.Check(ts.Tasks()[0].Kind(), Equals, "switch-snap-channel")
	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Channel, Equals, "18/stable")
	c.Check(snapsup.Revision(), Equals, snap.R(1))
	c.Check(snapdActionChannels(s.fakeBackend.ops), DeepEquals, []string{"latest/stable", "18/stable"})
}

func (s *targetTestSuite) TestUpdateSnapdRefusesUnknownTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/stable",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "snapd",
	})

	goal := snapstate.StoreUpdateGoal(snapstate.StoreUpdate{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel: "20/stable",
		},
	})
	_, err := snapstate.UpdateOne(context.Background(), s.state, goal, nil, snapstate.Options{})
	c.Assert(err, ErrorMatches, `cannot refresh snapd: channel "20/stable" is not a UC track for boot base 18`)
}

func (s *targetTestSuite) TestUpdateSnapdRejectsRedirectOffMappedTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)
	s.fakeStore.redirectChannel = map[string]string{
		"18/stable": "2.0/stable",
	}
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/stable",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "snapd",
	})

	goal := snapstate.StoreUpdateGoal(snapstate.StoreUpdate{
		InstanceName: "snapd",
		RevOpts: snapstate.RevisionOptions{
			Channel: "stable",
		},
	})
	_, err := snapstate.UpdateOne(context.Background(), s.state, goal, nil, snapstate.Options{})
	c.Assert(err, ErrorMatches, `cannot refresh snapd: store redirected from "18/stable" to "2.0/stable"`)
}

func snapdActionKinds(ops fakeOps) []string {
	var kinds []string
	for _, op := range ops {
		if op.op == "storesvc-snap-action:action" && op.action.InstanceName == "snapd" {
			kinds = append(kinds, op.action.Action)
		}
	}
	return kinds
}

func (s *targetTestSuite) TestDownloadSnapdRemapsUCTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)

	ts, info, err := snapstate.Download(context.Background(), s.state, "snapd", nil, c.MkDir(), snapstate.RevisionOptions{
		Channel: "stable",
	}, snapstate.Options{})
	c.Assert(err, IsNil)
	c.Assert(info, NotNil)

	downloadSnap := ts.MaybeEdge(snapstate.BeginEdge)
	c.Assert(downloadSnap, NotNil)
	var snapsup snapstate.SnapSetup
	c.Assert(downloadSnap.Get("snap-setup", &snapsup), IsNil)
	c.Check(snapsup.Channel, Equals, "18/stable")
	c.Check(snapdActionChannels(s.fakeBackend.ops), DeepEquals, []string{"stable", "18/stable"})
	c.Check(snapdActionKinds(s.fakeBackend.ops), DeepEquals, []string{"download", "download"})
}

func (s *targetTestSuite) TestDownloadSnapdRejectsRedirectOffMappedTrack(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)
	s.fakeStore.redirectChannel = map[string]string{
		"18/stable": "2.0/stable",
	}

	_, _, err := snapstate.Download(context.Background(), s.state, "snapd", nil, c.MkDir(), snapstate.RevisionOptions{
		Channel: "stable",
	}, snapstate.Options{})
	c.Assert(err, ErrorMatches, `cannot download snapd: store redirected from "18/stable" to "2.0/stable"`)
}

func (s *targetTestSuite) TestDownloadSnapdIgnoresSameTrackRedirect(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.prepareSnapdUCTracks(c, ModelWithBase("core18"), uc18SnapdTracks)
	s.fakeStore.redirectChannel = map[string]string{
		"18/stable": "18/edge",
	}

	ts, _, err := snapstate.Download(context.Background(), s.state, "snapd", nil, c.MkDir(), snapstate.RevisionOptions{
		Channel: "stable",
	}, snapstate.Options{})
	c.Assert(err, IsNil)

	downloadSnap := ts.MaybeEdge(snapstate.BeginEdge)
	c.Assert(downloadSnap, NotNil)
	var snapsup snapstate.SnapSetup
	c.Assert(downloadSnap.Get("snap-setup", &snapsup), IsNil)
	c.Check(snapsup.Channel, Equals, "18/stable")
}
