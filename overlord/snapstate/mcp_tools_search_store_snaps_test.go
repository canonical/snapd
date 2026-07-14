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

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"

	. "gopkg.in/check.v1"
)

func (s *snapMCPSuite) TestSearchStoreSnapsValidate(c *C) {
	_, err := (snapstate.SearchStoreSnapsTool{}).Call(context.Background(), state.New(nil), map[string]any{"name": "  "})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "name must not be empty")
}

func (s *snapMCPSuite) TestSearchStoreSnapsValidateInvalidType(c *C) {
	err := (snapstate.SearchStoreSnapsTool{}).Validate(map[string]any{"name": true})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Matches, `invalid arguments: .*name.*`)
}

func (s *snapMCPSuite) TestSearchStoreSnapsValidateMissingName(c *C) {
	err := (snapstate.SearchStoreSnapsTool{}).Validate(map[string]any{})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "name must not be empty")
}

func (s *snapMCPSuite) TestCallSearchStoreSnaps(c *C) {
	st := state.New(nil)
	mockStore := &mockMCPStore{
		findResults: []*snap.Info{
			{
				SuggestedName:   "hello-world",
				Version:         "1.2",
				OriginalTitle:   "Hello World",
				OriginalSummary: "demo snap",
				Publisher:       snap.StoreAccount{Username: "publisher1"},
				StoreURL:        "https://snapcraft.io/hello-world",
				SideInfo:        snap.SideInfo{RealName: "hello-world", Revision: snap.R(7), SnapID: "snapid-1"},
			},
		},
	}
	st.Lock()
	snapstate.ReplaceStore(st, mockStore)
	st.Unlock()

	result, callErr := (snapstate.SearchStoreSnapsTool{}).Call(context.Background(), st, map[string]any{"name": "hello"})
	c.Assert(callErr, IsNil)

	c.Assert(mockStore.lastSearch, NotNil)
	c.Check(mockStore.lastSearch.Query, Equals, "hello")

	obj := resultToMap(c, result)
	storeSnaps := obj["store_snaps"].([]any)
	c.Assert(storeSnaps, HasLen, 1)
	first := storeSnaps[0].(map[string]any)
	c.Check(first["name"], Equals, "hello-world")
	c.Check(first["snap_id"], Equals, "snapid-1")
	c.Check(first["summary"], Equals, "demo snap")
}
