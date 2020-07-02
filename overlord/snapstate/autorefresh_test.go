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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/timeutil"
)

type autoRefreshStore struct {
	storetest.Store

	ops []string

	err error
}

func (r *autoRefreshStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	if assertQuery != nil {
		panic("no assertion query support")
	}
	if !opts.IsAutoRefresh {
		panic("AutoRefresh snap action did not set IsAutoRefresh flag")
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
	return nil, nil, r.err
}

type autoRefreshTestSuite struct {
	state *state.State

	store *autoRefreshStore

	restore func()
}

var _ = Suite(&autoRefreshTestSuite{})

func (s *autoRefreshTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	s.state = state.New(nil)

	s.store = &autoRefreshStore{}

	s.state.Lock()
	defer s.state.Unlock()
	snapstate.ReplaceStore(s.state, s.store)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		},
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})

	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	snapstate.IsOnMeteredConnection = func() (bool, error) { return false, nil }

	s.state.Set("seeded", true)
	s.state.Set("seed-time", time.Now())
	s.state.Set("refresh-privacy-key", "privacy-key")
	s.restore = snapstatetest.MockDeviceModel(DefaultModel())
}

func (s *autoRefreshTestSuite) TearDownTest(c *C) {
	snapstate.CanAutoRefresh = nil
	snapstate.AutoAliases = nil
	s.restore()
	dirs.SetRootDir("")
}

func (s *autoRefreshTestSuite) TestLastRefresh(c *C) {
	// this does an immediate refresh

	af := snapstate.NewAutoRefresh(s.state)
	err := af.Ensure()
	c.Check(err, IsNil)
	c.Check(s.store.ops, DeepEquals, []string{"list-refresh"})

	var lastRefresh time.Time
	s.state.Lock()
	s.state.Get("last-refresh", &lastRefresh)
	s.state.Unlock()
	c.Check(lastRefresh.Year(), Equals, time.Now().Year())
}

func (s *autoRefreshTestSuite) TestLastRefreshRefreshManaged(c *C) {
	snapstate.CanManageRefreshes = func(st *state.State) bool {
		return true
	}
	defer func() { snapstate.CanManageRefreshes = nil }()

	logbuf, restore := logger.MockLogger()
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	for _, t := range []struct {
		conf   string
		legacy bool
	}{
		{"refresh.timer", false},
		{"refresh.schedule", true},
	} {
		tr := config.NewTransaction(s.state)
		tr.Set("core", t.conf, "managed")
		tr.Commit()

		af := snapstate.NewAutoRefresh(s.state)
		s.state.Unlock()
		err := af.Ensure()
		s.state.Lock()
		c.Check(err, IsNil)
		c.Check(s.store.ops, HasLen, 0)

		refreshScheduleStr, legacy, err := af.RefreshSchedule()
		c.Check(refreshScheduleStr, Equals, "managed")
		c.Check(legacy, Equals, t.legacy)
		c.Check(err, IsNil)

		c.Check(af.NextRefresh(), DeepEquals, time.Time{})

		count := strings.Count(logbuf.String(),
			": refresh is managed via the snapd-control interface\n")
		c.Check(count, Equals, 1, Commentf("too many occurrences:\n%s", logbuf.String()))

		// ensure clean config for the next run
		s.state.Set("config", nil)
		logbuf.Reset()
	}
}

func (s *autoRefreshTestSuite) TestRefreshManagedTimerWins(c *C) {
	snapstate.CanManageRefreshes = func(st *state.State) bool {
		return true
	}
	defer func() { snapstate.CanManageRefreshes = nil }()

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	// the "refresh.timer" setting always takes precedence over
	// refresh.schedule
	tr.Set("core", "refresh.timer", "00:00-12:00")
	tr.Set("core", "refresh.schedule", "managed")
	tr.Commit()

	af := snapstate.NewAutoRefresh(s.state)
	s.state.Unlock()
	err := af.Ensure()
	s.state.Lock()
	c.Check(err, IsNil)
	c.Check(s.store.ops, DeepEquals, []string{"list-refresh"})

	refreshScheduleStr, legacy, err := af.RefreshSchedule()
	c.Check(refreshScheduleStr, Equals, "00:00-12:00")
	c.Check(legacy, Equals, false)
	c.Check(err, IsNil)
}

func (s *autoRefreshTestSuite) TestRefreshManagedDenied(c *C) {
	canManageCalled := false
	snapstate.CanManageRefreshes = func(st *state.State) bool {
		canManageCalled = true
		// always deny
		return false
	}
	defer func() { snapstate.CanManageRefreshes = nil }()

	logbuf, restore := logger.MockLogger()
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	for _, conf := range []string{"refresh.timer", "refresh.schedule"} {
		tr := config.NewTransaction(s.state)
		tr.Set("core", conf, "managed")
		tr.Commit()

		af := snapstate.NewAutoRefresh(s.state)
		for i := 0; i < 2; i++ {
			c.Logf("ensure iteration: %v", i)
			s.state.Unlock()
			err := af.Ensure()
			s.state.Lock()
			c.Check(err, IsNil)
			c.Check(s.store.ops, DeepEquals, []string{"list-refresh"})

			refreshScheduleStr, _, err := af.RefreshSchedule()
			c.Check(refreshScheduleStr, Equals, snapstate.DefaultRefreshSchedule)
			c.Check(err, IsNil)
			c.Check(canManageCalled, Equals, true)
			count := strings.Count(logbuf.String(),
				": managed refresh schedule denied, no properly configured snapd-control\n")
			c.Check(count, Equals, 1, Commentf("too many occurrences:\n%s", logbuf.String()))

			canManageCalled = false
		}

		// ensure clean config for the next run
		s.state.Set("config", nil)
		logbuf.Reset()
		canManageCalled = false
	}
}

func (s *autoRefreshTestSuite) TestLastRefreshNoRefreshNeeded(c *C) {
	s.state.Lock()
	s.state.Set("last-refresh", time.Now())
	s.state.Unlock()

	af := snapstate.NewAutoRefresh(s.state)
	err := af.Ensure()
	c.Check(err, IsNil)
	c.Check(s.store.ops, HasLen, 0)
}

func (s *autoRefreshTestSuite) TestRefreshBackoff(c *C) {
	s.store.err = fmt.Errorf("random store error")
	af := snapstate.NewAutoRefresh(s.state)
	err := af.Ensure()
	c.Check(err, ErrorMatches, "random store error")
	c.Check(s.store.ops, HasLen, 1)

	// override next refresh to be here already
	now := time.Now()
	snapstate.MockNextRefresh(af, now)

	// call ensure again, our back-off will prevent the store from
	// being hit again
	err = af.Ensure()
	c.Check(err, IsNil)
	c.Check(s.store.ops, HasLen, 1)

	// nextRefresh unchanged
	c.Check(af.NextRefresh().Equal(now), Equals, true)

	// fake that the retryRefreshDelay is over
	restore := snapstate.MockRefreshRetryDelay(1 * time.Millisecond)
	defer restore()
	time.Sleep(10 * time.Millisecond)

	// ensure hits the store again
	err = af.Ensure()
	c.Check(err, ErrorMatches, "random store error")
	c.Check(s.store.ops, HasLen, 2)

	// nextRefresh now zero
	c.Check(af.NextRefresh().IsZero(), Equals, true)
	// set it to something in the future
	snapstate.MockNextRefresh(af, time.Now().Add(time.Minute))

	// nothing really happens yet: the previous autorefresh failed
	// but it still counts as having tried to autorefresh
	err = af.Ensure()
	c.Check(err, IsNil)
	c.Check(s.store.ops, HasLen, 2)

	// pretend the time for next refresh is here
	snapstate.MockNextRefresh(af, time.Now())
	// including the wait for the retryRefreshDelay backoff
	time.Sleep(10 * time.Millisecond)

	// now yes it happens again
	err = af.Ensure()
	c.Check(err, ErrorMatches, "random store error")
	c.Check(s.store.ops, HasLen, 3)
	// and not *again* again
	err = af.Ensure()
	c.Check(err, IsNil)
	c.Check(s.store.ops, HasLen, 3)

	c.Check(s.store.ops, DeepEquals, []string{"list-refresh", "list-refresh", "list-refresh"})
}

func (s *autoRefreshTestSuite) TestRefreshPersistentError(c *C) {
	// fake that the retryRefreshDelay is over
	restore := snapstate.MockRefreshRetryDelay(1 * time.Millisecond)
	defer restore()

	initialLastRefresh := time.Now().Add(-12 * time.Hour)
	s.state.Lock()
	s.state.Set("last-refresh", initialLastRefresh)
	s.state.Unlock()

	s.store.err = &httputil.PerstistentNetworkError{Err: fmt.Errorf("error")}
	af := snapstate.NewAutoRefresh(s.state)
	err := af.Ensure()
	c.Check(err, ErrorMatches, "persistent network error: error")
	c.Check(s.store.ops, HasLen, 1)

	// last-refresh time remains untouched
	var lastRefresh time.Time
	s.state.Lock()
	s.state.Get("last-refresh", &lastRefresh)
	s.state.Unlock()
	c.Check(lastRefresh.Format(time.RFC3339), Equals, initialLastRefresh.Format(time.RFC3339))

	s.store.err = nil
	time.Sleep(10 * time.Millisecond)

	// call ensure again, refresh should be attempted again
	err = af.Ensure()
	c.Check(err, IsNil)
	c.Check(s.store.ops, HasLen, 2)
}

func (s *autoRefreshTestSuite) TestDefaultScheduleIsRandomized(c *C) {
	schedule, err := timeutil.ParseSchedule(snapstate.DefaultRefreshSchedule)
	c.Assert(err, IsNil)

	for _, sched := range schedule {
		for _, span := range sched.ClockSpans {
			c.Check(span.Start == span.End, Equals, false,
				Commentf("clock span %v is a single time, expected an actual span", span))
			c.Check(span.Spread, Equals, true,
				Commentf("clock span %v is not randomized", span))
		}
	}
}

func (s *autoRefreshTestSuite) TestLastRefreshRefreshHold(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	t0 := time.Now()
	s.state.Set("last-refresh", t0.Add(-12*time.Hour))

	holdTime := t0.Add(5 * time.Minute)
	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.hold", holdTime)
	tr.Commit()

	af := snapstate.NewAutoRefresh(s.state)
	s.state.Unlock()
	err := af.Ensure()
	s.state.Lock()
	c.Check(err, IsNil)

	// no refresh
	c.Check(s.store.ops, HasLen, 0)

	// hold still kept
	tr = config.NewTransaction(s.state)
	var t1 time.Time
	err = tr.Get("core", "refresh.hold", &t1)
	c.Assert(err, IsNil)
	c.Check(t1.Equal(holdTime), Equals, true)
}

func (s *autoRefreshTestSuite) TestLastRefreshRefreshHoldExpired(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	t0 := time.Now()
	s.state.Set("last-refresh", t0.Add(-12*time.Hour))

	holdTime := t0.Add(-5 * time.Minute)
	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.hold", holdTime)
	tr.Commit()

	af := snapstate.NewAutoRefresh(s.state)
	s.state.Unlock()
	err := af.Ensure()
	s.state.Lock()
	c.Check(err, IsNil)

	// refresh happened
	c.Check(s.store.ops, DeepEquals, []string{"list-refresh"})

	var lastRefresh time.Time
	s.state.Get("last-refresh", &lastRefresh)
	c.Check(lastRefresh.Year(), Equals, time.Now().Year())

	// hold was reset
	tr = config.NewTransaction(s.state)
	var t1 time.Time
	err = tr.Get("core", "refresh.hold", &t1)
	c.Assert(config.IsNoOption(err), Equals, true)
}

func (s *autoRefreshTestSuite) TestLastRefreshRefreshHoldExpiredReschedule(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	t0 := time.Now()
	s.state.Set("last-refresh", t0.Add(-12*time.Hour))

	holdTime := t0.Add(-1 * time.Minute)
	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.hold", holdTime)

	nextRefresh := t0.Add(5 * time.Minute).Truncate(time.Minute)
	schedule := fmt.Sprintf("%02d:%02d-%02d:59", nextRefresh.Hour(), nextRefresh.Minute(), nextRefresh.Hour())
	tr.Set("core", "refresh.timer", schedule)
	tr.Commit()

	af := snapstate.NewAutoRefresh(s.state)
	snapstate.MockLastRefreshSchedule(af, schedule)
	snapstate.MockNextRefresh(af, holdTime.Add(-2*time.Minute))

	s.state.Unlock()
	err := af.Ensure()
	s.state.Lock()
	c.Check(err, IsNil)

	// refresh did not happen yet
	c.Check(s.store.ops, HasLen, 0)

	// hold was reset
	tr = config.NewTransaction(s.state)
	var t1 time.Time
	err = tr.Get("core", "refresh.hold", &t1)
	c.Assert(config.IsNoOption(err), Equals, true)

	// check next refresh
	nextRefresh1 := af.NextRefresh()
	c.Check(nextRefresh1.Before(nextRefresh), Equals, false)
}

func (s *autoRefreshTestSuite) TestEffectiveRefreshHold(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// assume no seed-time
	s.state.Set("seed-time", nil)

	af := snapstate.NewAutoRefresh(s.state)

	t0, err := af.EffectiveRefreshHold()
	c.Assert(err, IsNil)
	c.Check(t0.IsZero(), Equals, true)

	holdTime := time.Now()
	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.hold", holdTime)
	tr.Commit()

	seedTime := holdTime.Add(-70 * 24 * time.Hour)
	s.state.Set("seed-time", seedTime)

	t1, err := af.EffectiveRefreshHold()
	c.Assert(err, IsNil)
	c.Check(t1.Equal(seedTime.Add(60*24*time.Hour)), Equals, true)

	lastRefresh := holdTime.Add(-65 * 24 * time.Hour)
	s.state.Set("last-refresh", lastRefresh)

	t1, err = af.EffectiveRefreshHold()
	c.Assert(err, IsNil)
	c.Check(t1.Equal(lastRefresh.Add(60*24*time.Hour)), Equals, true)

	s.state.Set("last-refresh", holdTime.Add(-6*time.Hour))
	t1, err = af.EffectiveRefreshHold()
	c.Assert(err, IsNil)
	c.Check(t1.Equal(holdTime), Equals, true)
}

func (s *autoRefreshTestSuite) TestEnsureLastRefreshAnchor(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	// set hold => no refreshes
	t0 := time.Now()
	holdTime := t0.Add(1 * time.Hour)
	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.hold", holdTime)
	tr.Commit()

	// with seed-time
	s.state.Set("seed-time", t0.Add(-1*time.Hour))

	af := snapstate.NewAutoRefresh(s.state)
	s.state.Unlock()
	err := af.Ensure()
	s.state.Lock()
	c.Check(err, IsNil)
	// no refresh
	c.Check(s.store.ops, HasLen, 0)
	lastRefresh, err := af.LastRefresh()
	c.Assert(err, IsNil)
	c.Check(lastRefresh.IsZero(), Equals, true)

	// no seed-time
	s.state.Set("seed-time", nil)

	// fallback to time of executable
	st, err := os.Stat("/proc/self/exe")
	c.Assert(err, IsNil)
	exeTime := st.ModTime()

	af = snapstate.NewAutoRefresh(s.state)
	s.state.Unlock()
	err = af.Ensure()
	s.state.Lock()
	c.Check(err, IsNil)
	// no refresh
	c.Check(s.store.ops, HasLen, 0)
	lastRefresh, err = af.LastRefresh()
	c.Assert(err, IsNil)
	c.Check(lastRefresh.Equal(exeTime), Equals, true)

	// clear
	s.state.Set("last-refresh", nil)
	// use core last refresh time
	coreCurrent := filepath.Join(dirs.SnapMountDir, "core", "current")
	err = os.MkdirAll(coreCurrent, 0755)
	c.Assert(err, IsNil)
	st, err = os.Stat(coreCurrent)
	c.Assert(err, IsNil)
	coreRefreshed := st.ModTime()

	af = snapstate.NewAutoRefresh(s.state)
	s.state.Unlock()
	err = af.Ensure()
	s.state.Lock()
	c.Check(err, IsNil)
	// no refresh
	c.Check(s.store.ops, HasLen, 0)
	lastRefresh, err = af.LastRefresh()
	c.Assert(err, IsNil)
	c.Check(lastRefresh.Equal(coreRefreshed), Equals, true)
}

func (s *autoRefreshTestSuite) TestAtSeedPolicy(c *C) {
	r := release.MockOnClassic(false)
	defer r()

	s.state.Lock()
	defer s.state.Unlock()

	af := snapstate.NewAutoRefresh(s.state)

	// on core, does nothing
	err := af.AtSeed()
	c.Assert(err, IsNil)
	c.Check(af.NextRefresh().IsZero(), Equals, true)
	tr := config.NewTransaction(s.state)
	var t1 time.Time
	err = tr.Get("core", "refresh.hold", &t1)
	c.Check(config.IsNoOption(err), Equals, true)

	release.MockOnClassic(true)
	now := time.Now()
	// on classic it sets a refresh hold of 2h
	err = af.AtSeed()
	c.Assert(err, IsNil)
	c.Check(af.NextRefresh().IsZero(), Equals, false)
	tr = config.NewTransaction(s.state)
	err = tr.Get("core", "refresh.hold", &t1)
	c.Check(err, IsNil)
	c.Check(t1.Before(now.Add(2*time.Hour)), Equals, false)
	c.Check(t1.After(now.Add(2*time.Hour+5*time.Minute)), Equals, false)

	// nop
	err = af.AtSeed()
	c.Assert(err, IsNil)
	var t2 time.Time
	tr = config.NewTransaction(s.state)
	err = tr.Get("core", "refresh.hold", &t2)
	c.Check(err, IsNil)
	c.Check(t1.Equal(t2), Equals, true)
}

func (s *autoRefreshTestSuite) TestCanRefreshOnMetered(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	can, err := snapstate.CanRefreshOnMeteredConnection(s.state)
	c.Assert(can, Equals, true)
	c.Assert(err, Equals, nil)

	// enable holding refreshes when on metered connection
	tr := config.NewTransaction(s.state)
	err = tr.Set("core", "refresh.metered", "hold")
	c.Assert(err, IsNil)
	tr.Commit()

	can, err = snapstate.CanRefreshOnMeteredConnection(s.state)
	c.Assert(can, Equals, false)
	c.Assert(err, Equals, nil)

	// explicitly disable holding refreshes when on metered connection
	tr = config.NewTransaction(s.state)
	err = tr.Set("core", "refresh.metered", "")
	c.Assert(err, IsNil)
	tr.Commit()

	can, err = snapstate.CanRefreshOnMeteredConnection(s.state)
	c.Assert(can, Equals, true)
	c.Assert(err, Equals, nil)
}

func (s *autoRefreshTestSuite) TestRefreshOnMeteredConnIsMetered(c *C) {
	// pretend we're on metered connection
	revert := snapstate.MockIsOnMeteredConnection(func() (bool, error) {
		return true, nil
	})
	defer revert()

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.metered", "hold")
	tr.Commit()

	af := snapstate.NewAutoRefresh(s.state)

	s.state.Set("last-refresh", time.Now().Add(-5*24*time.Hour))
	s.state.Unlock()
	err := af.Ensure()
	s.state.Lock()
	c.Check(err, IsNil)
	// no refresh
	c.Check(s.store.ops, HasLen, 0)

	c.Check(af.NextRefresh(), DeepEquals, time.Time{})

	// last refresh over 60 days ago, new one is launched regardless of
	// connection being metered
	s.state.Set("last-refresh", time.Now().Add(-61*24*time.Hour))
	s.state.Unlock()
	err = af.Ensure()
	s.state.Lock()
	c.Check(err, IsNil)
	c.Check(s.store.ops, DeepEquals, []string{"list-refresh"})
}

func (s *autoRefreshTestSuite) TestRefreshOnMeteredConnNotMetered(c *C) {
	// pretend we're on non-metered connection
	revert := snapstate.MockIsOnMeteredConnection(func() (bool, error) {
		return false, nil
	})
	defer revert()

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.metered", "hold")
	tr.Commit()

	af := snapstate.NewAutoRefresh(s.state)

	s.state.Set("last-refresh", time.Now().Add(-5*24*time.Hour))
	s.state.Unlock()
	err := af.Ensure()
	s.state.Lock()
	c.Check(err, IsNil)
	c.Check(s.store.ops, DeepEquals, []string{"list-refresh"})
}

func (s *autoRefreshTestSuite) TestInhibitRefreshWithinInhibitWindow(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{RealName: "pkg", Revision: snap.R(1)}
	info := &snap.Info{SideInfo: *si}
	snapst := &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
	}
	err := snapstate.InhibitRefresh(s.state, snapst, info, func(si *snap.Info) error {
		return &snapstate.BusySnapError{SnapName: "pkg"}
	})
	c.Assert(err, ErrorMatches, `snap "pkg" has running apps or hooks`)

	pending, _ := s.state.PendingWarnings()
	c.Assert(pending, HasLen, 1)
	c.Check(pending[0].String(), Equals, `snap "pkg" is currently in use. Its refresh will be postponed for up to 7 days to wait for the snap to no longer be in use.`)
}

func (s *autoRefreshTestSuite) TestInhibitRefreshWarnsAndRefreshesWhenOverdue(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	instant := time.Now()
	pastInstant := instant.Add(-snapstate.MaxInhibition * 2)

	si := &snap.SideInfo{RealName: "pkg", Revision: snap.R(1)}
	info := &snap.Info{SideInfo: *si}
	snapst := &snapstate.SnapState{
		Sequence:             []*snap.SideInfo{si},
		Current:              si.Revision,
		RefreshInhibitedTime: &pastInstant,
	}
	err := snapstate.InhibitRefresh(s.state, snapst, info, func(si *snap.Info) error {
		return &snapstate.BusySnapError{SnapName: "pkg"}
	})
	c.Assert(err, IsNil)

	pending, _ := s.state.PendingWarnings()
	c.Assert(pending, HasLen, 1)
	c.Check(pending[0].String(), Equals, `snap "pkg" has been running for the maximum allowable 7 days since its refresh was postponed. It will now be refreshed.`)
}
