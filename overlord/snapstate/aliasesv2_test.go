// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
)

func target(at *snapstate.AliasTargets) string {
	if at.Manual != "" {
		return at.Manual
	}
	return at.Auto
}

func (s *snapmgrTestSuite) TestApplyAliasesChange(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	auto1 := &snapstate.AliasTargets{
		Auto: "cmd1",
	}

	auto2 := &snapstate.AliasTargets{
		Auto: "cmd2",
	}

	manual1 := &snapstate.AliasTargets{
		Manual: "cmd1",
	}

	manual2 := &snapstate.AliasTargets{
		Manual: "manual2",
		Auto:   "cmd1",
	}

	scenarios := []struct {
		status    string
		newStatus string
		target    *snapstate.AliasTargets
		newTarget *snapstate.AliasTargets
		ops       string
	}{
		{"enabled", "enabled", nil, auto1, "add"},
		{"enabled", "disabled", auto1, auto1, "rm"},
		{"enabled", "enabled", auto1, auto2, "rm add"},
		{"enabled", "enabled", auto1, nil, "rm"},
		{"enabled", "enabled", nil, manual1, "add"},
		{"disabled", "disabled", nil, manual1, "add"},
		{"enabled", "disabled", auto1, manual2, "rm add"},
		{"enabled", "enabled", manual2, nil, "rm"},
		{"enabled", "enabled", manual2, auto1, "rm add"},
		{"enabled", "enabled", manual1, auto1, ""},
		{"disabled", "enabled", manual1, auto1, ""},
	}

	for _, scenario := range scenarios {
		prevAliases := make(map[string]*snapstate.AliasTargets)
		if scenario.target != nil {
			prevAliases["myalias"] = scenario.target
		}
		newAliases := make(map[string]*snapstate.AliasTargets)
		if scenario.newTarget != nil {
			newAliases["myalias"] = scenario.newTarget
		}

		err := snapstate.ApplyAliasesChange(s.state, "alias-snap1", scenario.status, prevAliases, scenario.newStatus, newAliases, s.fakeBackend)
		c.Assert(err, IsNil)

		var add, rm []*backend.Alias
		if strings.Contains(scenario.ops, "rm") {
			rm = []*backend.Alias{{"myalias", fmt.Sprintf("alias-snap1.%s", target(scenario.target))}}
		}

		if strings.Contains(scenario.ops, "add") {
			add = []*backend.Alias{{"myalias", fmt.Sprintf("alias-snap1.%s", target(scenario.newTarget))}}
		}

		expected := fakeOps{
			{
				op:        "update-aliases",
				rmAliases: rm,
				aliases:   add,
			},
		}

		// start with an easier-to-read error if this fails:
		c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops(), Commentf("%v", scenario))
		c.Assert(s.fakeBackend.ops, DeepEquals, expected, Commentf("%v", scenario))

		s.fakeBackend.ops = nil
	}
}

func (s *snapmgrTestSuite) TestApplyAliasesChangeMulti(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	prevAliases := map[string]*snapstate.AliasTargets{
		"myalias0": {Auto: "cmd0"},
	}
	newAliases := map[string]*snapstate.AliasTargets{
		"myalias1": {Auto: "alias-snap1"},
	}

	err := snapstate.ApplyAliasesChange(s.state, "alias-snap1", "enabled", prevAliases, "enabled", newAliases, s.fakeBackend)
	c.Assert(err, IsNil)

	expected := fakeOps{
		{
			op:        "update-aliases",
			rmAliases: []*backend.Alias{{"myalias0", "alias-snap1.cmd0"}},
			aliases:   []*backend.Alias{{"myalias1", "alias-snap1"}},
		},
	}

	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)
}
