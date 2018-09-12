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
	"time"

	"golang.org/x/net/context"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
)

type recordingStore struct {
	storetest.Store

	ops []string
}

func (r *recordingStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, user *auth.UserState, opts *store.RefreshOptions) ([]*snap.Info, error) {
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

	s.state.Set("refresh-privacy-key", "privacy-key")
}

func (s *refreshHintsTestSuite) TearDownTest(c *C) {
	snapstate.CanAutoRefresh = nil
	snapstate.AutoAliases = nil
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
