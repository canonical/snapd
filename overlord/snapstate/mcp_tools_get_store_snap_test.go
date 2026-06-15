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
	"time"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"

	. "gopkg.in/check.v1"
)

func (s *snapMCPSuite) TestCallGetStoreSnap(c *C) {
	st := state.New(nil)
	mockStore := &mockMCPStore{
		snapInfoResult: &snap.Info{
			SuggestedName:       "foo",
			Version:             "2.0",
			Architectures:       []string{"amd64"},
			Base:                "core24",
			Confinement:         snap.StrictConfinement,
			License:             "GPL-3.0",
			Tracks:              []string{"latest"},
			OriginalTitle:       "Foo",
			OriginalSummary:     "foo summary",
			OriginalDescription: "foo description",
			Publisher:           snap.StoreAccount{Username: "publisher2", DisplayName: "Publisher Two"},
			StoreURL:            "https://snapcraft.io/foo",
			OriginalLinks:       map[string][]string{"website": {"https://example.com/foo"}},
			Channels:            map[string]*snap.ChannelSnapInfo{"latest/stable": {Channel: "latest/stable", Version: "2.0", Revision: snap.R(11), Confinement: snap.StrictConfinement, ReleasedAt: time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC)}},
			SideInfo:            snap.SideInfo{RealName: "foo", Revision: snap.R(11), SnapID: "snapid-2"},
			SnapType:            snap.TypeApp,
		},
	}
	st.Lock()
	snapstate.ReplaceStore(st, mockStore)
	st.Unlock()

	result, callErr := (snapstate.GetStoreSnapTool{}).Call(context.Background(), st, map[string]any{"snap_name": "foo"})
	c.Assert(callErr, IsNil)

	c.Check(mockStore.lastSnapSpec.Name, Equals, "foo")

	out := resultToMap(c, result)
	c.Check(out["name"], Equals, "foo")
	c.Check(out["description"], Equals, "foo description")
	c.Check(out["publisher"], Equals, "Publisher Two")
	c.Check(out["website"], Equals, "https://example.com/foo")
	channels := out["channels"].(map[string]any)
	stable := channels["latest/stable"].(map[string]any)
	c.Assert(stable, NotNil)
	c.Check(stable["version"], Equals, "2.0")
	c.Check(stable["revision"], Equals, float64(11))
}

func (s *snapMCPSuite) TestCallGetStoreSnapMissing(c *C) {
	st := state.New(nil)
	mockStore := &mockMCPStore{snapInfoErr: store.ErrSnapNotFound}
	st.Lock()
	snapstate.ReplaceStore(st, mockStore)
	st.Unlock()

	result, callErr := (snapstate.GetStoreSnapTool{}).Call(context.Background(), st, map[string]any{"snap_name": "missing"})
	c.Check(result, IsNil)
	c.Assert(callErr, NotNil)
	c.Check(callErr.Error(), Equals, `cannot get store snap "missing": snap not found`)
}
