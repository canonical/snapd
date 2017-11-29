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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
)

type recordingStore struct {
	storetest.Store

	ops []string
}

func (r *recordingStore) ListRefresh(cands []*store.RefreshCandidate, _ *auth.UserState, flags *store.RefreshOptions) ([]*snap.Info, error) {
	r.ops = append(r.ops, "list-refresh")
	return nil, nil
}

type refreshHintsTestSuite struct {
	state *state.State

	store *recordingStore
}

var _ = Suite(&refreshHintsTestSuite{})

func (s *refreshHintsTestSuite) SetUpTest(c *C) {
	s.state = state.New(nil)

	s.store = &recordingStore{}
	s.state.Lock()
	snapstate.ReplaceStore(s.state, s.store)
	s.state.Unlock()

	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }
}

func (s *refreshHintsTestSuite) TearDownTest(c *C) {
	snapstate.CanAutoRefresh = nil
}

func (s *refreshHintsTestSuite) TestLastRefresh(c *C) {
	rh := snapstate.NewRefreshHints(s.state)
	err := rh.Ensure()
	c.Check(err, IsNil)
	c.Check(s.store.ops, DeepEquals, []string{"list-refresh"})
}

func (s *refreshHintsTestSuite) TestLastRefreshNoRefreshNeeded(c *C) {
	s.state.Lock()
	s.state.Set("last-refresh-hints", time.Now().Add(23*time.Hour))
	s.state.Unlock()

	rh := snapstate.NewRefreshHints(s.state)
	err := rh.Ensure()
	c.Check(err, IsNil)
	c.Check(s.store.ops, HasLen, 0)
}
