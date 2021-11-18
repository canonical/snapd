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
	"context"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/testutil"
)

type recordingStore struct {
	storetest.Store

	ops            []string
	refreshedSnaps []*snap.Info
}

func (r *recordingStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	if assertQuery != nil {
		panic("no assertion query support")
	}
	if ctx == nil || !auth.IsEnsureContext(ctx) {
		panic("Ensure marked context required")
	}
	if len(currentSnaps) != len(actions) || len(currentSnaps) == 0 {
		panic("expected in test one action for each current snaps, and at least one snap")
	}
	for _, a := range actions {
		if a.Action != "refresh" {
			panic("expected refresh actions")
		}
	}
	r.ops = append(r.ops, "list-refresh")

	res := []store.SnapActionResult{}
	for _, rs := range r.refreshedSnaps {
		res = append(res, store.SnapActionResult{Info: rs})
	}
	return res, nil, nil
}

type refreshHintsTestSuite struct {
	testutil.BaseTest
	state *state.State

	store *recordingStore
}

var _ = Suite(&refreshHintsTestSuite{})

func (s *refreshHintsTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())

	s.state = state.New(nil)
	s.store = &recordingStore{}
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.ReplaceStore(s.state, s.store)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		},
		Current:         snap.R(5),
		SnapType:        "app",
		UserID:          1,
		CohortKey:       "cohort",
		TrackingChannel: "stable",
	})

	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}

	s.state.Set("refresh-privacy-key", "privacy-key")

	restoreModel := snapstatetest.MockDeviceModel(DefaultModel())
	s.AddCleanup(restoreModel)
	restoreEnforcedValidationSets := snapstate.MockEnforcedValidationSets(func(st *state.State) (*snapasserts.ValidationSets, error) {
		return nil, nil
	})
	s.AddCleanup(restoreEnforcedValidationSets)
	s.AddCleanup(func() {
		dirs.SetRootDir("/")
		snapstate.CanAutoRefresh = nil
		snapstate.AutoAliases = nil
	})
}

func (s *refreshHintsTestSuite) TestLastRefresh(c *C) {
	rh := snapstate.NewRefreshHints(s.state)
	err := rh.Ensure()
	c.Check(err, IsNil)
	c.Check(s.store.ops, DeepEquals, []string{"list-refresh"})
}

func (s *refreshHintsTestSuite) TestLastRefreshNoRefreshNeeded(c *C) {
	s.state.Lock()
	s.state.Set("last-refresh-hints", time.Now().Add(-23*time.Hour))
	s.state.Unlock()

	rh := snapstate.NewRefreshHints(s.state)
	err := rh.Ensure()
	c.Check(err, IsNil)
	c.Check(s.store.ops, HasLen, 0)
}

func (s *refreshHintsTestSuite) TestLastRefreshNoRefreshNeededBecauseOfFullAutoRefresh(c *C) {
	s.state.Lock()
	s.state.Set("last-refresh-hints", time.Now().Add(-48*time.Hour))
	s.state.Unlock()

	s.state.Lock()
	s.state.Set("last-refresh", time.Now().Add(-23*time.Hour))
	s.state.Unlock()

	rh := snapstate.NewRefreshHints(s.state)
	err := rh.Ensure()
	c.Check(err, IsNil)
	c.Check(s.store.ops, HasLen, 0)
}

func (s *refreshHintsTestSuite) TestAtSeedPolicy(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	s.state.Lock()
	defer s.state.Unlock()

	rh := snapstate.NewRefreshHints(s.state)

	// on core, does nothing
	err := rh.AtSeed()
	c.Assert(err, IsNil)
	var t1 time.Time
	err = s.state.Get("last-refresh-hints", &t1)
	c.Check(err, Equals, state.ErrNoState)

	release.MockOnClassic(true)
	// on classic it sets last-refresh-hints to now,
	// postponing it of 24h
	err = rh.AtSeed()
	c.Assert(err, IsNil)
	err = s.state.Get("last-refresh-hints", &t1)
	c.Check(err, IsNil)

	// nop if tried again
	err = rh.AtSeed()
	c.Assert(err, IsNil)
	var t2 time.Time
	err = s.state.Get("last-refresh-hints", &t2)
	c.Check(err, IsNil)
	c.Check(t1.Equal(t2), Equals, true)
}

func (s *refreshHintsTestSuite) TestRefreshHintsStoresRefreshCandidates(c *C) {
	s.state.Lock()
	repo := interfaces.NewRepository()
	for _, iface := range builtin.Interfaces() {
		err := repo.AddInterface(iface)
		c.Assert(err, IsNil)
	}
	ifacerepo.Replace(s.state, repo)

	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", Revision: snap.R(1), SnapID: "other-snap-id"},
		},
		Current:         snap.R(1),
		SnapType:        "app",
		TrackingChannel: "devel",
		UserID:          0,
	})
	s.state.Unlock()

	info2 := &snap.Info{
		Version:       "v1",
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			RealName: "other-snap",
			Revision: snap.R(2),
		},
		DownloadInfo: snap.DownloadInfo{
			Size: int64(88),
		},
	}
	plugs := map[string]*snap.PlugInfo{
		"plug": {
			Snap:      info2,
			Name:      "plug",
			Interface: "content",
			Attrs: map[string]interface{}{
				"default-provider": "foo-snap:",
				"content":          "some-content",
			},
			Apps:  map[string]*snap.AppInfo{},
			Hooks: map[string]*snap.HookInfo{},
		}}
	info2.Plugs = plugs

	s.store.refreshedSnaps = []*snap.Info{{
		Version:       "2",
		Architectures: []string{"all"},
		Base:          "some-base",
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			RealName: "some-snap",
			Revision: snap.R(1),
		},
		DownloadInfo: snap.DownloadInfo{
			Size: int64(99),
		},
	}, info2}

	rh := snapstate.NewRefreshHints(s.state)
	err := rh.Ensure()
	c.Check(err, IsNil)
	c.Check(s.store.ops, DeepEquals, []string{"list-refresh"})

	s.state.Lock()
	defer s.state.Unlock()

	var candidates map[string]*snapstate.RefreshCandidate
	c.Assert(s.state.Get("refresh-candidates", &candidates), IsNil)
	c.Assert(candidates, HasLen, 2)
	cand1 := candidates["some-snap"]
	c.Assert(cand1, NotNil)
	c.Check(cand1.InstanceName(), Equals, "some-snap")
	c.Check(cand1.SnapBase(), Equals, "some-base")
	c.Check(cand1.Type(), Equals, snap.TypeApp)
	c.Check(cand1.DownloadSize(), Equals, int64(99))
	c.Check(cand1.Version, Equals, "2")

	cand2 := candidates["other-snap"]
	c.Assert(cand2, NotNil)
	c.Check(cand2.InstanceName(), Equals, "other-snap")
	c.Check(cand2.SnapBase(), Equals, "")
	c.Check(cand2.Type(), Equals, snap.TypeApp)
	c.Check(cand2.DownloadSize(), Equals, int64(88))
	c.Check(cand2.Version, Equals, "v1")

	var snapst1 snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst1)
	c.Assert(err, IsNil)

	sup, snapst, err := cand1.SnapSetupForUpdate(s.state, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Check(sup, DeepEquals, &snapstate.SnapSetup{
		Base: "some-base",
		Type: "app",
		SideInfo: &snap.SideInfo{
			RealName: "some-snap",
			Revision: snap.R(1),
		},
		PlugsOnly: true,
		CohortKey: "cohort",
		Channel:   "stable",
		Flags: snapstate.Flags{
			IsAutoRefresh: true,
		},
		DownloadInfo: &snap.DownloadInfo{
			Size: int64(99),
		},
	})
	c.Check(snapst, DeepEquals, &snapst1)

	var snapst2 snapstate.SnapState
	err = snapstate.Get(s.state, "other-snap", &snapst2)
	c.Assert(err, IsNil)

	sup, snapst, err = cand2.SnapSetupForUpdate(s.state, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Check(sup, DeepEquals, &snapstate.SnapSetup{
		Type: "app",
		SideInfo: &snap.SideInfo{
			RealName: "other-snap",
			Revision: snap.R(2),
		},
		Prereq:             []string{"foo-snap"},
		PrereqContentAttrs: map[string][]string{"foo-snap": {"some-content"}},
		PlugsOnly:          true,
		Channel:            "devel",
		Flags: snapstate.Flags{
			IsAutoRefresh: true,
		},
		DownloadInfo: &snap.DownloadInfo{
			Size: int64(88),
		},
	})
	c.Check(snapst, DeepEquals, &snapst2)
}

func (s *refreshHintsTestSuite) TestPruneRefreshCandidates(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// enable gate-auto-refresh-hook feature
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.gate-auto-refresh-hook", true)
	tr.Commit()

	// check that calling PruneRefreshCandidates when there is nothing to do is fine.
	c.Assert(snapstate.PruneRefreshCandidates(st, "some-snap"), IsNil)

	candidates := map[string]*snapstate.RefreshCandidate{
		"snap-a": {
			SnapSetup: snapstate.SnapSetup{
				Type: "app",
				SideInfo: &snap.SideInfo{
					RealName: "snap-a",
					Revision: snap.R(1),
				},
			},
		},
		"snap-b": {
			SnapSetup: snapstate.SnapSetup{
				Type: "app",
				SideInfo: &snap.SideInfo{
					RealName: "snap-b",
					Revision: snap.R(1),
				},
			},
		},
		"snap-c": {
			SnapSetup: snapstate.SnapSetup{
				Type: "app",
				SideInfo: &snap.SideInfo{
					RealName: "snap-c",
					Revision: snap.R(1),
				},
			},
		},
	}
	st.Set("refresh-candidates", candidates)

	c.Assert(snapstate.PruneRefreshCandidates(st, "snap-a"), IsNil)

	var candidates2 map[string]*snapstate.RefreshCandidate
	c.Assert(st.Get("refresh-candidates", &candidates2), IsNil)
	_, ok := candidates2["snap-a"]
	c.Check(ok, Equals, false)
	_, ok = candidates2["snap-b"]
	c.Check(ok, Equals, true)
	_, ok = candidates2["snap-c"]
	c.Check(ok, Equals, true)

	var candidates3 map[string]*snapstate.RefreshCandidate
	c.Assert(snapstate.PruneRefreshCandidates(st, "snap-b"), IsNil)
	c.Assert(st.Get("refresh-candidates", &candidates3), IsNil)
	_, ok = candidates3["snap-a"]
	c.Check(ok, Equals, false)
	_, ok = candidates3["snap-b"]
	c.Check(ok, Equals, false)
	_, ok = candidates3["snap-c"]
	c.Check(ok, Equals, true)
}

func (s *refreshHintsTestSuite) TestPruneRefreshCandidatesIncorrectFormat(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// bad format - an array
	candidates := []*snapstate.RefreshCandidate{{
		SnapSetup: snapstate.SnapSetup{Type: "app", SideInfo: &snap.SideInfo{RealName: "snap-a", Revision: snap.R(1)}},
	}}
	st.Set("refresh-candidates", candidates)

	// it doesn't fail as long as experimental.gate-auto-refresh-hook is not enabled
	c.Assert(snapstate.PruneRefreshCandidates(st, "snap-a"), IsNil)
	var data interface{}
	// and refresh-candidates has been removed from the state
	c.Check(st.Get("refresh-candidates", data), Equals, state.ErrNoState)
}

func (s *refreshHintsTestSuite) TestRefreshHintsNotApplicableWrongArch(c *C) {
	s.state.Lock()
	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", Revision: snap.R(1), SnapID: "other-snap-id"},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})
	s.state.Unlock()

	s.store.refreshedSnaps = []*snap.Info{{
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			RealName: "some-snap",
			Revision: snap.R(1),
		},
	}, {
		Architectures: []string{"somearch"},
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			RealName: "other-snap",
			Revision: snap.R(2),
		},
	}}

	rh := snapstate.NewRefreshHints(s.state)
	c.Assert(rh.Ensure(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	var candidates map[string]*snapstate.RefreshCandidate
	c.Assert(s.state.Get("refresh-candidates", &candidates), IsNil)
	c.Assert(candidates, HasLen, 1)
	c.Check(candidates["some-snap"], NotNil)
}

const otherSnapYaml = `name: other-snap
version: 1.0
epoch: 1
type: app
`

func (s *refreshHintsTestSuite) TestRefreshHintsNotApplicableWrongEpoch(c *C) {
	s.state.Lock()

	si := &snap.SideInfo{RealName: "other-snap", Revision: snap.R(1), SnapID: "other-snap-id"}
	snaptest.MockSnap(c, otherSnapYaml, si)
	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		SnapType: "app",
	})
	s.state.Unlock()

	s.store.refreshedSnaps = []*snap.Info{{
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			RealName: "some-snap",
			Revision: snap.R(1),
		},
	}, {
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			RealName: "other-snap",
			Revision: snap.R(2),
		},
		Epoch: snap.Epoch{Read: []uint32{2}, Write: []uint32{2}},
	}}

	rh := snapstate.NewRefreshHints(s.state)
	c.Assert(rh.Ensure(), IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	var candidates map[string]*snapstate.RefreshCandidate
	c.Assert(s.state.Get("refresh-candidates", &candidates), IsNil)
	c.Assert(candidates, HasLen, 1)
	// other-snap ignored due to epoch
	c.Check(candidates["some-snap"], NotNil)
}
