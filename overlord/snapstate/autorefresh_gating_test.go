// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2022 Canonical Ltd
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
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

type autoRefreshGatingStore struct {
	*fakeStore
	refreshedSnaps []*snap.Info
}

type autorefreshGatingSuite struct {
	testutil.BaseTest
	state *state.State
	repo  *interfaces.Repository
	store *autoRefreshGatingStore
}

var _ = Suite(&autorefreshGatingSuite{})

func (s *autorefreshGatingSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() {
		dirs.SetRootDir("/")
	})
	s.state = state.New(nil)

	s.repo = interfaces.NewRepository()
	for _, iface := range builtin.Interfaces() {
		c.Assert(s.repo.AddInterface(iface), IsNil)
	}

	s.state.Lock()
	defer s.state.Unlock()
	ifacerepo.Replace(s.state, s.repo)

	s.store = &autoRefreshGatingStore{fakeStore: &fakeStore{}}
	snapstate.ReplaceStore(s.state, s.store)
	s.state.Set("refresh-privacy-key", "privacy-key")

	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		return snapasserts.NewValidationSets(), nil
	})
	s.AddCleanup(restore)
}

func (r *autoRefreshGatingStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	if assertQuery != nil {
		panic("no assertion query support")
	}
	if len(currentSnaps) != len(actions) || len(currentSnaps) == 0 {
		panic("expected in test one action for each current snaps, and at least one snap")
	}
	for _, a := range actions {
		if a.Action != "refresh" {
			panic("expected refresh actions")
		}
	}

	res := []store.SnapActionResult{}
	for _, rs := range r.refreshedSnaps {
		res = append(res, store.SnapActionResult{Info: rs})
	}

	return res, nil, nil
}

func mockInstalledSnap(c *C, st *state.State, snapYaml string, hasHook bool) *snap.Info {
	snapInfo := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{
		Revision: snap.R(1),
	})

	snapName := snapInfo.SnapName()
	si := &snap.SideInfo{RealName: snapName, SnapID: snapName + "-id", Revision: snap.R(1)}
	snapstate.Set(st, snapName, &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: string(snapInfo.Type()),
	})

	if hasHook {
		c.Assert(os.MkdirAll(snapInfo.HooksDir(), 0775), IsNil)
		err := os.WriteFile(filepath.Join(snapInfo.HooksDir(), "gate-auto-refresh"), nil, 0755)
		c.Assert(err, IsNil)
	}
	return snapInfo
}

func mockLastRefreshed(c *C, st *state.State, refreshedTime string, snaps ...string) {
	refreshed, err := time.Parse(time.RFC3339, refreshedTime)
	c.Assert(err, IsNil)
	for _, snapName := range snaps {
		var snapst snapstate.SnapState
		c.Assert(snapstate.Get(st, snapName, &snapst), IsNil)
		snapst.LastRefreshTime = &refreshed
		snapstate.Set(st, snapName, &snapst)
	}
}

const baseSnapAyaml = `name: base-snap-a
type: base
`

const snapAyaml = `name: snap-a
type: app
base: base-snap-a
`

const baseSnapByaml = `name: base-snap-b
type: base
`

const snapByaml = `name: snap-b
type: app
base: base-snap-b
version: 1
`

const snapBByaml = `name: snap-bb
type: app
base: base-snap-b
version: 1
`

const kernelYaml = `name: kernel
type: kernel
version: 1
`

const gadget1Yaml = `name: gadget
type: gadget
version: 1
`

const snapCyaml = `name: snap-c
type: app
version: 1
`

const snapDyaml = `name: snap-d
type: app
version: 1
slots:
    slot: desktop
`

const snapEyaml = `name: snap-e
type: app
version: 1
base: other-base
plugs:
    plug: desktop
`

const snapFyaml = `name: snap-f
type: app
version: 1
plugs:
    plug: desktop
`

const snapGyaml = `name: snap-g
type: app
version: 1
base: other-base
plugs:
    desktop:
    mir:
`

const coreYaml = `name: core
type: os
version: 1
slots:
    desktop:
    mir:
`

const core18Yaml = `name: core18
type: os
version: 1
`

const snapdYaml = `name: snapd
version: 1
type: snapd
slots:
    desktop:
`

const someSnap = `name: some-snap
type: app
`

const someOtherSnap = `name: some-other-snap
type: app
`

func (s *autorefreshGatingSuite) TestHoldDurationLeft(c *C) {
	now, err := time.Parse(time.RFC3339, "2021-06-03T10:00:00Z")
	c.Assert(err, IsNil)
	maxPostponement := time.Hour * 24 * 90

	for i, tc := range []struct {
		lastRefresh, firstHeld string
		maxDuration            string
		expected               string
	}{
		{
			"2021-05-03T10:00:00Z", // last refreshed (1 month ago)
			"2021-06-03T10:00:00Z", // first held now
			"48h",                  // max duration
			"48h",                  // expected
		},
		{
			"2021-05-03T10:00:00Z", // last refreshed (1 month ago)
			"2021-06-02T10:00:00Z", // first held (1 day ago)
			"48h",                  // max duration
			"24h",                  // expected
		},
		{
			"2021-05-03T10:00:00Z", // last refreshed (1 month ago)
			"2021-06-01T10:00:00Z", // first held (2 days ago)
			"48h",                  // max duration
			"00h",                  // expected
		},
		{
			"2021-03-08T10:00:00Z", // last refreshed (almost 3 months ago)
			"2021-06-01T10:00:00Z", // first held
			"2160h",                // max duration (90 days)
			"72h",                  // expected
		},
		{
			"2021-03-04T10:00:00Z", // last refreshed
			"2021-06-01T10:00:00Z", // first held (2 days ago)
			"2160h",                // max duration (90 days)
			"-24h",                 // expected (refresh is 1 day overdue)
		},
		{
			"2021-06-01T10:00:00Z", // last refreshed (2 days ago)
			"2021-06-03T10:00:00Z", // first held now
			"2160h",                // max duration (90 days)
			"2112h",                // expected (max minus 2 days)
		},
	} {
		lastRefresh, err := time.Parse(time.RFC3339, tc.lastRefresh)
		c.Assert(err, IsNil)
		firstHeld, err := time.Parse(time.RFC3339, tc.firstHeld)
		c.Assert(err, IsNil)
		maxDuration, err := time.ParseDuration(tc.maxDuration)
		c.Assert(err, IsNil)
		expected, err := time.ParseDuration(tc.expected)
		c.Assert(err, IsNil)

		left := snapstate.HoldDurationLeft(now, lastRefresh, firstHeld, maxDuration, maxPostponement)
		c.Check(left, Equals, expected, Commentf("case #%d", i))
	}
}

func (s *autorefreshGatingSuite) TestLastRefreshedHelper(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	inf := mockInstalledSnap(c, st, snapAyaml, false)
	stat, err := os.Stat(inf.MountFile())
	c.Assert(err, IsNil)

	refreshed, err := snapstate.LastRefreshed(st, "snap-a")
	c.Assert(err, IsNil)
	c.Check(refreshed, DeepEquals, stat.ModTime())

	t, err := time.Parse(time.RFC3339, "2021-01-01T10:00:00Z")
	c.Assert(err, IsNil)

	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(st, "snap-a", &snapst), IsNil)
	snapst.LastRefreshTime = &t
	snapstate.Set(st, "snap-a", &snapst)

	refreshed, err = snapstate.LastRefreshed(st, "snap-a")
	c.Assert(err, IsNil)
	c.Check(refreshed, DeepEquals, t)
}

func (s *autorefreshGatingSuite) TestHoldRefreshHelper(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	restore := snapstate.MockTimeNow(func() time.Time {
		t, err := time.Parse(time.RFC3339, "2021-05-10T10:00:00Z")
		c.Assert(err, IsNil)
		return t
	})
	defer restore()

	mockInstalledSnap(c, st, snapAyaml, false)
	mockInstalledSnap(c, st, snapByaml, false)
	mockInstalledSnap(c, st, snapCyaml, false)
	mockInstalledSnap(c, st, snapDyaml, false)
	mockInstalledSnap(c, st, snapEyaml, false)
	mockInstalledSnap(c, st, snapFyaml, false)

	mockLastRefreshed(c, st, "2021-05-09T10:00:00Z", "snap-a", "snap-b", "snap-c", "snap-d", "snap-e", "snap-f")

	_, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", 0, "snap-b", "snap-c")
	c.Assert(err, IsNil)
	// this could be merged with the above HoldRefresh call, but it's fine if
	// done separately too.
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", 0, "snap-e")
	c.Assert(err, IsNil)
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-d", 0, "snap-e")
	c.Assert(err, IsNil)
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-f", 0, "snap-f")
	c.Assert(err, IsNil)

	var gating map[string]map[string]*snapstate.HoldState
	c.Assert(st.Get("snaps-hold", &gating), IsNil)
	c.Check(gating, DeepEquals, map[string]map[string]*snapstate.HoldState{
		"snap-b": {
			// holding of other snaps for maxOtherHoldDuration (48h)
			"snap-a": snapstate.MockHoldState("2021-05-10T10:00:00Z", "2021-05-12T10:00:00Z"),
		},
		"snap-c": {
			"snap-a": snapstate.MockHoldState("2021-05-10T10:00:00Z", "2021-05-12T10:00:00Z"),
		},
		"snap-e": {
			"snap-a": snapstate.MockHoldState("2021-05-10T10:00:00Z", "2021-05-12T10:00:00Z"),
			"snap-d": snapstate.MockHoldState("2021-05-10T10:00:00Z", "2021-05-12T10:00:00Z"),
		},
		"snap-f": {
			// holding self set for maxPostponement minus 1 day due to last refresh.
			"snap-f": snapstate.MockHoldState("2021-05-10T10:00:00Z", "2021-08-07T10:00:00Z"),
		},
	})
}

func (s *autorefreshGatingSuite) TestHoldRefreshReturnsMinimumHoldTime(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	now := "2021-05-10T10:00:00Z"
	restore := snapstate.MockTimeNow(func() time.Time {
		t, err := time.Parse(time.RFC3339, now)
		c.Assert(err, IsNil)
		return t
	})
	defer restore()

	mockInstalledSnap(c, st, snapAyaml, false)
	mockInstalledSnap(c, st, snapByaml, false)
	mockInstalledSnap(c, st, snapCyaml, false)
	mockInstalledSnap(c, st, snapDyaml, false)
	mockInstalledSnap(c, st, snapEyaml, false)
	mockInstalledSnap(c, st, snapFyaml, false)

	mockLastRefreshed(c, st, "2021-05-09T10:00:00Z", "snap-a", "snap-b", "snap-c", "snap-d", "snap-e", "snap-f")

	// only holding self: max postponement - buffer time returned
	rem, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", 0, "snap-a")
	c.Assert(err, IsNil)
	c.Check(rem.String(), Equals, "2136h0m0s")

	// holding self and some other snaps, max hold time of holding other snaps returned.
	rem, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", 0, "snap-a", "snap-b", "snap-c", "snap-e")
	c.Assert(err, IsNil)
	c.Check(rem.String(), Equals, "48h0m0s")

	// advance time
	now = "2021-05-11T12:00:00Z"
	rem, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", 0, "snap-a", "snap-b", "snap-c", "snap-e")
	c.Assert(err, IsNil)
	// it's now less due to previous hold
	c.Check(rem.String(), Equals, "22h0m0s")

	var gating map[string]map[string]*snapstate.HoldState
	c.Assert(st.Get("snaps-hold", &gating), IsNil)
	c.Check(gating, DeepEquals, map[string]map[string]*snapstate.HoldState{
		"snap-b": {
			// holding of other snaps for maxOtherHoldDuration (48h)
			"snap-a": snapstate.MockHoldState("2021-05-10T10:00:00Z", "2021-05-12T10:00:00Z"),
		},
		"snap-c": {
			"snap-a": snapstate.MockHoldState("2021-05-10T10:00:00Z", "2021-05-12T10:00:00Z"),
		},
		"snap-e": {
			"snap-a": snapstate.MockHoldState("2021-05-10T10:00:00Z", "2021-05-12T10:00:00Z"),
		},
		"snap-a": {
			// holding self set for maxPostponement minus 1 day due to last refresh.
			"snap-a": snapstate.MockHoldState("2021-05-10T10:00:00Z", "2021-08-07T10:00:00Z"),
		},
	})
}

func (s *autorefreshGatingSuite) TestHoldRefreshHelperMultipleTimes(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	lastRefreshed := "2021-05-09T10:00:00Z"
	now := "2021-05-10T10:00:00Z"
	restore := snapstate.MockTimeNow(func() time.Time {
		t, err := time.Parse(time.RFC3339, now)
		c.Assert(err, IsNil)
		return t
	})
	defer restore()

	mockInstalledSnap(c, st, snapAyaml, false)
	mockInstalledSnap(c, st, snapByaml, false)
	// snap-a was last refreshed yesterday
	mockLastRefreshed(c, st, lastRefreshed, "snap-a")

	// hold it for just a bit (10h) initially
	hold := time.Hour * 10
	rem, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-b", hold, "snap-a")
	c.Assert(err, IsNil)
	c.Check(rem.String(), Equals, "48h0m0s")
	var gating map[string]map[string]*snapstate.HoldState
	c.Assert(st.Get("snaps-hold", &gating), IsNil)
	c.Check(gating, DeepEquals, map[string]map[string]*snapstate.HoldState{
		"snap-a": {
			"snap-b": snapstate.MockHoldState(now, "2021-05-10T20:00:00Z"),
		},
	})

	// holding for a shorter time is fine too
	hold = time.Hour * 5
	rem, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-b", hold, "snap-a")
	c.Assert(err, IsNil)
	c.Check(rem.String(), Equals, "48h0m0s")
	c.Assert(st.Get("snaps-hold", &gating), IsNil)
	c.Check(gating, DeepEquals, map[string]map[string]*snapstate.HoldState{
		"snap-a": {
			"snap-b": snapstate.MockHoldState(now, "2021-05-10T15:00:00Z"),
		},
	})

	oldNow := now

	// a refresh on next day
	now = "2021-05-11T08:00:00Z"

	// default hold time requested
	hold = 0
	rem, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-b", hold, "snap-a")
	c.Assert(err, IsNil)
	c.Check(rem.String(), Equals, "26h0m0s")
	c.Assert(st.Get("snaps-hold", &gating), IsNil)
	c.Check(gating, DeepEquals, map[string]map[string]*snapstate.HoldState{
		"snap-a": {
			// maximum for holding other snaps, but taking into consideration
			// firstHeld time = "2021-05-10T10:00:00".
			"snap-b": snapstate.MockHoldState(oldNow, "2021-05-12T10:00:00Z"),
		},
	})
}

func (s *autorefreshGatingSuite) TestHoldRefreshHelperCloseToMaxPostponement(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	lastRefreshedStr := "2021-01-01T10:00:00Z"
	lastRefreshed, err := time.Parse(time.RFC3339, lastRefreshedStr)
	c.Assert(err, IsNil)
	// we are 1 day before maxPostponent
	now := lastRefreshed.Add(89 * time.Hour * 24)

	restore := snapstate.MockTimeNow(func() time.Time { return now })
	defer restore()

	mockInstalledSnap(c, st, snapAyaml, false)
	mockInstalledSnap(c, st, snapByaml, false)
	mockLastRefreshed(c, st, lastRefreshedStr, "snap-a")

	// request default hold time
	var hold time.Duration
	rem, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-b", hold, "snap-a")
	c.Assert(err, IsNil)
	c.Check(rem.String(), Equals, "24h0m0s")

	var gating map[string]map[string]*snapstate.HoldState
	c.Assert(st.Get("snaps-hold", &gating), IsNil)
	c.Assert(gating, HasLen, 1)
	c.Check(gating["snap-a"]["snap-b"].HoldUntil.String(), DeepEquals, lastRefreshed.Add(90*time.Hour*24).String())
}

func (s *autorefreshGatingSuite) TestHoldRefreshExplicitHoldTime(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	now := "2021-05-10T10:00:00Z"
	restore := snapstate.MockTimeNow(func() time.Time {
		t, err := time.Parse(time.RFC3339, now)
		c.Assert(err, IsNil)
		return t
	})
	defer restore()

	mockInstalledSnap(c, st, snapAyaml, false)
	mockInstalledSnap(c, st, snapByaml, false)

	hold := time.Hour * 24 * 3
	// holding self for 3 days
	_, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", hold, "snap-a")
	c.Assert(err, IsNil)

	// snap-b holds snap-a for 1 day
	hold = time.Hour * 24
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-b", hold, "snap-a")
	c.Assert(err, IsNil)

	var gating map[string]map[string]*snapstate.HoldState
	c.Assert(st.Get("snaps-hold", &gating), IsNil)
	c.Check(gating, DeepEquals, map[string]map[string]*snapstate.HoldState{
		"snap-a": {
			"snap-a": snapstate.MockHoldState(now, "2021-05-13T10:00:00Z"),
			"snap-b": snapstate.MockHoldState(now, "2021-05-11T10:00:00Z"),
		},
	})
}

func (s *autorefreshGatingSuite) TestHoldRefreshHelperErrors(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	now := "2021-05-10T10:00:00Z"
	restore := snapstate.MockTimeNow(func() time.Time {
		t, err := time.Parse(time.RFC3339, now)
		c.Assert(err, IsNil)
		return t
	})
	defer restore()

	mockInstalledSnap(c, st, snapAyaml, false)
	mockInstalledSnap(c, st, snapByaml, false)
	// snap-b was refreshed a few days ago
	mockLastRefreshed(c, st, "2021-05-01T10:00:00Z", "snap-b")

	// holding itself
	hold := time.Hour * 24 * 96
	_, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", hold, "snap-a")
	c.Assert(err, ErrorMatches, `cannot hold some snaps:\n - requested holding duration for snap "snap-a" of 2304h0m0s by snap "snap-a" exceeds maximum holding time`)

	// holding other snap
	hold = time.Hour * 49
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", hold, "snap-b")
	c.Check(err, ErrorMatches, `cannot hold some snaps:\n - requested holding duration for snap "snap-b" of 49h0m0s by snap "snap-a" exceeds maximum holding time`)
	herr, ok := err.(*snapstate.HoldError)
	c.Assert(ok, Equals, true)
	c.Check(herr.SnapsInError, DeepEquals, map[string]snapstate.HoldDurationError{
		"snap-b": {
			Err:          fmt.Errorf(`requested holding duration for snap "snap-b" of 49h0m0s by snap "snap-a" exceeds maximum holding time`),
			DurationLeft: 48 * time.Hour,
		},
	})

	// hold for maximum allowed for other snaps
	hold = time.Hour * 48
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", hold, "snap-b")
	c.Assert(err, IsNil)
	// 2 days passed since it was first held
	now = "2021-05-12T10:00:00Z"
	hold = time.Minute * 2
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", hold, "snap-b")
	c.Assert(err, ErrorMatches, `cannot hold some snaps:\n - snap "snap-a" cannot hold snap "snap-b" anymore, maximum refresh postponement exceeded`)

	// refreshed long time ago (> maxPostponement)
	mockLastRefreshed(c, st, "2021-01-01T10:00:00Z", "snap-b")
	hold = time.Hour * 2
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-b", hold, "snap-b")
	c.Assert(err, ErrorMatches, `cannot hold some snaps:\n - snap "snap-b" cannot hold snap "snap-b" anymore, maximum refresh postponement exceeded`)
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-b", 0, "snap-b")
	c.Assert(err, ErrorMatches, `cannot hold some snaps:\n - snap "snap-b" cannot hold snap "snap-b" anymore, maximum refresh postponement exceeded`)
}

func (s *autorefreshGatingSuite) TestHoldAndProceedWithRefreshHelper(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, st, snapAyaml, false)
	mockInstalledSnap(c, st, snapByaml, false)
	mockInstalledSnap(c, st, snapCyaml, false)
	mockInstalledSnap(c, st, snapDyaml, false)
	mockInstalledSnap(c, st, snapEyaml, false)
	mockInstalledSnap(c, st, snapFyaml, false)

	mockLastRefreshed(c, st, "2021-05-09T10:00:00Z", "snap-b", "snap-c", "snap-d", "snap-e", "snap-f")

	restore := snapstate.MockTimeNow(func() time.Time {
		t, err := time.Parse(time.RFC3339, "2021-05-10T10:00:00Z")
		c.Assert(err, IsNil)
		return t
	})
	defer restore()

	// nothing is held initially
	held, err := snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(held, IsNil)

	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", 0, "snap-b", "snap-c")
	c.Assert(err, IsNil)
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-d", 0, "snap-c")
	c.Assert(err, IsNil)
	// holding self
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-d", time.Hour*24*4, "snap-d")
	c.Assert(err, IsNil)
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-e", 0, "snap-e", "snap-f")
	c.Assert(err, IsNil)

	held, err = snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(held, testutil.DeepUnsortedMatches, map[string][]string{"snap-b": {"snap-a"}, "snap-c": {"snap-a", "snap-d"}, "snap-d": {"snap-d"}, "snap-e": {"snap-e"}, "snap-f": {"snap-e"}})

	// check that specifying a subset of snaps held by snap-e only unblocks those snaps
	c.Assert(snapstate.ProceedWithRefresh(st, "snap-e", []string{"snap-f"}), IsNil)

	held, err = snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(held, testutil.DeepUnsortedMatches, map[string][]string{"snap-b": {"snap-a"}, "snap-c": {"snap-a", "snap-d"}, "snap-d": {"snap-d"}, "snap-e": {"snap-e"}})
	// clear the rest of snap-e's held snaps
	c.Assert(snapstate.ProceedWithRefresh(st, "snap-e", nil), IsNil)

	c.Assert(snapstate.ProceedWithRefresh(st, "snap-a", nil), IsNil)
	held, err = snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(held, DeepEquals, map[string][]string{"snap-c": {"snap-d"}, "snap-d": {"snap-d"}})

	c.Assert(snapstate.ProceedWithRefresh(st, "snap-d", nil), IsNil)
	held, err = snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(held, IsNil)
}

// Test that if all snaps cannot be held anymore, we don't hold only some of them
// e.g. is a snap and its base snap have updates and the snap wants to hold (itself
// and the base) but the base cannot be held, it doesn't make sense to refresh the
// base but hold the affected snap.
func (s *autorefreshGatingSuite) TestDontHoldSomeSnapsIfSomeFail(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// snap-b and snap-bb have base-snap-b base
	mockInstalledSnap(c, st, snapByaml, useHook)
	mockInstalledSnap(c, st, snapBByaml, useHook)
	mockInstalledSnap(c, st, baseSnapByaml, noHook)

	mockInstalledSnap(c, st, snapCyaml, useHook)
	mockInstalledSnap(c, st, snapDyaml, useHook)

	now := "2021-05-01T10:00:00Z"
	restore := snapstate.MockTimeNow(func() time.Time {
		t, err := time.Parse(time.RFC3339, now)
		c.Assert(err, IsNil)
		return t
	})
	defer restore()

	// snap-b, base-snap-b get refreshed and affect snap-b (gating snap)
	_, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-b", 0, "snap-b", "base-snap-b")
	c.Assert(err, IsNil)
	// unrealted snap-d gets refreshed and holds itself
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-d", 0, "snap-d")
	c.Assert(err, IsNil)

	// advance time by 49h
	now = "2021-05-03T11:00:00Z"
	// snap-b, base-snap-b and snap-c get refreshed and snap-a (gating snap) wants to hold them
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-b", 0, "snap-b", "base-snap-b", "snap-c")
	c.Assert(err, ErrorMatches, `cannot hold some snaps:\n - snap "snap-b" cannot hold snap "base-snap-b" anymore, maximum refresh postponement exceeded`)
	// snap-bb (gating snap) wants to hold base-snap-b as well and succeeds since it didn't exceed its holding time yet
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-bb", 0, "base-snap-b")
	c.Assert(err, IsNil)

	held, err := snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	// note, snap-b couldn't hold base-snap-b anymore so we didn't hold snap-b
	// and snap-c. base-snap-b was held by snap-bb.
	c.Check(held, DeepEquals, map[string][]string{
		"snap-d":      {"snap-d"},
		"base-snap-b": {"snap-bb"},
	})
}

func (s *autorefreshGatingSuite) TestPruneGatingHelper(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	restore := snapstate.MockTimeNow(func() time.Time {
		t, err := time.Parse(time.RFC3339, "2021-05-10T10:00:00Z")
		c.Assert(err, IsNil)
		return t
	})
	defer restore()

	mockInstalledSnap(c, st, snapAyaml, false)
	mockInstalledSnap(c, st, snapByaml, false)
	mockInstalledSnap(c, st, snapCyaml, false)
	mockInstalledSnap(c, st, snapDyaml, false)

	_, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", 0, "snap-b", "snap-c")
	c.Assert(err, IsNil)
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-d", 0, "snap-d", "snap-c")
	c.Assert(err, IsNil)
	// validity
	held, err := snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(held, testutil.DeepUnsortedMatches, map[string][]string{"snap-c": {"snap-a", "snap-d"}, "snap-b": {"snap-a"}, "snap-d": {"snap-d"}})

	candidates := map[string]*snapstate.RefreshCandidate{"snap-c": {}}

	// only snap-c has a refresh candidate, snap-b and snap-d should be forgotten.
	c.Assert(snapstate.PruneGating(st, candidates), IsNil)
	var gating map[string]map[string]*snapstate.HoldState
	c.Assert(st.Get("snaps-hold", &gating), IsNil)
	c.Check(gating, DeepEquals, map[string]map[string]*snapstate.HoldState{
		"snap-c": {
			"snap-a": snapstate.MockHoldState("2021-05-10T10:00:00Z", "2021-05-12T10:00:00Z"),
			"snap-d": snapstate.MockHoldState("2021-05-10T10:00:00Z", "2021-05-12T10:00:00Z"),
		},
	})
	held, err = snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(held, testutil.DeepUnsortedMatches, map[string][]string{"snap-c": {"snap-a", "snap-d"}})
}

func (s *autorefreshGatingSuite) TestPruneGatingHelperWithSystemHeld(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	restore := snapstate.MockTimeNow(func() time.Time {
		t, err := time.Parse(time.RFC3339, "2021-05-10T10:00:00Z")
		c.Assert(err, IsNil)
		return t
	})
	defer restore()

	mockInstalledSnap(c, st, snapAyaml, false)
	mockInstalledSnap(c, st, snapByaml, false)
	mockInstalledSnap(c, st, snapCyaml, false)
	mockInstalledSnap(c, st, snapDyaml, false)

	_, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", 0, "snap-b", "snap-c")
	c.Assert(err, IsNil)
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-d", 0, "snap-d", "snap-c")
	c.Assert(err, IsNil)
	// snap-b is also held by system forever
	err = snapstate.HoldRefreshesBySystem(st, snapstate.HoldGeneral, "forever", []string{"snap-b"})
	c.Assert(err, IsNil)
	// validity
	held, err := snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(held, testutil.DeepUnsortedMatches, map[string][]string{"snap-c": {"snap-a", "snap-d"}, "snap-b": {"snap-a", "system"}, "snap-d": {"snap-d"}})

	candidates := map[string]*snapstate.RefreshCandidate{"snap-c": {}}

	// only snap-c has a refresh candidate, snap-b and snap-d should be forgotten.
	c.Assert(snapstate.PruneGating(st, candidates), IsNil)
	var gating map[string]map[string]*snapstate.HoldState
	c.Assert(st.Get("snaps-hold", &gating), IsNil)
	sysHoldState := snapstate.MockHoldState("2021-05-10T10:00:00Z", "forever")
	sysHoldState.Level = snapstate.HoldGeneral
	c.Check(gating, DeepEquals, map[string]map[string]*snapstate.HoldState{
		"snap-b": {
			"system": sysHoldState,
		},
		"snap-c": {
			"snap-a": snapstate.MockHoldState("2021-05-10T10:00:00Z", "2021-05-12T10:00:00Z"),
			"snap-d": snapstate.MockHoldState("2021-05-10T10:00:00Z", "2021-05-12T10:00:00Z"),
		},
	})
	held, err = snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(held, testutil.DeepUnsortedMatches, map[string][]string{"snap-c": {"snap-d", "snap-a"}, "snap-b": {"system"}})
}

func (s *autorefreshGatingSuite) TestPruneGatingHelperNoGating(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	restore := snapstate.MockTimeNow(func() time.Time {
		t, err := time.Parse(time.RFC3339, "2021-05-10T10:00:00Z")
		c.Assert(err, IsNil)
		return t
	})
	defer restore()

	mockInstalledSnap(c, st, snapAyaml, false)

	held, err := snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(held, HasLen, 0)

	snapstate.MockTimeNow(func() time.Time {
		c.Fatalf("not expected")
		return time.Time{}
	})

	candidates := map[string]*snapstate.RefreshCandidate{"snap-a": {}}
	c.Assert(snapstate.PruneGating(st, candidates), IsNil)
	held, err = snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(held, HasLen, 0)
}

func (s *autorefreshGatingSuite) TestResetGatingForRefreshedHelper(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	restore := snapstate.MockTimeNow(func() time.Time {
		t, err := time.Parse(time.RFC3339, "2021-05-10T10:00:00Z")
		c.Assert(err, IsNil)
		return t
	})
	defer restore()

	mockInstalledSnap(c, st, snapAyaml, false)
	mockInstalledSnap(c, st, snapByaml, false)
	mockInstalledSnap(c, st, snapCyaml, false)
	mockInstalledSnap(c, st, snapDyaml, false)

	_, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", 0, "snap-b", "snap-c")
	c.Assert(err, IsNil)
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-d", 0, "snap-d", "snap-c")
	c.Assert(err, IsNil)

	c.Assert(snapstate.ResetGatingForRefreshed(st, "snap-b", "snap-c"), IsNil)
	var gating map[string]map[string]*snapstate.HoldState
	c.Assert(st.Get("snaps-hold", &gating), IsNil)
	c.Check(gating, DeepEquals, map[string]map[string]*snapstate.HoldState{
		"snap-d": {
			// holding self set for maxPostponement (95 days - buffer = 90 days)
			"snap-d": snapstate.MockHoldState("2021-05-10T10:00:00Z", "2021-08-08T10:00:00Z"),
		},
	})

	held, err := snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(held, DeepEquals, map[string][]string{"snap-d": {"snap-d"}})
}

func (s *autorefreshGatingSuite) TestResetGatingForRefreshedHelperWithSystemHeld(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	restore := snapstate.MockTimeNow(func() time.Time {
		t, err := time.Parse(time.RFC3339, "2021-05-10T10:00:00Z")
		c.Assert(err, IsNil)
		return t
	})
	defer restore()

	mockInstalledSnap(c, st, snapAyaml, false)
	mockInstalledSnap(c, st, snapByaml, false)
	mockInstalledSnap(c, st, snapCyaml, false)
	mockInstalledSnap(c, st, snapDyaml, false)

	_, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", 0, "snap-b", "snap-c")
	c.Assert(err, IsNil)
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-d", 0, "snap-d", "snap-c")
	c.Assert(err, IsNil)
	// snap-b is also held by system forever
	err = snapstate.HoldRefreshesBySystem(st, snapstate.HoldGeneral, "forever", []string{"snap-b"})
	c.Assert(err, IsNil)

	c.Assert(snapstate.ResetGatingForRefreshed(st, "snap-b", "snap-c"), IsNil)
	var gating map[string]map[string]*snapstate.HoldState
	c.Assert(st.Get("snaps-hold", &gating), IsNil)
	sysHoldState := snapstate.MockHoldState("2021-05-10T10:00:00Z", "forever")
	sysHoldState.Level = snapstate.HoldGeneral
	c.Check(gating, DeepEquals, map[string]map[string]*snapstate.HoldState{
		"snap-b": {
			"system": sysHoldState,
		},
		"snap-d": {
			// holding self set for maxPostponement (95 days - buffer = 90 days)
			"snap-d": snapstate.MockHoldState("2021-05-10T10:00:00Z", "2021-08-08T10:00:00Z"),
		},
	})

	held, err := snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(held, DeepEquals, map[string][]string{"snap-b": {"system"}, "snap-d": {"snap-d"}})
}

func (s *autorefreshGatingSuite) TestPruneSnapsHold(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, st, snapAyaml, false)
	mockInstalledSnap(c, st, snapByaml, false)
	mockInstalledSnap(c, st, snapCyaml, false)
	mockInstalledSnap(c, st, snapDyaml, false)

	// snap-a is holding itself and 3 other snaps
	_, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", 0, "snap-a", "snap-b", "snap-c", "snap-d")
	c.Assert(err, IsNil)
	// in addition, snap-c is held by snap-d.
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-d", 0, "snap-c")
	c.Assert(err, IsNil)

	// validity check
	held, err := snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(held, testutil.DeepUnsortedMatches, map[string][]string{
		"snap-a": {"snap-a"},
		"snap-b": {"snap-a"},
		"snap-c": {"snap-a", "snap-d"},
		"snap-d": {"snap-a"},
	})

	c.Check(snapstate.PruneSnapsHold(st, "snap-a"), IsNil)

	// after pruning snap-a, snap-c is still held.
	held, err = snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(held, DeepEquals, map[string][]string{
		"snap-c": {"snap-d"},
	})
	var gating map[string]map[string]*snapstate.HoldState
	c.Assert(st.Get("snaps-hold", &gating), IsNil)
	c.Assert(gating, HasLen, 1)
	c.Check(gating["snap-c"], HasLen, 1)
	c.Check(gating["snap-c"]["snap-d"], NotNil)
}

const useHook = true
const noHook = false

func checkGatingTask(c *C, task *state.Task, expected map[string]*snapstate.RefreshCandidate) {
	c.Assert(task.Kind(), Equals, "conditional-auto-refresh")
	var snaps map[string]*snapstate.RefreshCandidate
	c.Assert(task.Get("snaps", &snaps), IsNil)
	c.Check(snaps, DeepEquals, expected)
}

func (s *autorefreshGatingSuite) TestAffectedByBase(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()
	mockInstalledSnap(c, s.state, snapAyaml, useHook)
	baseSnapA := mockInstalledSnap(c, s.state, baseSnapAyaml, noHook)
	// unrelated snaps
	snapB := mockInstalledSnap(c, s.state, snapByaml, useHook)
	mockInstalledSnap(c, s.state, baseSnapByaml, noHook)

	snapBAppSet, err := interfaces.NewSnapAppSet(snapB, nil)
	c.Assert(err, IsNil)

	c.Assert(s.repo.AddAppSet(snapBAppSet), IsNil)

	updates := []string{baseSnapA.InstanceName()}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-a": {
			Base: true,
			AffectingSnaps: map[string]bool{
				"base-snap-a": true,
			}}})
}

func (s *autorefreshGatingSuite) TestAffectedByCore(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()
	snapC := mockInstalledSnap(c, s.state, snapCyaml, useHook)
	core := mockInstalledSnap(c, s.state, coreYaml, noHook)
	snapB := mockInstalledSnap(c, s.state, snapByaml, useHook)

	coreAppSet, err := interfaces.NewSnapAppSet(core, nil)
	c.Assert(err, IsNil)
	snapBAppSet, err := interfaces.NewSnapAppSet(snapB, nil)
	c.Assert(err, IsNil)
	snapCAppSet, err := interfaces.NewSnapAppSet(snapC, nil)
	c.Assert(err, IsNil)

	c.Assert(s.repo.AddAppSet(coreAppSet), IsNil)
	c.Assert(s.repo.AddAppSet(snapBAppSet), IsNil)
	c.Assert(s.repo.AddAppSet(snapCAppSet), IsNil)

	updates := []string{core.InstanceName()}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-c": {
			Base: true,
			AffectingSnaps: map[string]bool{
				"core": true,
			}}})
}

func (s *autorefreshGatingSuite) TestAffectedByKernel(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()
	kernel := mockInstalledSnap(c, s.state, kernelYaml, noHook)
	mockInstalledSnap(c, s.state, snapCyaml, useHook)
	mockInstalledSnap(c, s.state, snapByaml, noHook)

	updates := []string{kernel.InstanceName()}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-c": {
			Restart: true,
			AffectingSnaps: map[string]bool{
				"kernel": true,
			}}})
}

func (s *autorefreshGatingSuite) TestAffectedBySelf(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()

	snapC := mockInstalledSnap(c, s.state, snapCyaml, useHook)
	updates := []string{snapC.InstanceName()}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-c": {
			AffectingSnaps: map[string]bool{
				"snap-c": true,
			}}})
}

func (s *autorefreshGatingSuite) TestAffectedByGadget(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()
	kernel := mockInstalledSnap(c, s.state, gadget1Yaml, noHook)
	mockInstalledSnap(c, s.state, snapCyaml, useHook)
	mockInstalledSnap(c, s.state, snapByaml, noHook)

	updates := []string{kernel.InstanceName()}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-c": {
			Restart: true,
			AffectingSnaps: map[string]bool{
				"gadget": true,
			}}})
}

func (s *autorefreshGatingSuite) TestAffectedBySlot(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()

	snapD := mockInstalledSnap(c, s.state, snapDyaml, noHook)
	snapE := mockInstalledSnap(c, s.state, snapEyaml, useHook)
	// unrelated snap
	snapF := mockInstalledSnap(c, s.state, snapFyaml, useHook)

	snapFAppSet, err := interfaces.NewSnapAppSet(snapF, nil)
	c.Assert(err, IsNil)
	snapDAppSet, err := interfaces.NewSnapAppSet(snapD, nil)
	c.Assert(err, IsNil)
	snapEAppSet, err := interfaces.NewSnapAppSet(snapE, nil)
	c.Assert(err, IsNil)

	c.Assert(s.repo.AddAppSet(snapFAppSet), IsNil)
	c.Assert(s.repo.AddAppSet(snapDAppSet), IsNil)
	c.Assert(s.repo.AddAppSet(snapEAppSet), IsNil)
	cref := &interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "snap-e", Name: "plug"}, SlotRef: interfaces.SlotRef{Snap: "snap-d", Name: "slot"}}
	_, err = s.repo.Connect(cref, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	updates := []string{snapD.InstanceName()}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-e": {
			Restart: true,
			AffectingSnaps: map[string]bool{
				"snap-d": true,
			}}})
}

func (s *autorefreshGatingSuite) TestNotAffectedByCoreOrSnapdSlot(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()

	snapG := mockInstalledSnap(c, s.state, snapGyaml, useHook)
	core := mockInstalledSnap(c, s.state, coreYaml, noHook)
	snapB := mockInstalledSnap(c, s.state, snapByaml, useHook)

	snapGAppSet, err := interfaces.NewSnapAppSet(snapG, nil)
	c.Assert(err, IsNil)
	coreAppSet, err := interfaces.NewSnapAppSet(core, nil)
	c.Assert(err, IsNil)
	snapBAppSet, err := interfaces.NewSnapAppSet(snapB, nil)
	c.Assert(err, IsNil)

	c.Assert(s.repo.AddAppSet(snapGAppSet), IsNil)
	c.Assert(s.repo.AddAppSet(coreAppSet), IsNil)
	c.Assert(s.repo.AddAppSet(snapBAppSet), IsNil)

	cref := &interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "snap-g", Name: "mir"}, SlotRef: interfaces.SlotRef{Snap: "core", Name: "mir"}}
	_, err = s.repo.Connect(cref, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	updates := []string{core.InstanceName()}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, HasLen, 0)
}

func (s *autorefreshGatingSuite) TestNotAffectedByPlugWithMountBackend(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()

	snapD := mockInstalledSnap(c, s.state, snapDyaml, useHook)
	snapE := mockInstalledSnap(c, s.state, snapEyaml, noHook)
	// unrelated snap
	snapF := mockInstalledSnap(c, s.state, snapFyaml, useHook)

	snapFAppSet, err := interfaces.NewSnapAppSet(snapF, nil)
	c.Assert(err, IsNil)
	snapDAppSet, err := interfaces.NewSnapAppSet(snapD, nil)
	c.Assert(err, IsNil)
	snapEAppSet, err := interfaces.NewSnapAppSet(snapE, nil)
	c.Assert(err, IsNil)

	c.Assert(s.repo.AddAppSet(snapFAppSet), IsNil)
	c.Assert(s.repo.AddAppSet(snapDAppSet), IsNil)
	c.Assert(s.repo.AddAppSet(snapEAppSet), IsNil)
	cref := &interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "snap-e", Name: "plug"}, SlotRef: interfaces.SlotRef{Snap: "snap-d", Name: "slot"}}
	_, err = s.repo.Connect(cref, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	// snapE has a plug using mount backend and is refreshed, this doesn't affect slot of snap-d.
	updates := []string{snapE.InstanceName()}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, HasLen, 0)
}

func (s *autorefreshGatingSuite) TestAffectedByPlugWithMountBackendSnapdSlot(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()

	snapdSnap := mockInstalledSnap(c, s.state, snapdYaml, noHook)
	snapG := mockInstalledSnap(c, s.state, snapGyaml, useHook)
	// unrelated snap
	snapF := mockInstalledSnap(c, s.state, snapFyaml, useHook)

	snapFAppSet, err := interfaces.NewSnapAppSet(snapF, nil)
	c.Assert(err, IsNil)
	snapdSnapAppSet, err := interfaces.NewSnapAppSet(snapdSnap, nil)
	c.Assert(err, IsNil)
	snapGAppSet, err := interfaces.NewSnapAppSet(snapG, nil)
	c.Assert(err, IsNil)

	c.Assert(s.repo.AddAppSet(snapFAppSet), IsNil)
	c.Assert(s.repo.AddAppSet(snapdSnapAppSet), IsNil)
	c.Assert(s.repo.AddAppSet(snapGAppSet), IsNil)
	cref := &interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "snap-g", Name: "desktop"}, SlotRef: interfaces.SlotRef{Snap: "snapd", Name: "desktop"}}
	_, err = s.repo.Connect(cref, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	// snapE has a plug using mount backend, refreshing snapd affects snapE.
	updates := []string{snapdSnap.InstanceName()}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-g": {
			Restart: true,
			AffectingSnaps: map[string]bool{
				"snapd": true,
			}}})
}

func (s *autorefreshGatingSuite) TestAffectedByPlugWithMountBackendCoreSlot(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	st := s.state

	st.Lock()
	defer st.Unlock()

	coreSnap := mockInstalledSnap(c, s.state, coreYaml, noHook)
	snapG := mockInstalledSnap(c, s.state, snapGyaml, useHook)

	coreAppSet, err := interfaces.NewSnapAppSet(coreSnap, nil)
	c.Assert(err, IsNil)

	snapGAppSet, err := interfaces.NewSnapAppSet(snapG, nil)
	c.Assert(err, IsNil)

	c.Assert(s.repo.AddAppSet(coreAppSet), IsNil)
	c.Assert(s.repo.AddAppSet(snapGAppSet), IsNil)
	cref := &interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: "snap-g", Name: "desktop"}, SlotRef: interfaces.SlotRef{Snap: "core", Name: "desktop"}}
	_, err = s.repo.Connect(cref, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	// snapG has a plug using mount backend, refreshing core affects snapE.
	updates := []string{coreSnap.InstanceName()}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-g": {
			Restart: true,
			AffectingSnaps: map[string]bool{
				"core": true,
			}}})
}

func (s *autorefreshGatingSuite) TestAffectedByBootBase(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	st := s.state

	r := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer r()

	st.Lock()
	defer st.Unlock()
	mockInstalledSnap(c, s.state, snapAyaml, useHook)
	mockInstalledSnap(c, s.state, snapByaml, useHook)
	mockInstalledSnap(c, s.state, snapDyaml, useHook)
	mockInstalledSnap(c, s.state, snapEyaml, useHook)
	core18 := mockInstalledSnap(c, s.state, core18Yaml, noHook)

	updates := []string{core18.InstanceName()}
	affected, err := snapstate.AffectedByRefresh(st, updates)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-a": {
			Base:    false,
			Restart: true,
			AffectingSnaps: map[string]bool{
				"core18": true,
			},
		},
		"snap-b": {
			Base:    false,
			Restart: true,
			AffectingSnaps: map[string]bool{
				"core18": true,
			},
		},
		"snap-d": {
			Base:    false,
			Restart: true,
			AffectingSnaps: map[string]bool{
				"core18": true,
			},
		},
		"snap-e": {
			Base:    false,
			Restart: true,
			AffectingSnaps: map[string]bool{
				"core18": true,
			}}})
}

func (s *autorefreshGatingSuite) TestCreateAutoRefreshGateHooks(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	affected := []string{"snap-a", "snap-b"}
	seenSnaps := make(map[string]bool)

	ts := snapstate.CreateGateAutoRefreshHooks(st, affected)
	c.Assert(ts.Tasks(), HasLen, 2)

	checkHook := func(t *state.Task) {
		c.Assert(t.Kind(), Equals, "run-hook")
		var hs hookstate.HookSetup
		c.Assert(t.Get("hook-setup", &hs), IsNil)
		c.Check(hs.Hook, Equals, "gate-auto-refresh")
		c.Check(hs.Optional, Equals, true)
		seenSnaps[hs.Snap] = true
	}

	checkHook(ts.Tasks()[0])
	checkHook(ts.Tasks()[1])

	c.Check(seenSnaps, DeepEquals, map[string]bool{"snap-a": true, "snap-b": true})
}

func (s *autorefreshGatingSuite) TestAffectedByRefreshCandidates(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, st, snapAyaml, useHook)
	// unrelated snap
	mockInstalledSnap(c, st, snapByaml, useHook)

	// no refresh-candidates in state
	affected, err := snapstate.AffectedByRefreshCandidates(st)
	c.Assert(err, IsNil)
	c.Check(affected, HasLen, 0)

	candidates := map[string]*snapstate.RefreshCandidate{"snap-a": {}}
	st.Set("refresh-candidates", &candidates)

	affected, err = snapstate.AffectedByRefreshCandidates(st)
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, map[string]*snapstate.AffectedSnapInfo{
		"snap-a": {
			AffectingSnaps: map[string]bool{
				"snap-a": true,
			}}})
}

func (s *autorefreshGatingSuite) TestAffectingSnapsForAffectedByRefreshCandidates(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, st, snapAyaml, useHook)
	mockInstalledSnap(c, st, snapByaml, useHook)
	mockInstalledSnap(c, st, baseSnapByaml, useHook)

	candidates := map[string]*snapstate.RefreshCandidate{
		"snap-a":      {},
		"snap-b":      {},
		"base-snap-b": {},
	}
	st.Set("refresh-candidates", &candidates)

	affecting, err := snapstate.AffectingSnapsForAffectedByRefreshCandidates(st, "snap-b")
	c.Assert(err, IsNil)
	c.Check(affecting, DeepEquals, []string{"base-snap-b", "snap-b"})
}

func (s *autorefreshGatingSuite) TestAutorefreshPhase1FeatureFlag(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	st.Set("seeded", true)

	restore := snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = nil }()

	s.store.refreshedSnaps = []*snap.Info{{
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			RealName: "snap-a",
			Revision: snap.R(8),
		},
	}}
	mockInstalledSnap(c, s.state, snapAyaml, useHook)

	// gate-auto-refresh-hook feature not enabled, expect old-style refresh.
	_, updateTss, err := snapstate.AutoRefresh(context.TODO(), st)
	c.Check(err, IsNil)
	tss := updateTss.Refresh
	c.Assert(tss, HasLen, 2)
	c.Check(tss[0].Tasks()[0].Kind(), Equals, "prerequisites")
	c.Check(tss[0].Tasks()[1].Kind(), Equals, "download-snap")
	c.Check(tss[1].Tasks()[0].Kind(), Equals, "check-rerefresh")

	// enable gate-auto-refresh-hook feature
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.gate-auto-refresh-hook", true)
	tr.Commit()

	_, updateTss, err = snapstate.AutoRefresh(context.TODO(), st)
	c.Check(err, IsNil)
	tss = updateTss.Refresh
	c.Assert(tss, HasLen, 2)
	task := tss[0].Tasks()[0]
	c.Check(task.Kind(), Equals, "conditional-auto-refresh")
	var toUpdate map[string]*snapstate.RefreshCandidate
	c.Assert(task.Get("snaps", &toUpdate), IsNil)
	seenSnaps := make(map[string]bool)
	for up := range toUpdate {
		seenSnaps[up] = true
	}
	c.Check(seenSnaps, DeepEquals, map[string]bool{"snap-a": true})
	c.Check(tss[1].Tasks()[0].Kind(), Equals, "run-hook")
}

func (s *autorefreshGatingSuite) TestAutoRefreshPhase1(c *C) {
	s.store.refreshedSnaps = []*snap.Info{{
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			RealName: "snap-a",
			Revision: snap.R(8),
		},
	}, {
		Architectures: []string{"all"},
		SnapType:      snap.TypeBase,
		SideInfo: snap.SideInfo{
			RealName: "base-snap-b",
			Revision: snap.R(3),
		},
	}, {
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			RealName: "snap-c",
			Revision: snap.R(5),
		},
	}, {
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			// this one will be monitored for running apps
			RealName: "snap-f",
			Revision: snap.R(6),
		},
	}}

	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, s.state, snapAyaml, useHook)
	mockInstalledSnap(c, s.state, snapByaml, useHook)
	mockInstalledSnap(c, s.state, snapCyaml, noHook)
	mockInstalledSnap(c, s.state, baseSnapByaml, noHook)
	mockInstalledSnap(c, s.state, snapDyaml, noHook)
	mockInstalledSnap(c, s.state, snapFyaml, noHook)

	restore := snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	// pretend some snaps are held
	_, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "gating-snap", 0, "snap-a", "snap-d")
	c.Assert(err, IsNil)
	// validity check
	heldSnaps, err := snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(heldSnaps, DeepEquals, map[string][]string{
		"snap-a": {"gating-snap"},
		"snap-d": {"gating-snap"},
	})
	// we're already monitoring one of the snaps
	st.Cache("monitored-snaps", map[string]context.CancelFunc{
		"snap-f": func() {},
	})
	st.Set("refresh-candidates", map[string]*snapstate.RefreshCandidate{
		"snap-f": {
			SnapSetup: snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "snap-f", Revision: snap.R(6)}},
			Monitored: true,
		},
	})
	names, tss, err := snapstate.AutoRefreshPhase1(context.TODO(), st, "")
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"base-snap-b", "snap-a", "snap-c", "snap-f"})
	c.Assert(tss, HasLen, 2)

	c.Assert(tss[0].Tasks(), HasLen, 1)
	checkGatingTask(c, tss[0].Tasks()[0], map[string]*snapstate.RefreshCandidate{
		"snap-a": {
			SnapSetup: snapstate.SnapSetup{
				Type:      "app",
				PlugsOnly: true,
				Flags: snapstate.Flags{
					IsAutoRefresh: true,
				},
				SideInfo: &snap.SideInfo{
					RealName: "snap-a",
					Revision: snap.R(8),
				},
				DownloadInfo: &snap.DownloadInfo{},
			},
		},
		"base-snap-b": {
			SnapSetup: snapstate.SnapSetup{
				Type:      "base",
				PlugsOnly: true,
				Flags: snapstate.Flags{
					IsAutoRefresh: true,
				},
				SideInfo: &snap.SideInfo{
					RealName: "base-snap-b",
					Revision: snap.R(3),
				},
				DownloadInfo: &snap.DownloadInfo{},
			},
		},
		"snap-c": {
			SnapSetup: snapstate.SnapSetup{
				Type:      "app",
				PlugsOnly: true,
				Flags: snapstate.Flags{
					IsAutoRefresh: true,
				},
				SideInfo: &snap.SideInfo{
					RealName: "snap-c",
					Revision: snap.R(5),
				},
				DownloadInfo: &snap.DownloadInfo{},
			},
		},
		"snap-f": {
			SnapSetup: snapstate.SnapSetup{
				Type:      "app",
				PlugsOnly: true,
				Flags: snapstate.Flags{
					IsAutoRefresh: true,
				},
				SideInfo: &snap.SideInfo{
					RealName: "snap-f",
					Revision: snap.R(6),
				},
				DownloadInfo: &snap.DownloadInfo{},
			},
			Monitored: true,
		},
	})

	c.Assert(tss[1].Tasks(), HasLen, 2)

	// check hooks for affected snaps
	seenSnaps := make(map[string]bool)
	var hs hookstate.HookSetup
	task := tss[1].Tasks()[0]
	c.Assert(task.Get("hook-setup", &hs), IsNil)
	c.Check(hs.Hook, Equals, "gate-auto-refresh")
	seenSnaps[hs.Snap] = true

	task = tss[1].Tasks()[1]
	c.Assert(task.Get("hook-setup", &hs), IsNil)
	c.Check(hs.Hook, Equals, "gate-auto-refresh")
	seenSnaps[hs.Snap] = true

	// hook for snap-a because it gets refreshed, for snap-b because its base
	// gets refreshed. snap-c is refreshed but doesn't have the hook.
	c.Check(seenSnaps, DeepEquals, map[string]bool{"snap-a": true, "snap-b": true})

	// check that refresh-candidates in the state were updated
	var candidates map[string]*snapstate.RefreshCandidate
	c.Assert(st.Get("refresh-candidates", &candidates), IsNil)
	c.Assert(candidates, HasLen, 4)
	c.Check(candidates["snap-a"], NotNil)
	c.Check(candidates["base-snap-b"], NotNil)
	c.Check(candidates["snap-c"], NotNil)
	c.Assert(candidates["snap-f"], NotNil)
	c.Check(candidates["snap-f"].Monitored, Equals, true)

	// check that after autoRefreshPhase1 any held snaps that are not in refresh
	// candidates got removed.
	heldSnaps, err = snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	// snap-d got removed from held snaps.
	c.Check(heldSnaps, DeepEquals, map[string][]string{
		"snap-a": {"gating-snap"},
	})
}

// this test demonstrates that affectedByRefresh uses current snap info (not
// snap infos of store updates) by simulating a different base for the updated
// snap from the store.
func (s *autorefreshGatingSuite) TestAffectedByRefreshUsesCurrentSnapInfo(c *C) {
	s.store.refreshedSnaps = []*snap.Info{{
		Architectures: []string{"all"},
		SnapType:      snap.TypeBase,
		SideInfo: snap.SideInfo{
			RealName: "base-snap-b",
			Revision: snap.R(3),
		},
	}, {
		Architectures: []string{"all"},
		Base:          "new-base",
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			RealName: "snap-b",
			Revision: snap.R(5),
		},
	}}

	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, s.state, snapByaml, useHook)
	mockInstalledSnap(c, s.state, baseSnapByaml, noHook)

	restore := snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	names, tss, err := snapstate.AutoRefreshPhase1(context.TODO(), st, "")
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"base-snap-b", "snap-b"})
	c.Assert(tss, HasLen, 2)

	c.Assert(tss[0].Tasks(), HasLen, 1)
	checkGatingTask(c, tss[0].Tasks()[0], map[string]*snapstate.RefreshCandidate{
		"snap-b": {
			SnapSetup: snapstate.SnapSetup{
				Type:      "app",
				Base:      "new-base",
				PlugsOnly: true,
				Flags: snapstate.Flags{
					IsAutoRefresh: true,
				},
				SideInfo: &snap.SideInfo{
					RealName: "snap-b",
					Revision: snap.R(5),
				},
				DownloadInfo: &snap.DownloadInfo{},
			},
		},
		"base-snap-b": {
			SnapSetup: snapstate.SnapSetup{
				Type:      "base",
				PlugsOnly: true,
				Flags: snapstate.Flags{
					IsAutoRefresh: true,
				},
				SideInfo: &snap.SideInfo{
					RealName: "base-snap-b",
					Revision: snap.R(3),
				},
				DownloadInfo: &snap.DownloadInfo{},
			},
		},
	})

	c.Assert(tss[1].Tasks(), HasLen, 1)
	var hs hookstate.HookSetup
	task := tss[1].Tasks()[0]
	c.Assert(task.Get("hook-setup", &hs), IsNil)
	c.Check(hs.Hook, Equals, "gate-auto-refresh")
	c.Check(hs.Snap, Equals, "snap-b")

	// check that refresh-candidates in the state were updated
	var candidates map[string]*snapstate.RefreshCandidate
	c.Assert(st.Get("refresh-candidates", &candidates), IsNil)
	c.Assert(candidates, HasLen, 2)
	c.Check(candidates["snap-b"], NotNil)
	c.Check(candidates["base-snap-b"], NotNil)
}

func (s *autorefreshGatingSuite) TestAutoRefreshPhase1ConflictsFilteredOut(c *C) {
	s.store.refreshedSnaps = []*snap.Info{{
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			RealName: "snap-a",
			Revision: snap.R(8),
		},
	}, {
		Architectures: []string{"all"},
		SnapType:      snap.TypeBase,
		SideInfo: snap.SideInfo{
			RealName: "snap-c",
			Revision: snap.R(5),
		},
	}}

	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, s.state, snapAyaml, useHook)
	mockInstalledSnap(c, s.state, snapCyaml, noHook)

	conflictChange := st.NewChange("conflicting change", "")
	conflictTask := st.NewTask("conflicting task", "")
	si := &snap.SideInfo{
		RealName: "snap-c",
		Revision: snap.R(1),
	}
	sup := snapstate.SnapSetup{SideInfo: si}
	conflictTask.Set("snap-setup", sup)
	conflictChange.AddTask(conflictTask)

	restore := snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	logbuf, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	names, tss, err := snapstate.AutoRefreshPhase1(context.TODO(), st, "")
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"snap-a"})
	c.Assert(tss, HasLen, 2)

	c.Assert(tss[0].Tasks(), HasLen, 1)
	checkGatingTask(c, tss[0].Tasks()[0], map[string]*snapstate.RefreshCandidate{
		"snap-a": {
			SnapSetup: snapstate.SnapSetup{
				Type:      "app",
				PlugsOnly: true,
				Flags: snapstate.Flags{
					IsAutoRefresh: true,
				},
				SideInfo: &snap.SideInfo{
					RealName: "snap-a",
					Revision: snap.R(8),
				},
				DownloadInfo: &snap.DownloadInfo{},
			}}})

	c.Assert(tss[1].Tasks(), HasLen, 1)

	c.Assert(logbuf.String(), testutil.Contains, `cannot refresh snap "snap-c": snap "snap-c" has "conflicting change" change in progress`)

	seenSnaps := make(map[string]bool)
	var hs hookstate.HookSetup
	c.Assert(tss[1].Tasks()[0].Get("hook-setup", &hs), IsNil)
	c.Check(hs.Hook, Equals, "gate-auto-refresh")
	seenSnaps[hs.Snap] = true

	c.Check(seenSnaps, DeepEquals, map[string]bool{"snap-a": true})

	// check that refresh-candidates in the state were updated
	var candidates map[string]*snapstate.RefreshCandidate
	c.Assert(st.Get("refresh-candidates", &candidates), IsNil)
	c.Assert(candidates, HasLen, 2)
	c.Check(candidates["snap-a"], NotNil)
	c.Check(candidates["snap-c"], NotNil)
}

func (s *autorefreshGatingSuite) TestAutoRefreshPhase1NoHooks(c *C) {
	s.store.refreshedSnaps = []*snap.Info{{
		Architectures: []string{"all"},
		SnapType:      snap.TypeBase,
		SideInfo: snap.SideInfo{
			RealName: "base-snap-b",
			Revision: snap.R(3),
		},
	}, {
		Architectures: []string{"all"},
		SnapType:      snap.TypeBase,
		SideInfo: snap.SideInfo{
			RealName: "snap-c",
			Revision: snap.R(5),
		},
	}}

	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, s.state, snapByaml, noHook)
	mockInstalledSnap(c, s.state, snapCyaml, noHook)
	mockInstalledSnap(c, s.state, baseSnapByaml, noHook)

	restore := snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	names, tss, err := snapstate.AutoRefreshPhase1(context.TODO(), st, "")
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"base-snap-b", "snap-c"})
	c.Assert(tss, HasLen, 1)

	c.Assert(tss[0].Tasks(), HasLen, 1)
	c.Check(tss[0].Tasks()[0].Kind(), Equals, "conditional-auto-refresh")
}

func (s *autorefreshGatingSuite) TestHoldRefreshesBySystemIndefinitely(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{
		Revision: snap.R(1),
		SnapID:   "some-snap-id",
		RealName: "some-snap",
	}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		Active:   true,
	})

	fixedTime, err := time.Parse(time.RFC3339, "0001-02-03T04:05:06Z")
	c.Assert(err, IsNil)
	restore := snapstate.MockTimeNow(func() time.Time {
		return fixedTime
	})
	defer restore()

	err = snapstate.HoldRefreshesBySystem(s.state, snapstate.HoldAutoRefresh, "forever", []string{"some-snap"})
	c.Assert(err, IsNil)

	var gating map[string]map[string]*snapstate.HoldState
	c.Assert(s.state.Get("snaps-hold", &gating), IsNil)
	c.Assert(gating, DeepEquals, map[string]map[string]*snapstate.HoldState{
		"some-snap": {"system": &snapstate.HoldState{
			FirstHeld: fixedTime,
			HoldUntil: fixedTime.Add(time.Duration(1<<63 - 1)),
		}},
	})
}

func (s *autorefreshGatingSuite) TestUnholdSnaps(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{
		Revision: snap.R(1),
		SnapID:   "some-snap-id",
		RealName: "some-snap",
	}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		Active:   true,
	})

	fixedTime, err := time.Parse(time.RFC3339, "0001-02-03T04:05:06Z")
	c.Assert(err, IsNil)
	gating := map[string]map[string]*snapstate.HoldState{
		"some-snap": {"system": &snapstate.HoldState{
			FirstHeld: fixedTime,
			HoldUntil: fixedTime.Add(time.Duration(1<<63 - 1)),
		}},
	}
	s.state.Set("snaps-hold", gating)

	err = snapstate.ProceedWithRefresh(s.state, "system", []string{"some-snap"})
	c.Assert(err, IsNil)

	gating = make(map[string]map[string]*snapstate.HoldState)
	c.Assert(s.state.Get("snaps-hold", &gating), IsNil)
	c.Assert(gating, HasLen, 0)
}

func fakeReadInfo(name string, si *snap.SideInfo) (*snap.Info, error) {
	info := &snap.Info{
		SuggestedName: name,
		SideInfo:      *si,
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		Epoch:         snap.Epoch{},
	}
	switch name {
	case "base-snap-b":
		info.SnapType = snap.TypeBase
	case "snap-a", "snap-b":
		info.Hooks = map[string]*snap.HookInfo{
			"gate-auto-refresh": {
				Name: "gate-auto-refresh",
				Snap: info,
			},
		}
		if name == "snap-b" {
			info.Base = "base-snap-b"
		}
	}
	return info, nil
}

func (s *snapmgrTestSuite) testAutoRefreshPhase2(c *C, beforePhase1 func(), gateAutoRefreshHook func(snapName string), expected []string) *state.Change {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// enable gate-auto-refresh-hook feature
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.gate-auto-refresh-hook", true)
	tr.Commit()

	s.o.TaskRunner().AddHandler("run-hook", func(t *state.Task, tomb *tomb.Tomb) error {
		var hsup hookstate.HookSetup
		t.State().Lock()
		defer t.State().Unlock()
		c.Assert(t.Get("hook-setup", &hsup), IsNil)
		if hsup.Hook == "gate-auto-refresh" && gateAutoRefreshHook != nil {
			gateAutoRefreshHook(hsup.Snap)
		}
		return nil
	}, nil)

	restoreInstallSize := snapstate.MockInstallSize(func(st *state.State, snaps []snapstate.MinimalInstallInfo, userID int, prqt snapstate.PrereqTracker) (uint64, error) {
		c.Fatal("unexpected call to installSize")
		return 0, nil
	})
	defer restoreInstallSize()

	snapstate.ReplaceStore(s.state, &autoRefreshGatingStore{
		fakeStore: s.fakeStore,
		refreshedSnaps: []*snap.Info{{
			Architectures: []string{"all"},
			SnapType:      snap.TypeApp,
			SideInfo: snap.SideInfo{
				RealName: "snap-a",
				Revision: snap.R(8),
			},
		}, {
			Architectures: []string{"all"},
			SnapType:      snap.TypeBase,
			SideInfo: snap.SideInfo{
				RealName: "base-snap-b",
				Revision: snap.R(3),
			},
		}}})

	mockInstalledSnap(c, s.state, snapAyaml, useHook)
	mockInstalledSnap(c, s.state, snapByaml, useHook)
	mockInstalledSnap(c, s.state, baseSnapByaml, noHook)

	snapstate.MockSnapReadInfo(fakeReadInfo)

	restore := snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	if beforePhase1 != nil {
		beforePhase1()
	}

	names, tss, err := snapstate.AutoRefreshPhase1(context.TODO(), st, "")
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"base-snap-b", "snap-a"})

	var snaps map[string]interface{}
	c.Assert(tss[0].Tasks()[0].Kind(), Equals, "conditional-auto-refresh")
	c.Assert(tss[0].Tasks()[0].Get("snaps", &snaps), IsNil)
	c.Assert(snaps, HasLen, 2)
	c.Check(snaps["snap-a"], NotNil)
	c.Check(snaps["base-snap-b"], NotNil)

	chg := s.state.NewChange("refresh", "...")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	s.settle(c)

	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(chg.Err(), IsNil)

	verifyPhasedAutorefreshTasks(c, chg.Tasks(), expected)

	return chg
}

func (s *snapmgrTestSuite) TestAutoRefreshPhase2(c *C) {
	expected := []string{
		"conditional-auto-refresh",
		"run-hook [snap-a;gate-auto-refresh]",
		// snap-b hook is triggered because of base-snap-b refresh
		"run-hook [snap-b;gate-auto-refresh]",
		"prerequisites",
		"download-snap",
		"validate-snap",
		"mount-snap",
		"run-hook [base-snap-b;pre-refresh]",
		"stop-snap-services",
		"remove-aliases",
		"unlink-current-snap",
		"copy-snap-data",
		"setup-profiles",
		"link-snap",
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"run-hook [base-snap-b;post-refresh]",
		"start-snap-services",
		"cleanup",
		"run-hook [base-snap-b;check-health]",
		"prerequisites",
		"download-snap",
		"validate-snap",
		"mount-snap",
		"run-hook [snap-a;pre-refresh]",
		"stop-snap-services",
		"remove-aliases",
		"unlink-current-snap",
		"copy-snap-data",
		"setup-profiles",
		"link-snap",
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"run-hook [snap-a;post-refresh]",
		"start-snap-services",
		"cleanup",
		"run-hook [snap-a;configure]",
		"run-hook [snap-a;check-health]",
		"check-rerefresh",
	}

	seenSnapsWithGateAutoRefreshHook := make(map[string]bool)

	chg := s.testAutoRefreshPhase2(c, nil, func(snapName string) {
		seenSnapsWithGateAutoRefreshHook[snapName] = true
	}, expected)

	c.Check(seenSnapsWithGateAutoRefreshHook, DeepEquals, map[string]bool{
		"snap-a": true,
		"snap-b": true,
	})

	s.state.Lock()
	defer s.state.Unlock()

	tasks := chg.Tasks()
	c.Check(tasks[len(tasks)-1].Summary(), Equals, `Monitoring snaps "base-snap-b", "snap-a" to determine whether extra refresh steps are required`)

	var snaps map[string]interface{}
	c.Assert(chg.Tasks()[0].Kind(), Equals, "conditional-auto-refresh")
	chg.Tasks()[0].Get("snaps", &snaps)
	c.Assert(snaps, HasLen, 2)
	c.Check(snaps["snap-a"], NotNil)
	c.Check(snaps["base-snap-b"], NotNil)

	// all snaps refreshed, all removed from refresh-candidates.
	var candidates map[string]*snapstate.RefreshCandidate
	c.Assert(s.state.Get("refresh-candidates", &candidates), testutil.ErrorIs, &state.NoStateError{})
}

func (s *snapmgrTestSuite) TestAutoRefreshPhase2Held(c *C) {
	logbuf, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	expected := []string{
		"conditional-auto-refresh",
		"run-hook [snap-a;gate-auto-refresh]",
		// snap-b hook is triggered because of base-snap-b refresh
		"run-hook [snap-b;gate-auto-refresh]",
		"prerequisites",
		"download-snap",
		"validate-snap",
		"mount-snap",
		"run-hook [snap-a;pre-refresh]",
		"stop-snap-services",
		"remove-aliases",
		"unlink-current-snap",
		"copy-snap-data",
		"setup-profiles",
		"link-snap",
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"run-hook [snap-a;post-refresh]",
		"start-snap-services",
		"cleanup",
		"run-hook [snap-a;configure]",
		"run-hook [snap-a;check-health]",
		"check-rerefresh",
	}

	chg := s.testAutoRefreshPhase2(c, nil, func(snapName string) {
		if snapName == "snap-b" {
			// pretend than snap-b calls snapctl --hold to hold refresh of base-snap-b
			_, err := snapstate.HoldRefresh(s.state, snapstate.HoldAutoRefresh, "snap-b", 0, "base-snap-b")
			c.Assert(err, IsNil)
		}
	}, expected)

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(logbuf.String(), testutil.Contains, `skipping refresh of held snaps: base-snap-b`)
	tasks := chg.Tasks()

	var snaps map[string]interface{}
	c.Assert(chg.Tasks()[0].Kind(), Equals, "conditional-auto-refresh")
	chg.Tasks()[0].Get("snaps", &snaps)
	c.Assert(snaps, HasLen, 1)
	c.Check(snaps["snap-a"], NotNil)

	// no re-refresh for base-snap-b because it was held.
	c.Check(tasks[len(tasks)-1].Summary(), Equals, `Monitoring snap "snap-a" to determine whether extra refresh steps are required`)
}

func (s *snapmgrTestSuite) TestAutoRefreshPhase2Proceed(c *C) {
	logbuf, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	expected := []string{
		"conditional-auto-refresh",
		"run-hook [snap-a;gate-auto-refresh]",
		// snap-b hook is triggered because of base-snap-b refresh
		"run-hook [snap-b;gate-auto-refresh]",
		"prerequisites",
		"download-snap",
		"validate-snap",
		"mount-snap",
		"run-hook [snap-a;pre-refresh]",
		"stop-snap-services",
		"remove-aliases",
		"unlink-current-snap",
		"copy-snap-data",
		"setup-profiles",
		"link-snap",
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"run-hook [snap-a;post-refresh]",
		"start-snap-services",
		"cleanup",
		"run-hook [snap-a;configure]",
		"run-hook [snap-a;check-health]",
		"check-rerefresh",
	}

	s.testAutoRefreshPhase2(c, func() {
		// pretend that snap-a and base-snap-b are initially held
		_, err := snapstate.HoldRefresh(s.state, snapstate.HoldAutoRefresh, "snap-a", 0, "snap-a")
		c.Assert(err, IsNil)
		_, err = snapstate.HoldRefresh(s.state, snapstate.HoldAutoRefresh, "snap-b", 0, "base-snap-b")
		c.Assert(err, IsNil)
	}, func(snapName string) {
		if snapName == "snap-a" {
			// pretend than snap-a calls snapctl --proceed
			err := snapstate.ProceedWithRefresh(s.state, "snap-a", nil)
			c.Assert(err, IsNil)
		}
		// note, do nothing about snap-b which just keeps its hold state in
		// the test, but if we were using real gate-auto-refresh hook
		// handler, the default behavior for snap-b if it doesn't call --hold
		// would be to proceed (hook handler would take care of that).
	}, expected)

	c.Assert(logbuf.String(), testutil.Contains, `skipping refresh of held snaps: base-snap-b`)
}

func (s *snapmgrTestSuite) TestAutoRefreshPhase2AllHeld(c *C) {
	logbuf, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	expected := []string{
		"conditional-auto-refresh",
		"run-hook [snap-a;gate-auto-refresh]",
		// snap-b hook is triggered because of base-snap-b refresh
		"run-hook [snap-b;gate-auto-refresh]",
	}

	s.testAutoRefreshPhase2(c, nil, func(snapName string) {
		switch snapName {
		case "snap-b":
			// pretend that snap-b calls snapctl --hold to hold refresh of base-snap-b
			_, err := snapstate.HoldRefresh(s.state, snapstate.HoldAutoRefresh, "snap-b", 0, "base-snap-b")
			c.Assert(err, IsNil)
		case "snap-a":
			// pretend that snap-a calls snapctl --hold to hold itself
			_, err := snapstate.HoldRefresh(s.state, snapstate.HoldAutoRefresh, "snap-a", 0, "snap-a")
			c.Assert(err, IsNil)
		default:
			c.Fatalf("unexpected snap %q", snapName)
		}
	}, expected)

	c.Assert(logbuf.String(), testutil.Contains, `skipping refresh of held snaps: base-snap-b,snap-a`)
}

func (s *snapmgrTestSuite) testAutoRefreshPhase2DiskSpaceCheck(c *C, fail bool) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	restore := snapstate.MockOsutilCheckFreeSpace(func(path string, sz uint64) error {
		c.Check(sz, Equals, snapstate.SafetyMarginDiskSpace(123))
		if fail {
			return &osutil.NotEnoughDiskSpaceError{}
		}
		return nil
	})
	defer restore()

	var installSizeCalled bool
	restoreInstallSize := snapstate.MockInstallSize(func(st *state.State, snaps []snapstate.MinimalInstallInfo, userID int, prqt snapstate.PrereqTracker) (uint64, error) {
		installSizeCalled = true
		seen := map[string]bool{}
		for _, sn := range snaps {
			seen[sn.InstanceName()] = true
		}
		c.Check(seen, DeepEquals, map[string]bool{
			"base-snap-b": true,
			"snap-a":      true,
		})
		return 123, nil
	})
	defer restoreInstallSize()

	restoreModel := snapstatetest.MockDeviceModel(DefaultModel())
	defer restoreModel()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.check-disk-space-refresh", true)
	tr.Commit()

	snapstate.ReplaceStore(s.state, &autoRefreshGatingStore{
		fakeStore: s.fakeStore,
		refreshedSnaps: []*snap.Info{{
			Architectures: []string{"all"},
			SnapType:      snap.TypeApp,
			SideInfo: snap.SideInfo{
				RealName: "snap-a",
				Revision: snap.R(8),
			},
		}, {
			Architectures: []string{"all"},
			SnapType:      snap.TypeBase,
			SideInfo: snap.SideInfo{
				RealName: "base-snap-b",
				Revision: snap.R(3),
			},
		}}})

	mockInstalledSnap(c, s.state, snapAyaml, useHook)
	mockInstalledSnap(c, s.state, snapByaml, useHook)
	mockInstalledSnap(c, s.state, baseSnapByaml, noHook)

	snapstate.MockSnapReadInfo(fakeReadInfo)

	names, tss, err := snapstate.AutoRefreshPhase1(context.TODO(), st, "")
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"base-snap-b", "snap-a"})

	chg := s.state.NewChange("refresh", "...")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	s.settle(c)

	c.Check(installSizeCalled, Equals, true)
	if fail {
		c.Check(chg.Status(), Equals, state.ErrorStatus)
		c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n- Run auto-refresh for ready snaps \(insufficient space.*`)
	} else {
		c.Check(chg.Status(), Equals, state.DoneStatus)
		c.Check(chg.Err(), IsNil)
	}
}

func (s *snapmgrTestSuite) TestAutoRefreshPhase2DiskSpaceError(c *C) {
	fail := true
	s.testAutoRefreshPhase2DiskSpaceCheck(c, fail)
}

func (s *snapmgrTestSuite) TestAutoRefreshPhase2DiskSpaceHappy(c *C) {
	var nofail bool
	s.testAutoRefreshPhase2DiskSpaceCheck(c, nofail)
}

// XXX: this case is probably artificial; with proper conflict prevention
// we shouldn't get conflicts from doInstall in phase2.
func (s *snapmgrTestSuite) TestAutoRefreshPhase2Conflict(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	snapstate.ReplaceStore(s.state, &autoRefreshGatingStore{
		fakeStore: s.fakeStore,
		refreshedSnaps: []*snap.Info{{
			Architectures: []string{"all"},
			SnapType:      snap.TypeApp,
			SideInfo: snap.SideInfo{
				RealName: "snap-a",
				Revision: snap.R(8),
			},
		}, {
			Architectures: []string{"all"},
			SnapType:      snap.TypeBase,
			SideInfo: snap.SideInfo{
				RealName: "base-snap-b",
				Revision: snap.R(3),
			},
		}}})

	mockInstalledSnap(c, s.state, snapAyaml, useHook)
	mockInstalledSnap(c, s.state, snapByaml, useHook)
	mockInstalledSnap(c, s.state, baseSnapByaml, noHook)

	snapstate.MockSnapReadInfo(fakeReadInfo)

	restore := snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	names, tss, err := snapstate.AutoRefreshPhase1(context.TODO(), st, "")
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"base-snap-b", "snap-a"})

	chg := s.state.NewChange("refresh", "...")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	conflictChange := st.NewChange("conflicting change", "")
	conflictTask := st.NewTask("conflicting task", "")
	si := &snap.SideInfo{
		RealName: "snap-a",
		Revision: snap.R(1),
	}
	sup := snapstate.SnapSetup{SideInfo: si}
	conflictTask.Set("snap-setup", sup)
	conflictChange.AddTask(conflictTask)
	conflictTask.WaitFor(tss[0].Tasks()[0])

	s.settle(c)

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Check(chg.Err(), IsNil)

	// no refresh of snap-a because of the conflict.
	expected := []string{
		"conditional-auto-refresh",
		"run-hook [snap-a;gate-auto-refresh]",
		// snap-b hook is triggered because of base-snap-b refresh
		"run-hook [snap-b;gate-auto-refresh]",
		"prerequisites",
		"download-snap",
		"validate-snap",
		"mount-snap",
		"run-hook [base-snap-b;pre-refresh]",
		"stop-snap-services",
		"remove-aliases",
		"unlink-current-snap",
		"copy-snap-data",
		"setup-profiles",
		"link-snap",
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"run-hook [base-snap-b;post-refresh]",
		"start-snap-services",
		"cleanup",
		"run-hook [base-snap-b;check-health]",
		"check-rerefresh",
	}
	verifyPhasedAutorefreshTasks(c, chg.Tasks(), expected)
}

func (s *snapmgrTestSuite) TestAutoRefreshPhase2ConflictOtherSnapOp(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	snapstate.ReplaceStore(s.state, &autoRefreshGatingStore{
		fakeStore: s.fakeStore,
		refreshedSnaps: []*snap.Info{{
			Architectures: []string{"all"},
			SnapType:      snap.TypeApp,
			SideInfo: snap.SideInfo{
				RealName: "snap-a",
				Revision: snap.R(8),
			},
		}}})

	mockInstalledSnap(c, s.state, snapAyaml, useHook)

	snapstate.MockSnapReadInfo(fakeReadInfo)

	restore := snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	names, tss, err := snapstate.AutoRefreshPhase1(context.TODO(), st, "")
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"snap-a"})

	chg := s.state.NewChange("fake-auto-refresh", "...")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	s.state.Unlock()
	// run first task
	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	_, err = snapstate.Remove(s.state, "snap-a", snap.R(8), nil)
	c.Assert(err, DeepEquals, &snapstate.ChangeConflictError{
		ChangeKind: "fake-auto-refresh",
		Snap:       "snap-a",
		ChangeID:   chg.ID(),
	})

	_, err = snapstate.Update(s.state, "snap-a", nil, 0, snapstate.Flags{})
	c.Assert(err, DeepEquals, &snapstate.ChangeConflictError{
		ChangeKind: "fake-auto-refresh",
		Snap:       "snap-a",
		ChangeID:   chg.ID(),
	})

	// only 2 tasks because we don't run settle() so conditional-auto-refresh
	// doesn't run and no new tasks get created.
	expected := []string{
		"conditional-auto-refresh",
		"run-hook [snap-a;gate-auto-refresh]",
	}
	verifyPhasedAutorefreshTasks(c, chg.Tasks(), expected)
}

func (s *snapmgrTestSuite) TestAutoRefreshPhase2GatedSnaps(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// enable gate-auto-refresh-hook feature
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.gate-auto-refresh-hook", true)
	tr.Commit()

	restore := snapstate.MockSnapsToRefresh(func(gatingTask *state.Task) ([]*snapstate.RefreshCandidate, error) {
		c.Assert(gatingTask.Kind(), Equals, "conditional-auto-refresh")
		var candidates map[string]*snapstate.RefreshCandidate
		c.Assert(gatingTask.Get("snaps", &candidates), IsNil)
		seenSnaps := make(map[string]bool)
		var filteredByGatingHooks []*snapstate.RefreshCandidate
		for _, cand := range candidates {
			seenSnaps[cand.InstanceName()] = true
			if cand.InstanceName() == "snap-a" {
				continue
			}
			filteredByGatingHooks = append(filteredByGatingHooks, cand)
		}
		c.Check(seenSnaps, DeepEquals, map[string]bool{
			"snap-a":      true,
			"base-snap-b": true,
		})
		return filteredByGatingHooks, nil
	})
	defer restore()

	snapstate.ReplaceStore(s.state, &autoRefreshGatingStore{
		fakeStore: s.fakeStore,
		refreshedSnaps: []*snap.Info{
			{
				Architectures: []string{"all"},
				SnapType:      snap.TypeApp,
				SideInfo: snap.SideInfo{
					RealName: "snap-a",
					Revision: snap.R(8),
				},
			}, {
				Architectures: []string{"all"},
				SnapType:      snap.TypeBase,
				SideInfo: snap.SideInfo{
					RealName: "base-snap-b",
					Revision: snap.R(3),
				},
			},
		}})

	mockInstalledSnap(c, s.state, snapAyaml, useHook)
	mockInstalledSnap(c, s.state, snapByaml, useHook)
	mockInstalledSnap(c, s.state, baseSnapByaml, noHook)

	snapstate.MockSnapReadInfo(fakeReadInfo)

	restoreModel := snapstatetest.MockDeviceModel(DefaultModel())
	defer restoreModel()

	names, tss, err := snapstate.AutoRefreshPhase1(context.TODO(), st, "")
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"base-snap-b", "snap-a"})

	chg := s.state.NewChange("refresh", "...")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	s.settle(c)

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Check(chg.Err(), IsNil)

	expected := []string{
		"conditional-auto-refresh",
		"run-hook [snap-a;gate-auto-refresh]",
		// snap-b hook is triggered because of base-snap-b refresh
		"run-hook [snap-b;gate-auto-refresh]",
		"prerequisites",
		"download-snap",
		"validate-snap",
		"mount-snap",
		"run-hook [base-snap-b;pre-refresh]",
		"stop-snap-services",
		"remove-aliases",
		"unlink-current-snap",
		"copy-snap-data",
		"setup-profiles",
		"link-snap",
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"run-hook [base-snap-b;post-refresh]",
		"start-snap-services",
		"cleanup",
		"run-hook [base-snap-b;check-health]",
		"check-rerefresh",
	}
	tasks := chg.Tasks()
	verifyPhasedAutorefreshTasks(c, tasks, expected)
	// no re-refresh for snap-a because it was held.
	c.Check(tasks[len(tasks)-1].Summary(), Equals, `Monitoring snap "base-snap-b" to determine whether extra refresh steps are required`)

	// only snap-a remains in refresh-candidates because it was held;
	// base-snap-b got pruned (was refreshed).
	var candidates map[string]*snapstate.RefreshCandidate
	c.Assert(st.Get("refresh-candidates", &candidates), IsNil)
	c.Assert(candidates, HasLen, 1)
	c.Check(candidates["snap-a"], NotNil)
}

func (s *snapmgrTestSuite) TestAutoRefreshForGatingSnapErrorAutoRefreshInProgress(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("auto-refresh", "...")
	task := st.NewTask("foo", "...")
	chg.AddTask(task)

	c.Assert(snapstate.AutoRefreshForGatingSnap(st, "snap-a"), ErrorMatches, `there is an auto-refresh in progress`)
}

func (s *snapmgrTestSuite) TestAutoRefreshForGatingSnapErrorNothingHeld(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	c.Assert(snapstate.AutoRefreshForGatingSnap(st, "snap-a"), ErrorMatches, `no snaps are held by snap "snap-a"`)
}

func (s *autorefreshGatingSuite) TestAutoRefreshForGatingSnap(c *C) {
	s.store.refreshedSnaps = []*snap.Info{{
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			RealName: "snap-a",
			Revision: snap.R(8),
		},
	}, {
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		Base:          "base-snap-b",
		SideInfo: snap.SideInfo{
			RealName: "snap-b",
			Revision: snap.R(2),
		},
	}, {
		Architectures: []string{"all"},
		SnapType:      snap.TypeBase,
		SideInfo: snap.SideInfo{
			RealName: "base-snap-b",
			Revision: snap.R(3),
		},
	}}

	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, s.state, snapAyaml, useHook)
	mockInstalledSnap(c, s.state, snapByaml, useHook)
	mockInstalledSnap(c, s.state, baseSnapByaml, noHook)

	restore := snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	// pretend some snaps are held
	_, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-b", 0, "base-snap-b")
	c.Assert(err, IsNil)
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", 0, "snap-a")
	c.Assert(err, IsNil)

	lastRefreshTime := time.Now().Add(-99 * time.Hour)
	st.Set("last-refresh", lastRefreshTime)

	// pretend that snap-b triggers auto-refresh (by calling snapctl refresh --proceed)
	c.Assert(snapstate.AutoRefreshForGatingSnap(st, "snap-b"), IsNil)

	changes := st.Changes()
	c.Assert(changes, HasLen, 1)
	chg := changes[0]
	c.Assert(chg.Kind(), Equals, "auto-refresh")
	c.Check(chg.Summary(), Equals, `Auto-refresh snaps "base-snap-b", "snap-b"`)
	var snapNames []string
	var apiData map[string]interface{}
	c.Assert(chg.Get("snap-names", &snapNames), IsNil)
	c.Check(snapNames, DeepEquals, []string{"base-snap-b", "snap-b"})
	c.Assert(chg.Get("api-data", &apiData), IsNil)
	c.Check(apiData, DeepEquals, map[string]interface{}{
		"snap-names": []interface{}{"base-snap-b", "snap-b"},
	})

	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 2)
	conditionalRefreshTask := tasks[0]
	checkGatingTask(c, conditionalRefreshTask, map[string]*snapstate.RefreshCandidate{
		"base-snap-b": {
			SnapSetup: snapstate.SnapSetup{
				Type:      "base",
				PlugsOnly: true,
				Flags: snapstate.Flags{
					IsAutoRefresh: true,
				},
				SideInfo: &snap.SideInfo{
					RealName: "base-snap-b",
					Revision: snap.R(3),
				},
				DownloadInfo: &snap.DownloadInfo{},
			},
		},
		"snap-b": {
			SnapSetup: snapstate.SnapSetup{
				Type:      "app",
				Base:      "base-snap-b",
				PlugsOnly: true,
				Flags: snapstate.Flags{
					IsAutoRefresh: true,
				},
				SideInfo: &snap.SideInfo{
					RealName: "snap-b",
					Revision: snap.R(2),
				},
				DownloadInfo: &snap.DownloadInfo{},
			},
		},
	})

	// the gate-auto-refresh hook task for snap-b is present
	c.Check(tasks[1].Kind(), Equals, "run-hook")
	var hs hookstate.HookSetup
	c.Assert(tasks[1].Get("hook-setup", &hs), IsNil)
	c.Check(hs.Hook, Equals, "gate-auto-refresh")
	c.Check(hs.Snap, Equals, "snap-b")
	c.Check(hs.Optional, Equals, true)

	// last-refresh wasn't modified
	var lr time.Time
	st.Get("last-refresh", &lr)
	c.Check(lr.Equal(lastRefreshTime), Equals, true)
}

func (s *autorefreshGatingSuite) TestAutoRefreshForGatingSnapMoreAffectedSnaps(c *C) {
	s.store.refreshedSnaps = []*snap.Info{{
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			RealName: "snap-a",
			Revision: snap.R(8),
		},
	}, {
		Architectures: []string{"all"},
		SnapType:      snap.TypeBase,
		SideInfo: snap.SideInfo{
			RealName: "base-snap-b",
			Revision: snap.R(3),
		},
	}, {
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		Base:          "base-snap-b",
		SideInfo: snap.SideInfo{
			RealName: "snap-b",
			Revision: snap.R(2),
		},
	}}

	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, s.state, snapAyaml, useHook)
	mockInstalledSnap(c, s.state, snapByaml, useHook)
	mockInstalledSnap(c, s.state, snapBByaml, useHook)
	mockInstalledSnap(c, s.state, baseSnapByaml, noHook)

	restore := snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	// pretend snap-b holds base-snap-b.
	_, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-b", 0, "base-snap-b")
	c.Assert(err, IsNil)

	// pretend that snap-b triggers auto-refresh (by calling snapctl refresh --proceed)
	c.Assert(snapstate.AutoRefreshForGatingSnap(st, "snap-b"), IsNil)

	changes := st.Changes()
	c.Assert(changes, HasLen, 1)
	chg := changes[0]
	c.Assert(chg.Kind(), Equals, "auto-refresh")
	c.Check(chg.Summary(), Equals, `Auto-refresh snaps "base-snap-b", "snap-b"`)
	var snapNames []string
	var apiData map[string]interface{}
	c.Assert(chg.Get("snap-names", &snapNames), IsNil)
	c.Check(snapNames, DeepEquals, []string{"base-snap-b", "snap-b"})
	c.Assert(chg.Get("api-data", &apiData), IsNil)
	c.Check(apiData, DeepEquals, map[string]interface{}{
		"snap-names": []interface{}{"base-snap-b", "snap-b"},
	})

	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 3)
	conditionalRefreshTask := tasks[0]
	checkGatingTask(c, conditionalRefreshTask, map[string]*snapstate.RefreshCandidate{
		"base-snap-b": {
			SnapSetup: snapstate.SnapSetup{
				Type:      "base",
				PlugsOnly: true,
				Flags: snapstate.Flags{
					IsAutoRefresh: true,
				},
				SideInfo: &snap.SideInfo{
					RealName: "base-snap-b",
					Revision: snap.R(3),
				},
				DownloadInfo: &snap.DownloadInfo{},
			},
		},
		"snap-b": {
			SnapSetup: snapstate.SnapSetup{
				Type:      "app",
				Base:      "base-snap-b",
				PlugsOnly: true,
				Flags: snapstate.Flags{
					IsAutoRefresh: true,
				},
				SideInfo: &snap.SideInfo{
					RealName: "snap-b",
					Revision: snap.R(2),
				},
				DownloadInfo: &snap.DownloadInfo{},
			},
		},
	})

	seenSnaps := make(map[string]bool)

	// check that the gate-auto-refresh hooks are run.
	// snap-bb's hook is triggered because it is affected by base-snap-b refresh
	// (and intersects with affecting snap of snap-b). Note, snap-a is not here
	// because it is not affected by snaps affecting snap-b.
	for i := 1; i <= 2; i++ {
		c.Assert(tasks[i].Kind(), Equals, "run-hook")
		var hs hookstate.HookSetup
		c.Assert(tasks[i].Get("hook-setup", &hs), IsNil)
		c.Check(hs.Hook, Equals, "gate-auto-refresh")
		c.Check(hs.Optional, Equals, true)
		seenSnaps[hs.Snap] = true
	}
	c.Check(seenSnaps, DeepEquals, map[string]bool{
		"snap-b":  true,
		"snap-bb": true,
	})
}

func (s *autorefreshGatingSuite) TestAutoRefreshForGatingSnapNoCandidatesAnymore(c *C) {
	// only snap-a will have a refresh available
	s.store.refreshedSnaps = []*snap.Info{{
		Architectures: []string{"all"},
		SnapType:      snap.TypeApp,
		SideInfo: snap.SideInfo{
			RealName: "snap-a",
			Revision: snap.R(8),
		},
	}}

	logbuf, restoreLogger := logger.MockLogger()
	defer restoreLogger()

	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, s.state, snapAyaml, useHook)
	mockInstalledSnap(c, s.state, snapByaml, useHook)
	mockInstalledSnap(c, s.state, baseSnapByaml, noHook)

	restore := snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	// pretend some snaps are held
	_, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-b", 0, "base-snap-b")
	c.Assert(err, IsNil)
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", 0, "snap-a")
	c.Assert(err, IsNil)

	// pretend that snap-b triggers auto-refresh (by calling snapctl refresh --proceed)
	c.Assert(snapstate.AutoRefreshForGatingSnap(st, "snap-b"), IsNil)
	c.Assert(st.Changes(), HasLen, 0)

	// but base-snap-b has no update anymore.
	c.Check(logbuf.String(), testutil.Contains, `auto-refresh: all snaps previously held by "snap-b" are up-to-date`)
}

func (s *autorefreshGatingSuite) TestHoldRefreshesBySystem(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	timeNow, err := time.Parse(time.RFC3339, "2021-05-10T10:00:00Z")
	c.Assert(err, IsNil)
	restore := snapstate.MockTimeNow(func() time.Time {
		return timeNow
	})
	defer restore()

	mockInstalledSnap(c, st, snapAyaml, false)

	// advance time 100 years into the future
	hold := time.Hour * 24 * 3
	// holding self for 3 days
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", hold, "snap-a")
	c.Assert(err, IsNil)

	// the user holds the snap for a long time
	err = snapstate.HoldRefreshesBySystem(st, snapstate.HoldGeneral, "9999-01-01T00:00:00Z", []string{"snap-a"})
	c.Assert(err, IsNil)

	var gating map[string]map[string]*snapstate.HoldState
	c.Assert(st.Get("snaps-hold", &gating), IsNil)

	holdstate := gating["snap-a"]["system"]
	firstTime, untilTime := holdstate.FirstHeld, holdstate.HoldUntil
	c.Check(firstTime.Equal(timeNow), Equals, true)
	// if the supplied hold time exceeds the currentTime + maxDuration, it's truncated
	c.Check(untilTime.Equal(timeNow.Add(snapstate.MaxDuration)), Equals, true)

	holdstate = gating["snap-a"]["snap-a"]
	firstTime, untilTime = holdstate.FirstHeld, holdstate.HoldUntil
	c.Check(firstTime.Equal(timeNow), Equals, true)
	snapAUntilTime, err := time.Parse(time.RFC3339, "2021-05-13T10:00:00Z")
	c.Assert(err, IsNil)
	c.Check(untilTime.Equal(snapAUntilTime), Equals, true)
}

func (s *autorefreshGatingSuite) TestHoldRefreshesBySystemFailsIfNotInstalled(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, st, snapAyaml, false)

	err := snapstate.HoldRefreshesBySystem(st, snapstate.HoldAutoRefresh, "3000-01-01T00:00:00Z", []string{"snap-a", "snap-b"})
	c.Assert(err, ErrorMatches, `snap "snap-b" is not installed`)

	var gating map[string]map[string]*snapstate.HoldState
	c.Assert(st.Get("snaps-hold", &gating), ErrorMatches, `no state entry for key \"snaps-hold\"`)
}

func (s *autorefreshGatingSuite) TestHoldLevels(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, st, snapAyaml, false)
	mockInstalledSnap(c, st, snapByaml, false)
	mockInstalledSnap(c, st, snapCyaml, false)
	mockInstalledSnap(c, st, snapDyaml, false)
	mockInstalledSnap(c, st, snapEyaml, false)
	mockInstalledSnap(c, st, snapFyaml, false)

	mockLastRefreshed(c, st, "2021-05-09T10:00:00Z", "snap-b", "snap-c", "snap-d", "snap-e", "snap-f")

	restore := snapstate.MockTimeNow(func() time.Time {
		t, err := time.Parse(time.RFC3339, "2021-05-10T10:00:00Z")
		c.Assert(err, IsNil)
		return t
	})
	defer restore()

	err := snapstate.HoldRefreshesBySystem(st, snapstate.HoldAutoRefresh, "forever", []string{"snap-b", "snap-c"})
	c.Assert(err, IsNil)
	_, err = snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-d", 0, "snap-c")
	c.Assert(err, IsNil)
	// holding self
	_, err = snapstate.HoldRefresh(st, snapstate.HoldGeneral, "snap-d", time.Hour*24*4, "snap-d")
	c.Assert(err, IsNil)
	err = snapstate.HoldRefreshesBySystem(st, snapstate.HoldGeneral, "forever", []string{"snap-e", "snap-f"})
	c.Assert(err, IsNil)

	held, err := snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(held, testutil.DeepUnsortedMatches, map[string][]string{"snap-b": {"system"}, "snap-c": {"snap-d", "system"}, "snap-d": {"snap-d"}, "snap-e": {"system"}, "snap-f": {"system"}})

	held, err = snapstate.HeldSnaps(st, snapstate.HoldGeneral)
	c.Assert(err, IsNil)
	c.Check(held, DeepEquals, map[string][]string{"snap-d": {"snap-d"}, "snap-e": {"system"}, "snap-f": {"system"}})
}

func (s *autorefreshGatingSuite) TestSnapsCanBeHeldForeverBySystem(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	timeNow := time.Now()
	restore := snapstate.MockTimeNow(func() time.Time {
		return timeNow
	})
	defer restore()

	mockInstalledSnap(c, st, snapAyaml, false)

	// the user holds the snap for as long as possible
	_, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "system", snapstate.MaxDuration, "snap-a")
	c.Assert(err, IsNil)

	// advance time 100 years into the future
	timeNow = timeNow.Add(100 * 365 * 24 * time.Hour)

	gatedSnaps, err := snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(gatedSnaps, DeepEquals, map[string][]string{"snap-a": {"system"}})
}

func (s *autorefreshGatingSuite) TestSnapsNotHeldForeverBySnaps(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, st, snapAyaml, false)

	var snapst snapstate.SnapState
	snapstate.Get(st, "snap-a", &snapst)

	longAgo, err := time.Parse(time.RFC3339, "2021-05-10T10:00:00Z")
	c.Assert(err, IsNil)

	snapst.LastRefreshTime = &longAgo
	snapstate.Set(st, "snap-a", &snapst)

	// insert a long hold manually because HoldRefresh also has a cutoff
	gating := map[string]map[string]*snapstate.HoldState{
		"snap-a": {
			"snap-b": &snapstate.HoldState{
				FirstHeld: time.Now(),
				HoldUntil: time.Now().Add(snapstate.MaxDuration),
			},
		},
	}
	st.Set("snaps-hold", gating)

	gatedSnaps, err := snapstate.HeldSnaps(st, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(gatedSnaps, HasLen, 0)
}

func (s *autorefreshGatingSuite) TestLongestGatingHold(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	now := time.Now()
	restore := snapstate.MockTimeNow(func() time.Time {
		return now
	})
	defer restore()

	mockInstalledSnap(c, st, snapAyaml, false)
	mockInstalledSnap(c, st, snapCyaml, false)

	_, err := snapstate.HoldRefresh(st, snapstate.HoldGeneral, "snap-a", 7*24*time.Hour, "snap-a")
	c.Assert(err, IsNil)

	_, err = snapstate.HoldRefresh(st, snapstate.HoldGeneral, "snap-c", 24*time.Hour, "snap-a")
	c.Assert(err, IsNil)

	holdTime, err := snapstate.LongestGatingHold(st, "snap-a")
	c.Assert(err, IsNil)
	c.Assert(holdTime.Equal(now.Add(7*24*time.Hour)), Equals, true)
}

func (s *autorefreshGatingSuite) TestGatingHoldEmptyTimeOnHoldNotFound(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, st, snapAyaml, false)

	holdTime, err := snapstate.LongestGatingHold(st, "snap-a")
	c.Assert(err, IsNil)
	c.Assert(holdTime.IsZero(), Equals, true)
}

func (s *autorefreshGatingSuite) TestSystemHold(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	now := time.Now()
	restore := snapstate.MockTimeNow(func() time.Time {
		return now
	})
	defer restore()

	mockInstalledSnap(c, st, snapAyaml, false)

	err := snapstate.HoldRefreshesBySystem(st, snapstate.HoldGeneral, "forever", []string{"snap-a"})
	c.Assert(err, IsNil)

	holdTime, err := snapstate.SystemHold(st, "snap-a")
	c.Assert(err, IsNil)
	c.Assert(holdTime.Equal(now.Add(snapstate.MaxDuration)), Equals, true)
}

func (s *autorefreshGatingSuite) TestSystemHoldEmptyTimeOnHoldNotFound(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	mockInstalledSnap(c, st, snapAyaml, false)

	holdTime, err := snapstate.SystemHold(st, "snap-a")
	c.Assert(err, IsNil)
	c.Assert(holdTime.IsZero(), Equals, true)
}

func verifyPhasedAutorefreshTasks(c *C, tasks []*state.Task, expected []string) {
	c.Assert(len(tasks), Equals, len(expected))
	for i, t := range tasks {
		var got string
		if t.Kind() == "run-hook" {
			var hsup hookstate.HookSetup
			c.Assert(t.Get("hook-setup", &hsup), IsNil)
			got = fmt.Sprintf("%s [%s;%s]", t.Kind(), hsup.Snap, hsup.Hook)
		} else {
			got = t.Kind()
		}
		c.Assert(got, Equals, expected[i], Commentf("#%d", i))
	}
}

func (s *validationSetsSuite) TestAutoRefreshPhase1WithValidationSets(c *C) {
	var requiredRevision string
	restoreEnforcedValidationSets := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		vs := snapasserts.NewValidationSets()
		someSnap := map[string]interface{}{
			"id":       "yOqKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap",
			"presence": "required",
			"revision": requiredRevision,
		}
		snapC := map[string]interface{}{
			"id":       "aOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "snap-c",
			"presence": "required",
		}
		vsa1 := s.mockValidationSetAssert(c, "bar", "1", someSnap, snapC)
		vs.Add(vsa1.(*asserts.ValidationSet))
		return vs, nil
	})
	defer restoreEnforcedValidationSets()

	st := s.state
	st.Lock()
	defer st.Unlock()

	repo := interfaces.NewRepository()
	ifacerepo.Replace(st, repo)

	mockInstalledSnap(c, s.state, someSnap, noHook)
	mockInstalledSnap(c, s.state, someOtherSnap, noHook)
	mockInstalledSnap(c, s.state, snapCyaml, noHook)

	restore := snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	refreshedDate := fakeRevDateEpoch.AddDate(0, 0, 1)
	requiredRevision = "1"
	names, _, err := snapstate.AutoRefreshPhase1(context.TODO(), st, "")
	c.Assert(err, IsNil)
	// some-snap is already at the required revision 1, so not refreshed
	c.Check(names, DeepEquals, []string{"snap-c", "some-other-snap"})

	// check that refresh-candidates in the state were updated
	var candidates map[string]*snapstate.RefreshCandidate
	c.Assert(st.Get("refresh-candidates", &candidates), IsNil)
	c.Assert(candidates, HasLen, 2)
	c.Check(candidates["snap-c"], NotNil)
	c.Check(candidates["some-other-snap"], NotNil)
	c.Check(candidates["some-snap"], IsNil)
	c.Assert(s.fakeBackend.ops, HasLen, 3)
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:  "snap-c",
			SnapID:        "snap-c-id",
			Revision:      snap.R(1),
			Epoch:         snap.E("1*"),
			RefreshedDate: refreshedDate,
		}, {
			InstanceName:  "some-other-snap",
			SnapID:        "some-other-snap-id",
			Revision:      snap.R(1),
			Epoch:         snap.E("1*"),
			RefreshedDate: refreshedDate,
		}, {
			InstanceName:  "some-snap",
			SnapID:        "some-snap-id",
			Revision:      snap.R(1),
			Epoch:         snap.E("1*"),
			RefreshedDate: refreshedDate,
		}},
	}, {
		op: "storesvc-snap-action:action",
		action: store.SnapAction{
			Action:         "refresh",
			InstanceName:   "snap-c",
			SnapID:         "snap-c-id",
			ValidationSets: []snapasserts.ValidationSetKey{"16/foo/bar/1"},
		},
		revno: snap.R(11),
	}, {
		op: "storesvc-snap-action:action",
		action: store.SnapAction{
			Action:       "refresh",
			InstanceName: "some-other-snap",
			SnapID:       "some-other-snap-id",
		},
		revno: snap.R(11),
	}})

	s.fakeBackend.ops = nil
	requiredRevision = "11"
	names, _, err = snapstate.AutoRefreshPhase1(context.TODO(), st, "")
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"snap-c", "some-other-snap", "some-snap"})

	// check that refresh-candidates in the state were updated
	var candidates2 map[string]*snapstate.RefreshCandidate
	c.Assert(st.Get("refresh-candidates", &candidates2), IsNil)
	c.Assert(candidates2, HasLen, 3)
	c.Check(candidates2["snap-c"], NotNil)
	c.Check(candidates2["some-snap"], NotNil)
	c.Check(candidates["some-other-snap"], NotNil)
	c.Assert(s.fakeBackend.ops, HasLen, 4)
	c.Check(s.fakeBackend.ops[1], DeepEquals, fakeOp{
		op: "storesvc-snap-action:action",
		action: store.SnapAction{
			Action:         "refresh",
			InstanceName:   "snap-c",
			SnapID:         "snap-c-id",
			ValidationSets: []snapasserts.ValidationSetKey{"16/foo/bar/1"},
		},
		revno: snap.R(11),
	})
	c.Check(s.fakeBackend.ops[3], DeepEquals, fakeOp{
		op: "storesvc-snap-action:action",
		action: store.SnapAction{
			Action:         "refresh",
			InstanceName:   "some-snap",
			SnapID:         "some-snap-id",
			ValidationSets: []snapasserts.ValidationSetKey{"16/foo/bar/1"},
			Revision:       snap.R(11),
		},
		revno: snap.R(11),
	})
}
