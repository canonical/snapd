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

func (s *snapMCPSuite) TestCallGetSnapRejectsInvalidType(c *C) {
	result, callErr := (snapstate.GetSnapTool{}).Call(context.Background(), state.New(nil), map[string]any{"snap_name": 1})
	c.Check(result, IsNil)
	c.Check(callErr, NotNil)
	c.Check(callErr.Error(), Matches, `.*invalid arguments:.*snap_name.*`)
}

func (s *snapMCPSuite) TestCallGetSnapMissingSnap(c *C) {
	result, callErr := (snapstate.GetSnapTool{}).Call(context.Background(), state.New(nil), map[string]any{"snap_name": "core"})
	c.Check(result, IsNil)
	c.Check(callErr.Error(), Equals, `cannot get snap "core": cannot find snap "core"`)
}

func (s *snapMCPSuite) TestCallGetSnap(c *C) {
	st := state.New(nil)

	restore := snapstate.MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		return &snap.Info{
			SuggestedName:   name,
			Version:         "1.2.3",
			OriginalTitle:   "Test Snap",
			OriginalSummary: "summary",
			Publisher:       snap.StoreAccount{Username: "publisher1"},
			SideInfo:        *si,
		}, nil
	})
	defer restore()

	st.Lock()
	snapstate.Set(st, "test-snap", &snapstate.SnapState{
		Sequence: sequence.SnapSequence{Revisions: []*sequence.RevisionSideState{
			sequence.NewRevisionSideState(&snap.SideInfo{RealName: "test-snap", Revision: snap.R(7)}, nil),
		}},
		Current:         snap.R(7),
		Active:          true,
		TrackingChannel: "latest/stable",
	})
	st.Unlock()

	result, callErr := (snapstate.GetSnapTool{}).Call(context.Background(), st, map[string]any{"snap_name": "test-snap"})
	c.Assert(callErr, IsNil)

	obj := resultToMap(c, result)
	c.Check(obj["name"], Equals, "test-snap")
	c.Check(obj["version"], Equals, "1.2.3")
	c.Check(obj["revision"], Equals, float64(7))
	c.Check(obj["channel"], Equals, "latest/stable")
	c.Check(obj["installed"], Equals, true)
	c.Check(obj["developer"], Equals, "publisher1")
	c.Check(obj["status"], Equals, "active")
	c.Check(obj["title"], Equals, "Test Snap")
	c.Check(obj["summary"], Equals, "summary")
}

func (s *snapMCPSuite) TestGetSnapValidateInvalidType(c *C) {
	err := (snapstate.GetSnapTool{}).Validate(map[string]any{"snap_name": true})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Matches, `invalid arguments: .*snap_name.*`)
}

func (s *snapMCPSuite) TestGetSnapValidateMissingSnapName(c *C) {
	err := (snapstate.GetSnapTool{}).Validate(map[string]any{})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "snap_name is required")
}
