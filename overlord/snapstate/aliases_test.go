// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
)

func (s *snapmgrTestSuite) TestAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// nothing
	aliases, err := snapstate.Aliases(s.state)
	c.Assert(err, IsNil)
	c.Check(aliases, HasLen, 0)

	// snaps with aliases
	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	snapstate.Set(s.state, "alias-snap2", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap2", Revision: snap.R(12)},
		},
		Current: snap.R(12),
		Active:  true,
	})

	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", Revision: snap.R(2)},
		},
		Current: snap.R(2),
		Active:  true,
	})

	s.state.Set("aliases", map[string]map[string]string{
		"alias-snap": {
			"alias1": "enabled",
			"alias5": "auto",
			"alias3": "disabled",
		},
		"alias-snap2": {
			"alias2": "enabled",
		},
	})

	aliases, err = snapstate.Aliases(s.state)
	c.Assert(err, IsNil)
	c.Check(aliases, DeepEquals, map[string]map[string]string{
		"alias-snap": {
			"alias1": "enabled",
			"alias5": "auto",
			"alias3": "disabled",
		},
		"alias-snap2": {
			"alias2": "enabled",
		},
	})
}
