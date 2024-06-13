// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2024 Canonical Ltd
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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timeutil"
	userclient "github.com/snapcore/snapd/usersession/client"
)

type autoRefreshStore struct {
	storetest.Store

	ops []string

	err         error
	refreshable []*snap.Info

	snapActionOpsFunc func()
}

func (r *autoRefreshStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	// this is a bit of a hack to simulate race conditions where while the store
	// has unlocked the global state lock something else could come in and
	// change the auto-refresh hold
	if r.snapActionOpsFunc != nil {
		r.snapActionOpsFunc()
		return nil, nil, r.err
	}

	if assertQuery != nil {
		panic("no assertion query support")
	}
	if !opts.Scheduled {
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

	var res []store.SnapActionResult
	for _, rs := range r.refreshable {
		res = append(res, store.SnapActionResult{Info: rs})
	}

	return res, nil, r.err
}

type autoRefreshTestSuite struct {
	testutil.BaseTest
	state *state.State

	store *autoRefreshStore
}

var _ = Suite(&autoRefreshTestSuite{})

func (s *autoRefreshTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.state = state.New(nil)

	s.store = &autoRefreshStore{}

	s.AddCleanup(func() { s.store.snapActionOpsFunc = nil })

	s.state.Lock()
	defer s.state.Unlock()
	snapstate.ReplaceStore(s.state, s.store)

	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.state, repo)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		}),
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})

	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }
	s.AddCleanup(func() { snapstate.CanAutoRefresh = nil })
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	s.AddCleanup(func() { snapstate.AutoAliases = nil })
	snapstate.IsOnMeteredConnection = func() (bool, error) { return false, nil }

	s.state.Set("seeded", true)
	s.state.Set("seed-time", time.Now())
	s.state.Set("refresh-privacy-key", "privacy-key")
	s.AddCleanup(snapstatetest.MockDeviceModel(DefaultModel()))

	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		return nil, nil
	})
	s.AddCleanup(restore)
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

	s.store.err = &httputil.PersistentNetworkError{Err: fmt.Errorf("error")}
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

func (s *autoRefreshTestSuite) TestRefreshHoldForever(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	lastRefresh := time.Now().Add(-12 * time.Hour)
	s.state.Set("last-refresh", lastRefresh)

	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.hold", "forever")
	tr.Commit()

	af := snapstate.NewAutoRefresh(s.state)
	s.state.Unlock()
	err := af.Ensure()
	s.state.Lock()
	c.Check(err, IsNil)

	// no refresh
	c.Check(s.store.ops, HasLen, 0)

	var storedRefresh time.Time
	c.Assert(s.state.Get("last-refresh", &storedRefresh), IsNil)
	cmt := Commentf("expected %s but got %s instead", lastRefresh.Format(time.RFC3339), storedRefresh.Format(time.RFC3339))
	c.Check(storedRefresh.Equal(lastRefresh), Equals, true, cmt)

	var holdVal string
	err = tr.Get("core", "refresh.hold", &holdVal)
	c.Assert(err, IsNil)
	c.Check(holdVal, Equals, "forever")
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

func (s *autoRefreshTestSuite) TestLastRefreshRefreshHoldExpiredButResetWhileLockUnlocked(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	t0 := time.Now()
	twelveHoursAgo := t0.Add(-12 * time.Hour)
	fiveMinutesAgo := t0.Add(-5 * time.Minute)
	oneHourInFuture := t0.Add(time.Hour)
	s.state.Set("last-refresh", twelveHoursAgo)

	holdTime := fiveMinutesAgo
	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.hold", holdTime)
	tr.Commit()

	logbuf, restore := logger.MockLogger()
	defer restore()

	sent := false
	ch := make(chan struct{})
	// make the store snap action function trigger a background go routine to
	// change the held-time underneath the auto-refresh
	go func() {
		// wait to be triggered by the snap action ops func
		<-ch
		s.state.Lock()
		defer s.state.Unlock()

		// now change the refresh.hold time to be an hour in the future
		tr := config.NewTransaction(s.state)
		tr.Set("core", "refresh.hold", oneHourInFuture)
		tr.Commit()

		// trigger the snap action ops func to proceed
		ch <- struct{}{}
	}()

	s.store.snapActionOpsFunc = func() {
		// only need to send once, this will be invoked multiple times for
		// multiple snaps
		if !sent {
			ch <- struct{}{}
			sent = true
			// wait for a response to ensure that we block waiting for the new
			// refresh time to be committed in time for us to read it after
			// returning in this go routine
			<-ch
		}
	}

	af := snapstate.NewAutoRefresh(s.state)
	s.state.Unlock()
	err := af.Ensure()
	s.state.Lock()
	c.Check(err, IsNil)

	var lastRefresh time.Time
	s.state.Get("last-refresh", &lastRefresh)
	c.Check(lastRefresh.Year(), Equals, time.Now().Year())

	// hold was reset mid-way to a new value one hour into the future
	tr = config.NewTransaction(s.state)
	var t1 time.Time
	err = tr.Get("core", "refresh.hold", &t1)
	c.Assert(err, IsNil)

	// when traversing json through the core config transaction, there will be
	// different wall/monotonic clock times, we remove this ambiguity by
	// formatting as rfc3339 which will strip this negligible difference in time
	c.Assert(t1.Format(time.RFC3339), Equals, oneHourInFuture.Format(time.RFC3339))

	// we shouldn't have had a message about "all snaps are up to date", we
	// should have a message about being aborted mid way

	c.Assert(logbuf.String(), testutil.Contains, "Auto-refresh was delayed mid-way through launching, aborting to try again later")
	c.Assert(logbuf.String(), Not(testutil.Contains), "auto-refresh: all snaps are up-to-date")
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

func (s *autoRefreshTestSuite) TestEnsureRefreshHoldAtLeastZeroTimes(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// setup hold-time as time.Time{} and next-refresh as now to simulate real
	// console-conf-start situations
	t0 := time.Now()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.hold", time.Time{})
	tr.Commit()

	af := snapstate.NewAutoRefresh(s.state)
	snapstate.MockNextRefresh(af, t0)

	err := af.EnsureRefreshHoldAtLeast(time.Hour)
	c.Assert(err, IsNil)

	s.state.Unlock()
	err = af.Ensure()
	s.state.Lock()
	c.Check(err, IsNil)

	// refresh did not happen
	c.Check(s.store.ops, HasLen, 0)

	// hold is now more than an hour later than when the test started
	tr = config.NewTransaction(s.state)
	var t1 time.Time
	err = tr.Get("core", "refresh.hold", &t1)
	c.Assert(err, IsNil)

	// use After() == false here in case somehow the t0 + 1hr is exactly t1,
	// Before() and After() are false for the same time instants
	c.Assert(t0.Add(time.Hour).After(t1), Equals, false)
}

func (s *autoRefreshTestSuite) TestEnsureRefreshHoldAtLeast(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// setup last-refresh as happening a long time ago, and refresh-hold as
	// having been expired
	t0 := time.Now()
	s.state.Set("last-refresh", t0.Add(-12*time.Hour))

	holdTime := t0.Add(-1 * time.Minute)
	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.hold", holdTime)

	tr.Commit()

	af := snapstate.NewAutoRefresh(s.state)
	snapstate.MockNextRefresh(af, holdTime.Add(-2*time.Minute))

	err := af.EnsureRefreshHoldAtLeast(time.Hour)
	c.Assert(err, IsNil)

	s.state.Unlock()
	err = af.Ensure()
	s.state.Lock()
	c.Check(err, IsNil)

	// refresh did not happen
	c.Check(s.store.ops, HasLen, 0)

	// hold is now more than an hour later than when the test started
	tr = config.NewTransaction(s.state)
	var t1 time.Time
	err = tr.Get("core", "refresh.hold", &t1)
	c.Assert(err, IsNil)

	// use After() == false here in case somehow the t0 + 1hr is exactly t1,
	// Before() and After() are false for the same time instants
	c.Assert(t0.Add(time.Hour).After(t1), Equals, false)

	// setting it to a shorter time will not change it
	err = af.EnsureRefreshHoldAtLeast(30 * time.Minute)
	c.Assert(err, IsNil)

	// time is still equal to t1
	tr = config.NewTransaction(s.state)
	var t2 time.Time
	err = tr.Get("core", "refresh.hold", &t2)
	c.Assert(err, IsNil)

	// when traversing json through the core config transaction, there will be
	// different wall/monotonic clock times, we remove this ambiguity by
	// formatting as rfc3339 which will strip this negligible difference in time
	c.Assert(t1.Format(time.RFC3339), Equals, t2.Format(time.RFC3339))
}

func (s *autoRefreshTestSuite) TestEffectiveRefreshHold(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	af := snapstate.NewAutoRefresh(s.state)
	t0, err := af.EffectiveRefreshHold()
	c.Assert(err, IsNil)
	c.Check(t0.IsZero(), Equals, true)

	holdTime := time.Now()
	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.hold", holdTime)
	tr.Commit()

	t1, err := af.EffectiveRefreshHold()
	c.Assert(err, IsNil)
	c.Check(t1.Equal(holdTime), Equals, true)

	longTime := time.Now().Add(snapstate.MaxDuration + time.Hour)
	// truncate time that isn't part of the RFC3339 timestamp
	longTime = longTime.UTC().Truncate(time.Second)

	tr = config.NewTransaction(s.state)
	tr.Set("core", "refresh.hold", longTime.Format(time.RFC3339))
	tr.Commit()

	t2, err := af.EffectiveRefreshHold()
	c.Assert(err, IsNil)

	t2 = t2.UTC().Truncate(time.Second)
	c.Check(t2.Equal(longTime), Equals, true)
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

func (s *autoRefreshTestSuite) TestAtSeedRefreshHeld(c *C) {
	// it is possible that refresh.hold has already been set to a valid time
	// or "forever" even before snapd was started for the first time
	r := release.MockOnClassic(true)
	defer r()

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "refresh.hold", "forever"), IsNil)
	tr.Commit()

	af := snapstate.NewAutoRefresh(s.state)
	err := af.AtSeed()
	c.Assert(err, IsNil)
	c.Check(af.NextRefresh().IsZero(), Equals, true)

	// now use a valid timestamp
	tr = config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "refresh.hold", time.Now().UTC()), IsNil)
	tr.Commit()

	af = snapstate.NewAutoRefresh(s.state)
	err = af.AtSeed()
	c.Assert(err, IsNil)
	c.Check(af.NextRefresh().IsZero(), Equals, true)
}

func (s *autoRefreshTestSuite) TestAtSeedInvalidHold(c *C) {
	// it is possible that refresh.hold has already been set to forever even
	// before snapd was started for the first time
	r := release.MockOnClassic(true)
	defer r()

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "refresh.hold", "this-is-invalid-time"), IsNil)
	tr.Commit()

	af := snapstate.NewAutoRefresh(s.state)

	err := af.AtSeed()
	c.Assert(err, ErrorMatches, `parsing time "this-is-invalid-time" .*cannot parse.*`)
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

	// last refresh over 96 days ago, new one is launched regardless of
	// connection being metered
	s.state.Set("last-refresh", time.Now().Add(-96*24*time.Hour))
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

func (s *autoRefreshTestSuite) TestInitialInhibitRefreshWithinInhibitWindow(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstate.MockAsyncPendingRefreshNotification(func(ctx context.Context, refreshInfo *userclient.PendingSnapRefreshInfo) {
		c.Fatal("shouldn't trigger pending refresh notification unless it was an auto-refresh and we're overdue")
	})
	defer restore()

	si := &snap.SideInfo{RealName: "pkg", Revision: snap.R(1)}
	info := &snap.Info{SideInfo: *si}
	snapst := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	}
	snapsup := &snapstate.SnapSetup{Flags: snapstate.Flags{IsAutoRefresh: true}}

	restore = snapstate.MockRefreshAppsCheck(func(si *snap.Info) error {
		return snapstate.NewBusySnapError(si, []int{123}, nil, nil)
	})
	defer restore()

	inhibitionTimeout, err := snapstate.InhibitRefresh(s.state, snapst, snapsup, info)
	c.Assert(err, ErrorMatches, `snap "pkg" has running apps or hooks, pids: 123`)
	c.Check(inhibitionTimeout, Equals, false)

	var timedErr *snapstate.TimedBusySnapError
	c.Assert(errors.As(err, &timedErr), Equals, true)

	refreshInfo := timedErr.PendingSnapRefreshInfo()
	c.Assert(refreshInfo, NotNil)
	c.Check(refreshInfo.InstanceName, Equals, "pkg")
	c.Check(refreshInfo.TimeRemaining, Equals, time.Hour*14*24-time.Second)
}

func (s *autoRefreshTestSuite) TestSubsequentInhibitRefreshWithinInhibitWindow(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstate.MockAsyncPendingRefreshNotification(func(ctx context.Context, refreshInfo *userclient.PendingSnapRefreshInfo) {
		c.Fatal("shouldn't trigger pending refresh notification unless it was an auto-refresh and we're overdue")
	})
	defer restore()

	instant := time.Now()
	pastInstant := instant.Add(-snapstate.MaxInhibitionDuration(s.state) / 2) // In the middle of the allowed window

	si := &snap.SideInfo{RealName: "pkg", Revision: snap.R(1)}
	info := &snap.Info{SideInfo: *si}
	snapst := &snapstate.SnapState{
		Sequence:             snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:              si.Revision,
		RefreshInhibitedTime: &pastInstant,
	}
	snapsup := &snapstate.SnapSetup{Flags: snapstate.Flags{IsAutoRefresh: true}}

	restore = snapstate.MockRefreshAppsCheck(func(si *snap.Info) error {
		return snapstate.NewBusySnapError(si, []int{123}, nil, nil)
	})
	defer restore()

	inhibitionTimeout, err := snapstate.InhibitRefresh(s.state, snapst, snapsup, info)
	c.Assert(err, ErrorMatches, `snap "pkg" has running apps or hooks, pids: 123`)
	c.Check(inhibitionTimeout, Equals, false)

	var timedErr *snapstate.TimedBusySnapError
	c.Assert(errors.As(err, &timedErr), Equals, true)
	refreshInfo := timedErr.PendingSnapRefreshInfo()

	c.Assert(refreshInfo, NotNil)
	c.Check(refreshInfo.InstanceName, Equals, "pkg")
	// XXX: This test measures real time, with second granularity.
	// It takes non-zero (hence the subtracted second) to execute the test.
	c.Check(refreshInfo.TimeRemaining, Equals, time.Hour*14*24/2-time.Second)
}

func (s *autoRefreshTestSuite) TestInhibitRefreshRefreshesWhenOverdue(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	notificationCount := 0
	restore := snapstate.MockAsyncPendingRefreshNotification(func(ctx context.Context, refreshInfo *userclient.PendingSnapRefreshInfo) {
		notificationCount++
		c.Check(refreshInfo.InstanceName, Equals, "pkg")
		c.Check(refreshInfo.TimeRemaining, Equals, time.Duration(0))
	})
	defer restore()

	instant := time.Now()
	pastInstant := instant.Add(-snapstate.MaxInhibitionDuration(s.state) * 2)

	si := &snap.SideInfo{RealName: "pkg", Revision: snap.R(1)}
	info := &snap.Info{SideInfo: *si}
	snapst := &snapstate.SnapState{
		Sequence:             snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:              si.Revision,
		RefreshInhibitedTime: &pastInstant,
	}
	snapsup := &snapstate.SnapSetup{Flags: snapstate.Flags{IsAutoRefresh: true}}

	restore = snapstate.MockRefreshAppsCheck(func(si *snap.Info) error {
		return &snapstate.BusySnapError{SnapInfo: si}
	})
	defer restore()

	inhibitionTimeout, err := snapstate.InhibitRefresh(s.state, snapst, snapsup, info)
	c.Assert(err == nil, Equals, true)
	c.Check(inhibitionTimeout, Equals, true)
	c.Check(notificationCount, Equals, 1)
}

func (s *autoRefreshTestSuite) TestInhibitNoNotificationOnManualRefresh(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstate.MockAsyncPendingRefreshNotification(func(ctx context.Context, refreshInfo *userclient.PendingSnapRefreshInfo) {
		c.Fatal("shouldn't trigger pending refresh notification if refresh was manual")
	})
	defer restore()

	pastInstant := time.Now().Add(-snapstate.MaxInhibitionDuration(s.state))

	si := &snap.SideInfo{RealName: "pkg", Revision: snap.R(1)}
	info := &snap.Info{SideInfo: *si}
	snapst := &snapstate.SnapState{
		Sequence:             snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:              si.Revision,
		RefreshInhibitedTime: &pastInstant,
	}
	// manual refresh
	snapsup := &snapstate.SnapSetup{Flags: snapstate.Flags{IsAutoRefresh: false}}

	restore = snapstate.MockRefreshAppsCheck(func(si *snap.Info) error {
		return &snapstate.BusySnapError{SnapInfo: si}
	})
	defer restore()

	inhibitionTimeout, err := snapstate.InhibitRefresh(s.state, snapst, snapsup, info)
	c.Assert(err, testutil.ErrorIs, &snapstate.BusySnapError{})
	c.Check(inhibitionTimeout, Equals, false)
}

func (s *autoRefreshTestSuite) TestBlockedAutoRefreshCreatesPreDownloads(c *C) {
	s.addRefreshableSnap("foo")

	restore := snapstate.MockRefreshAppsCheck(func(si *snap.Info) error {
		return snapstate.NewBusySnapError(si, []int{123}, nil, nil)
	})
	defer restore()

	af := snapstate.NewAutoRefresh(s.state)
	err := af.Ensure()
	c.Check(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	chgs := s.state.Changes()
	c.Assert(chgs, HasLen, 1)
	checkPreDownloadChange(c, chgs[0], "foo", snap.R(8))
}

func (s *autoRefreshTestSuite) TestAutoRefreshCreatesBothRefreshAndPreDownload(c *C) {
	s.addRefreshableSnap("foo", "bar")

	restore := snapstate.MockRefreshAppsCheck(func(si *snap.Info) error {
		if si.RealName == "foo" {
			return snapstate.NewBusySnapError(si, []int{123}, nil, nil)
		}

		return nil
	})
	defer restore()

	af := snapstate.NewAutoRefresh(s.state)
	err := af.Ensure()
	c.Check(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	chgs := s.state.Changes()
	c.Assert(chgs, HasLen, 2)
	// sort "auto-refresh" into first and "pre-download" into second
	sort.Slice(chgs, func(i, j int) bool {
		return chgs[i].Kind() < chgs[j].Kind()
	})

	checkPreDownloadChange(c, chgs[1], "foo", snap.R(8))

	c.Assert(chgs[0].Kind(), Equals, "auto-refresh")
	var names []string
	err = chgs[0].Get("snap-names", &names)
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"bar"})
}

func checkPreDownloadChange(c *C, chg *state.Change, name string, rev snap.Revision) {
	c.Assert(chg.Kind(), Equals, "pre-download")
	c.Assert(chg.Summary(), Equals, fmt.Sprintf(`Pre-download "%s" for auto-refresh`, name))
	c.Assert(chg.Tasks(), HasLen, 1)
	task := chg.Tasks()[0]
	c.Assert(task.Kind(), Equals, "pre-download-snap")
	c.Assert(task.Summary(), testutil.Contains, fmt.Sprintf("Pre-download snap %q (%s) from channel", name, rev))

	var snapsup snapstate.SnapSetup
	c.Assert(task.Get("snap-setup", &snapsup), IsNil)
	c.Assert(snapsup.InstanceName(), Equals, name)
	c.Assert(snapsup.Revision(), Equals, rev)

	var refreshInfo userclient.PendingSnapRefreshInfo
	c.Assert(task.Get("refresh-info", &refreshInfo), IsNil)
	c.Assert(refreshInfo.InstanceName, Equals, name)
}

func (s *autoRefreshTestSuite) addRefreshableSnap(names ...string) {
	s.state.Lock()
	defer s.state.Unlock()

	for _, name := range names {
		si := &snap.SideInfo{RealName: name, SnapID: fmt.Sprintf("%s-id", name), Revision: snap.R(1)}
		snapstate.Set(s.state, name, &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:  si.Revision,
			SnapType: string(snap.TypeApp),
		})

		s.store.refreshable = append(s.store.refreshable, &snap.Info{
			SnapType:      snap.TypeApp,
			Architectures: []string{"all"},
			SideInfo: snap.SideInfo{
				RealName: name,
				Revision: snap.R(8),
			}})
	}
}

func (s *autoRefreshTestSuite) TestTooSoonError(c *C) {
	c.Check(snapstate.TooSoonError{}, testutil.ErrorIs, snapstate.TooSoonError{})
	c.Check(snapstate.TooSoonError{}, Not(testutil.ErrorIs), errors.New(""))
	c.Check(snapstate.TooSoonError{}.Error(), Equals, "cannot auto-refresh so soon")
}

func setStoreAccess(s *state.State, access interface{}) {
	s.Lock()
	defer s.Unlock()

	tr := config.NewTransaction(s)
	tr.Set("core", "store.access", access)
	tr.Commit()
}

func (s *autoRefreshTestSuite) TestSnapStoreOffline(c *C) {
	s.addRefreshableSnap("foo")

	setStoreAccess(s.state, "offline")

	af := snapstate.NewAutoRefresh(s.state)
	err := af.Ensure()
	c.Check(err, IsNil)

	s.state.Lock()
	c.Check(s.state.Changes(), HasLen, 0)
	s.state.Unlock()

	setStoreAccess(s.state, nil)

	err = af.Ensure()
	c.Check(err, IsNil)

	c.Check(s.store.ops, DeepEquals, []string{"list-refresh"})
	s.state.Lock()
	c.Check(s.state.Changes(), HasLen, 1)
	s.state.Unlock()
}

func (s *autoRefreshTestSuite) testMaybeAddRefreshInhibitNotice(c *C, markerInterfaceConnected bool, warningFallback bool) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	var connCheckCalled int
	restore := snapstate.MockHasActiveConnection(func(st *state.State, iface string) (bool, error) {
		connCheckCalled++
		c.Check(iface, Equals, "snap-refresh-observe")
		return markerInterfaceConnected, nil
	})
	defer restore()

	// let's add some random warnings
	st.Warnf("this is a random warning 1")
	st.Warnf("this is a random warning 2")

	err := snapstate.MaybeAddRefreshInhibitNotice(st)
	c.Assert(err, IsNil)
	// empty set of inhibited snaps unchanged -> []
	// no notice recorded
	c.Assert(st.Notices(nil), HasLen, 0)
	// no "refresh inhibition" warnings recorded
	checkNoRefreshInhibitWarning(c, st)
	// Verify list is empty
	checkLastRecordedInhibitedSnaps(c, st, nil)

	now := time.Now()
	warningTime := now
	// mock time to determine if recorded warning is recent
	defer state.MockTime(warningTime)()
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:              snap.R(1),
		SnapType:             string(snap.TypeApp),
		RefreshInhibitedTime: &now,
	})
	err = snapstate.MaybeAddRefreshInhibitNotice(st)
	c.Assert(err, IsNil)
	// set of inhibited snaps changed -> ["some-snap"]
	// notice recorded
	expectedOccurrances := 1
	checkRefreshInhibitNotice(c, st, expectedOccurrances)
	// check warnings fallback
	if warningFallback {
		checkRefreshInhibitWarning(c, st, []string{"some-snap"}, warningTime)
	} else {
		checkNoRefreshInhibitWarning(c, st)
	}
	// check that the set of last recorded inhibited snaps is persisted
	checkLastRecordedInhibitedSnaps(c, st, []string{"some-snap"})

	// mock time to determine if recorded warning is recent
	warningTime = warningTime.Add(1 * time.Hour)
	defer state.MockTime(warningTime)()
	err = snapstate.MaybeAddRefreshInhibitNotice(st)
	c.Assert(err, IsNil)
	// set of inhibited snaps unchanged -> ["some-snap"]
	// no new notice recorded
	checkRefreshInhibitNotice(c, st, expectedOccurrances)
	// check warnings fallback
	if warningFallback {
		checkRefreshInhibitWarning(c, st, []string{"some-snap"}, warningTime)
	} else {
		checkNoRefreshInhibitWarning(c, st)
	}
	checkLastRecordedInhibitedSnaps(c, st, []string{"some-snap"})

	// mark "some-snap" as not inhibited
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:              snap.R(1),
		SnapType:             string(snap.TypeApp),
		RefreshInhibitedTime: nil,
	})
	// mark "some-other-snap" as inhibited
	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-other-snap", SnapID: "some-other-snap-id", Revision: snap.R(1)},
		}),
		Current:              snap.R(1),
		SnapType:             string(snap.TypeApp),
		RefreshInhibitedTime: &now,
	})
	// mock time to determine if recorded warning is recent
	warningTime = warningTime.Add(1 * time.Hour)
	defer state.MockTime(warningTime)()
	err = snapstate.MaybeAddRefreshInhibitNotice(st)
	c.Assert(err, IsNil)
	// set of inhibited snaps changed -> ["some-other-snap"]
	// notice recorded
	expectedOccurrances++
	checkRefreshInhibitNotice(c, st, expectedOccurrances)
	// check warnings fallback
	if warningFallback {
		checkRefreshInhibitWarning(c, st, []string{"some-other-snap"}, warningTime)
	} else {
		checkNoRefreshInhibitWarning(c, st)
	}
	checkLastRecordedInhibitedSnaps(c, st, []string{"some-other-snap"})

	// mark "some-other-snap" as not inhibited
	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-other-snap", SnapID: "some-other-snap-id", Revision: snap.R(1)},
		}),
		Current:              snap.R(1),
		SnapType:             string(snap.TypeApp),
		RefreshInhibitedTime: nil,
	})
	// mock time to determine if recorded warning is recent
	warningTime = warningTime.Add(1 * time.Hour)
	defer state.MockTime(warningTime)()
	err = snapstate.MaybeAddRefreshInhibitNotice(st)
	c.Assert(err, IsNil)
	// set of inhibited snaps changed -> []
	// notice recorded
	expectedOccurrances++
	checkRefreshInhibitNotice(c, st, expectedOccurrances)
	// no warning should be recorded and existing warning should be
	// removed if inhibited snaps set is empty
	checkNoRefreshInhibitWarning(c, st)
	checkLastRecordedInhibitedSnaps(c, st, []string{})

	// exercise multiple snaps inhibited
	// mark "some-snap" as inhibited
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:              snap.R(1),
		SnapType:             string(snap.TypeApp),
		RefreshInhibitedTime: &now,
	})
	// mark "some-other-snap" as inhibited
	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-other-snap", SnapID: "some-other-snap-id", Revision: snap.R(1)},
		}),
		Current:              snap.R(1),
		SnapType:             string(snap.TypeApp),
		RefreshInhibitedTime: &now,
	})
	// mock time to determine if recorded warning is recent
	warningTime = warningTime.Add(1 * time.Hour)
	defer state.MockTime(warningTime)()
	err = snapstate.MaybeAddRefreshInhibitNotice(st)
	c.Assert(err, IsNil)
	// set of inhibited snaps changed -> ["some-snap", "some-other-snap"]
	// notice recorded
	expectedOccurrances++
	checkRefreshInhibitNotice(c, st, expectedOccurrances)
	// check warnings fallback
	if warningFallback {
		checkRefreshInhibitWarning(c, st, []string{"some-snap", "some-other-snap"}, warningTime)
	} else {
		checkNoRefreshInhibitWarning(c, st)
	}
	// check that the set of last recorded inhibited snaps is persisted
	checkLastRecordedInhibitedSnaps(c, st, []string{"some-snap", "some-other-snap"})
}

func (s *autoRefreshTestSuite) TestMaybeAddRefreshInhibitNotice(c *C) {
	s.enableRefreshAppAwarenessUX()
	const markerInterfaceConnected = true
	const warningFallback = false
	s.testMaybeAddRefreshInhibitNotice(c, markerInterfaceConnected, warningFallback)
}

func (s *autoRefreshTestSuite) TestMaybeAddRefreshInhibitNoticeWarningFallbackError(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	logbuf, restore := logger.MockLogger()
	defer restore()

	// mark "some-snap" as inhibited
	now := time.Now()
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:              snap.R(1),
		SnapType:             string(snap.TypeApp),
		RefreshInhibitedTime: &now,
	})

	// Highly unlikely but just in case
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness-ux", "trigger-error")
	tr.Commit()

	err := snapstate.MaybeAddRefreshInhibitNotice(st)
	// warning fallback error is not propagated, only logged
	c.Assert(err, IsNil)
	// check error is logged
	c.Check(logbuf.String(), testutil.Contains, `Cannot add refresh inhibition warning: refresh-app-awareness-ux can only be set to 'true' or 'false', got "trigger-error"`)
	// notice recorded
	checkRefreshInhibitNotice(c, st, 1)
	// no warnings recorded due to error
	checkNoRefreshInhibitWarning(c, st)

	restore = snapstate.MockHasActiveConnection(func(st *state.State, iface string) (bool, error) {
		return false, fmt.Errorf("boom")
	})
	defer restore()

	st.Unlock()
	s.enableRefreshAppAwarenessUX()
	st.Lock()
	err = snapstate.MaybeAddRefreshInhibitNotice(st)
	// warning fallback error is not propagated, only logged
	c.Assert(err, IsNil)
	// check error is logged
	c.Check(logbuf.String(), testutil.Contains, "Cannot add refresh inhibition warning: boom")
	// set of inhibited snaps unchanged -> ["some-snap"]
	// no new notice recorded
	checkRefreshInhibitNotice(c, st, 1)
	// no warnings recorded due to error
	checkNoRefreshInhibitWarning(c, st)
}

func (s *autoRefreshTestSuite) TestMaybeAddRefreshInhibitNoticeWarningFallback(c *C) {
	s.enableRefreshAppAwarenessUX()
	const markerInterfaceConnected = false
	const warningFallback = true
	s.testMaybeAddRefreshInhibitNotice(c, markerInterfaceConnected, warningFallback)
}

func (s *autoRefreshTestSuite) TestMaybeAddRefreshInhibitNoticeWarningFallbackNoRAAUX(c *C) {
	const markerInterfaceConnected = false
	const warningFallback = false // because refresh-app-awareness-ux is disabled
	s.testMaybeAddRefreshInhibitNotice(c, markerInterfaceConnected, warningFallback)
}

func (s *autoRefreshTestSuite) enableRefreshAppAwarenessUX() {
	s.state.Lock()
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness-ux", true)
	tr.Commit()
	s.state.Unlock()
}

func checkNoRefreshInhibitWarning(c *C, st *state.State) {
	for _, warning := range st.AllWarnings() {
		if strings.HasSuffix(warning.String(), "close running apps to continue refresh.") {
			c.Error("inhibition warning found")
			return
		}
	}
}

func checkRefreshInhibitWarning(c *C, st *state.State, snaps []string, warningTime time.Time) {
	var inhibitionWarning *state.Warning
	for _, warning := range st.AllWarnings() {
		if !strings.HasSuffix(warning.String(), "close running apps to continue refresh.") {
			continue
		}
		if inhibitionWarning != nil {
			c.Errorf("found multiple inhibition warnings")
			return
		}
		inhibitionWarning = warning
	}

	// There is always one warning
	c.Assert(inhibitionWarning, NotNil)
	w := warningToMap(c, inhibitionWarning)
	c.Check(w["message"], Matches, "cannot refresh (.*) due running apps; close running apps to continue refresh.")
	for _, snap := range snaps {
		c.Check(w["message"], testutil.Contains, snap)
	}
	c.Check(w["repeat-after"], Equals, "24h0m0s")
	if !warningTime.IsZero() {
		c.Check(w["last-added"], Equals, warningTime.UTC().Format(time.RFC3339Nano))
	}
}

// warningToMap converts a Warning to a map using a JSON marshal-unmarshal round trip.
func warningToMap(c *C, warning *state.Warning) map[string]any {
	buf, err := json.Marshal(warning)
	c.Assert(err, IsNil)
	var n map[string]any
	err = json.Unmarshal(buf, &n)
	c.Assert(err, IsNil)
	return n
}

func (s *autoRefreshTestSuite) TestMaybeAsyncPendingRefreshNotification(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	var connCheckCalled int
	restore := snapstate.MockHasActiveConnection(func(st *state.State, iface string) (bool, error) {
		connCheckCalled++
		c.Check(iface, Equals, "snap-refresh-observe")
		// no snap has the marker interface connected
		return false, nil
	})
	defer restore()

	expectedInfo := &userclient.PendingSnapRefreshInfo{
		InstanceName:  "pkg",
		TimeRemaining: 10 * time.Second,
	}
	var notificationCalled int
	restore = snapstate.MockAsyncPendingRefreshNotification(func(ctx context.Context, psri *userclient.PendingSnapRefreshInfo) {
		notificationCalled++
		c.Check(psri, Equals, expectedInfo)
	})
	defer restore()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness-ux", true)
	tr.Commit()

	snapstate.MaybeAsyncPendingRefreshNotification(context.TODO(), s.state, expectedInfo)
	// no notification as refresh-appawareness-ux is enabled
	// i.e. notices + warnings fallback is used instead
	c.Check(connCheckCalled, Equals, 0)
	c.Check(notificationCalled, Equals, 0)

	tr.Set("core", "experimental.refresh-app-awareness-ux", false)
	tr.Commit()

	snapstate.MaybeAsyncPendingRefreshNotification(context.TODO(), s.state, expectedInfo)
	// notification sent as refresh-appawareness-ux is now disabled
	c.Check(connCheckCalled, Equals, 1)
	c.Check(notificationCalled, Equals, 1)
}

func (s *autoRefreshTestSuite) TestMaybeAsyncPendingRefreshNotificationSkips(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	var connCheckCalled int
	restore := snapstate.MockHasActiveConnection(func(st *state.State, iface string) (bool, error) {
		connCheckCalled++
		c.Check(iface, Equals, "snap-refresh-observe")
		// marker interface found
		return true, nil
	})
	defer restore()

	restore = snapstate.MockAsyncPendingRefreshNotification(func(ctx context.Context, psri *userclient.PendingSnapRefreshInfo) {
		c.Fatal("shouldn't trigger pending refresh notification because marker interface is connected")
	})
	defer restore()

	snapstate.MaybeAsyncPendingRefreshNotification(context.TODO(), s.state, &userclient.PendingSnapRefreshInfo{})
}
