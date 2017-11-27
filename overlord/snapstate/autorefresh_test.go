// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
)

type autoRefreshStore struct {
	storetest.Store

	ops []string

	listRefreshErr error
}

func (r *autoRefreshStore) ListRefresh(cands []*store.RefreshCandidate, _ *auth.UserState, flags *store.RefreshOptions) ([]*snap.Info, error) {
	r.ops = append(r.ops, "list-refresh")
	return nil, r.listRefreshErr
}

type autoRefreshTestSuite struct {
	state *state.State

	store *autoRefreshStore
}

var _ = Suite(&autoRefreshTestSuite{})

func (s *autoRefreshTestSuite) SetUpTest(c *C) {
	s.state = state.New(nil)

	s.store = &autoRefreshStore{}
	s.state.Lock()
	snapstate.ReplaceStore(s.state, s.store)
	s.state.Unlock()

	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }
}

func (s *autoRefreshTestSuite) TearDownTest(c *C) {
	snapstate.CanAutoRefresh = nil
}

func (s *autoRefreshTestSuite) TestLastRefresh(c *C) {
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
	snapstate.CanSetRefreshScheduleManaged = func(st *state.State) bool {
		return true
	}
	defer func() { snapstate.CanSetRefreshScheduleManaged = nil }()

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.schedule", "managed")
	tr.Commit()

	af := snapstate.NewAutoRefresh(s.state)
	s.state.Unlock()
	err := af.Ensure()
	s.state.Lock()
	c.Check(err, IsNil)
	c.Check(s.store.ops, HasLen, 0)

	refreshScheduleStr, err := af.RefreshSchedule()
	c.Check(refreshScheduleStr, Equals, "managed")
	c.Check(err, IsNil)

	c.Check(af.NextRefresh(), DeepEquals, time.Time{})
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
	s.store.listRefreshErr = fmt.Errorf("random store error")
	af := snapstate.NewAutoRefresh(s.state)
	err := af.Ensure()
	c.Check(err, ErrorMatches, "random store error")
	c.Check(s.store.ops, HasLen, 1)
	c.Check(s.store.ops, DeepEquals, []string{"list-refresh"})

	// call ensure again, our back-off will prevent the store from
	// being hit again
	err = af.Ensure()
	c.Check(err, IsNil)
	c.Check(s.store.ops, HasLen, 1)

	// fake that the retryRefreshDelay is over
	restore := snapstate.MockRefreshRetryDelay(1 * time.Millisecond)
	defer restore()
	time.Sleep(10 * time.Millisecond)

	err = af.Ensure()
	c.Check(err, ErrorMatches, "random store error")
	c.Check(s.store.ops, HasLen, 2)
}
