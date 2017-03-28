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

func target(at *snapstate.AliasTarget) string {
	if at.Manual != "" {
		return at.Manual
	}
	return at.Auto
}

func (s *snapmgrTestSuite) TestApplyAliasesChange(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	auto1 := &snapstate.AliasTarget{
		Auto: "cmd1",
	}

	auto2 := &snapstate.AliasTarget{
		Auto: "cmd2",
	}

	manual1 := &snapstate.AliasTarget{
		Manual: "cmd1",
	}

	manual2 := &snapstate.AliasTarget{
		Manual: "manual2",
		Auto:   "cmd1",
	}

	const (
		en  = snapstate.EnabledAliases
		dis = snapstate.DisabledAliases
	)
	scenarios := []struct {
		status    snapstate.AliasesStatus
		newStatus snapstate.AliasesStatus
		target    *snapstate.AliasTarget
		newTarget *snapstate.AliasTarget
		ops       string
	}{
		{en, en, nil, auto1, "add"},
		{en, dis, auto1, auto1, "rm"},
		{en, en, auto1, auto2, "rm add"},
		{en, en, auto1, nil, "rm"},
		{en, en, nil, manual1, "add"},
		{dis, dis, nil, manual1, "add"},
		{en, dis, auto1, manual2, "rm add"},
		{en, en, manual2, nil, "rm"},
		{en, en, manual2, auto1, "rm add"},
		{en, en, manual1, auto1, ""},
		{dis, en, manual1, auto1, ""},
	}

	for _, scenario := range scenarios {
		prevAliases := make(map[string]*snapstate.AliasTarget)
		if scenario.target != nil {
			prevAliases["myalias"] = scenario.target
		}
		newAliases := make(map[string]*snapstate.AliasTarget)
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

	prevAliases := map[string]*snapstate.AliasTarget{
		"myalias0": {Auto: "cmd0"},
	}
	newAliases := map[string]*snapstate.AliasTarget{
		"myalias1": {Auto: "alias-snap1"},
	}

	err := snapstate.ApplyAliasesChange(s.state, "alias-snap1", snapstate.EnabledAliases, prevAliases, snapstate.EnabledAliases, newAliases, s.fakeBackend)
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
