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
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"

	. "gopkg.in/check.v1"
)

func (s *snapMCPSuite) TestSnapToMapAllFields(c *C) {
	info := &snap.Info{
		SuggestedName:   "mysnap",
		Version:         "1.0",
		OriginalTitle:   "My Snap",
		OriginalSummary: "A test snap",
		Publisher:       snap.StoreAccount{Username: "someone"},
		SideInfo:        snap.SideInfo{RealName: "mysnap", Revision: snap.R(42)},
	}
	m := snapstate.SnapToMapForTest(info, "stable", true, "active")
	c.Check(m["name"], Equals, "mysnap")
	c.Check(m["version"], Equals, "1.0")
	c.Check(m["revision"], Equals, int(42))
	c.Check(m["channel"], Equals, "stable")
	c.Check(m["installed"], Equals, true)
	c.Check(m["developer"], Equals, "someone")
	c.Check(m["status"], Equals, "active")
	c.Check(m["title"], Equals, "My Snap")
	c.Check(m["summary"], Equals, "A test snap")
}

func (s *snapMCPSuite) TestSnapToMapZeroRevision(c *C) {
	info := &snap.Info{SuggestedName: "core", SideInfo: snap.SideInfo{RealName: "core", Revision: snap.R(0)}}
	m := snapstate.SnapToMapForTest(info, "", false, "")
	c.Check(m["revision"], Equals, int(0))
}

func (s *snapMCPSuite) TestCallListSnapsNoFilter(c *C) {
	result, callErr := (snapstate.ListSnapsTool{}).Call(context.Background(), state.New(nil), map[string]any{})
	c.Assert(callErr, IsNil)
	data := resultToMap(c, result)
	snaps := data["snaps"].([]any)
	c.Check(snaps, HasLen, 0)
}

func (s *snapMCPSuite) TestCallListSnapsMapsFields(c *C) {
	st := state.New(nil)

	restore := snapstate.MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		if name == "alpha-snap" {
			return &snap.Info{
				SuggestedName:   name,
				Version:         "2.1",
				OriginalTitle:   "Alpha Snap",
				OriginalSummary: "alpha summary",
				Publisher:       snap.StoreAccount{Username: "alpha-dev"},
				SideInfo:        *si,
			}, nil
		}
		return &snap.Info{
			SuggestedName:   name,
			Version:         "1.0",
			OriginalTitle:   "Other Snap",
			OriginalSummary: "other summary",
			Publisher:       snap.StoreAccount{Username: "other-dev"},
			SideInfo:        *si,
		}, nil
	})
	defer restore()

	st.Lock()
	snapstate.Set(st, "alpha-snap", &snapstate.SnapState{
		Sequence: sequence.SnapSequence{Revisions: []*sequence.RevisionSideState{
			sequence.NewRevisionSideState(&snap.SideInfo{RealName: "alpha-snap", Revision: snap.R(8)}, nil),
		}},
		Current:         snap.R(8),
		Active:          true,
		TrackingChannel: "latest/stable",
	})
	snapstate.Set(st, "other-snap", &snapstate.SnapState{
		Sequence: sequence.SnapSequence{Revisions: []*sequence.RevisionSideState{
			sequence.NewRevisionSideState(&snap.SideInfo{RealName: "other-snap", Revision: snap.R(1)}, nil),
		}},
		Current:         snap.R(1),
		Active:          false,
		TrackingChannel: "latest/edge",
	})
	st.Unlock()

	result, callErr := (snapstate.ListSnapsTool{}).Call(context.Background(), st, map[string]any{"name": "ALPHA"})
	c.Assert(callErr, IsNil)

	data := resultToMap(c, result)
	snaps := data["snaps"].([]any)
	c.Assert(snaps, HasLen, 1)

	first := snaps[0].(map[string]any)
	c.Check(first["name"], Equals, "alpha-snap")
	c.Check(first["version"], Equals, "2.1")
	c.Check(first["revision"], Equals, float64(8))
	c.Check(first["channel"], Equals, "latest/stable")
	c.Check(first["installed"], Equals, true)
	c.Check(first["developer"], Equals, "alpha-dev")
	c.Check(first["status"], Equals, "active")
	c.Check(first["title"], Equals, "Alpha Snap")
	c.Check(first["summary"], Equals, "alpha summary")
}

func (s *snapMCPSuite) TestListSnapsValidateInvalidType(c *C) {
	err := (snapstate.ListSnapsTool{}).Validate(map[string]any{"name": true})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Matches, `invalid arguments: .*name.*`)
}

func (s *snapMCPSuite) TestListSnapsValidateValidMap(c *C) {
	err := (snapstate.ListSnapsTool{}).Validate(map[string]any{"name": "alpha"})
	c.Assert(err, IsNil)
}
