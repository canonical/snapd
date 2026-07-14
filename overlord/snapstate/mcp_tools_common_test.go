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
	"encoding/json"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"

	. "gopkg.in/check.v1"
)

type snapMCPSuite struct{}

var _ = Suite(&snapMCPSuite{})

type mockMCPStore struct {
	storetest.Store

	findResults []*snap.Info
	findErr     error
	lastSearch  *store.Search

	snapInfoResult *snap.Info
	snapInfoErr    error
	lastSnapSpec   store.SnapSpec
}

func (s *mockMCPStore) Find(_ context.Context, search *store.Search, _ *auth.UserState) ([]*snap.Info, error) {
	s.lastSearch = search
	return s.findResults, s.findErr
}

func (s *mockMCPStore) SnapInfo(_ context.Context, snapSpec store.SnapSpec, _ *auth.UserState) (*snap.Info, error) {
	s.lastSnapSpec = snapSpec
	return s.snapInfoResult, s.snapInfoErr
}

func resultToMap(c *C, v any) map[string]any {
	b, err := json.Marshal(v)
	c.Assert(err, IsNil)
	var out map[string]any
	err = json.Unmarshal(b, &out)
	c.Assert(err, IsNil)
	return out
}
