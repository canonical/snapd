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

func (s *snapmgrTestSuite) TestApplyAliasChange(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	scenarios := []struct {
		status    string
		newStatus string
		target    string
		newTarget string
		ops       string
	}{
		{status: "-", newStatus: "disabled", target: "cmd1", ops: ""},
		{status: "disabled", newStatus: "disabled", target: "cmd1", ops: ""},
		{status: "disabled", newStatus: "disabled", target: "cmd1", newTarget: "cmd2", ops: ""},
		{status: "disabled", newStatus: "-", target: "cmd1", ops: ""},
		{status: "-", newStatus: "manual", target: "cmd1", ops: "add"},
		{status: "manual", newStatus: "manual", target: "cmd1", newTarget: "cmd2", ops: "rm add"},
		{status: "overridden", newStatus: "auto", target: "cmd1", ops: ""},
		{status: "auto", newStatus: "disabled", target: "cmd1", ops: "rm"},
		{status: "auto", newStatus: "-", target: "cmd1", ops: "rm"},
	}

	for _, scenario := range scenarios {
		prevStates := make(map[string]*snapstate.AliasState)
		if scenario.status != "-" {
			prevStates["myalias"] = &snapstate.AliasState{
				Status: scenario.status,
				Target: scenario.target,
			}
		}
		newStates := make(map[string]*snapstate.AliasState)
		if scenario.newStatus != "-" {
			newState := &snapstate.AliasState{
				Status: scenario.newStatus,
			}
			if scenario.newTarget != "" {
				newState.Target = scenario.newTarget
			} else {
				newState.Target = scenario.target
				scenario.newTarget = scenario.target
			}
			newStates["myalias"] = newState
		}

		err := snapstate.ApplyAliasChange(s.state, "alias-snap1", prevStates, newStates, s.fakeBackend)
		c.Assert(err, IsNil)

		var add, rm []*backend.Alias
		if strings.Contains(scenario.ops, "rm") {
			rm = []*backend.Alias{{"myalias", fmt.Sprintf("alias-snap1.%s", scenario.target)}}
		}

		if strings.Contains(scenario.ops, "add") {
			add = []*backend.Alias{{"myalias", fmt.Sprintf("alias-snap1.%s", scenario.newTarget)}}
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

func (s *snapmgrTestSuite) TestApplyAliasChangeMulti(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	prevStates := map[string]*snapstate.AliasState{
		"myalias0": {Status: "auto", Target: "cmd0"},
	}
	newStates := map[string]*snapstate.AliasState{
		"myalias1": {Status: "auto", Target: "alias-snap1"},
	}

	err := snapstate.ApplyAliasChange(s.state, "alias-snap1", prevStates, newStates, s.fakeBackend)
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
