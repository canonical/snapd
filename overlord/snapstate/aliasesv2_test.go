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
	"sort"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
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

func (s *snapmgrTestSuite) TestAutoAliasStatesDelta(c *C) {
	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.Name(), Equals, "alias-snap")
		return map[string]string{
			"alias1": "cmd1",
			"alias2": "cmd2",
			"alias4": "cmd4",
			"alias5": "cmd5",
			"alias6": "cmd6b",
		}, nil
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	s.state.Set("aliases-v2", map[string]map[string]*snapstate.AliasState{
		"alias-snap": {
			"alias1": {Status: "overridden", Target: "cmdx", AutoTargetBak: "cmd1"},
			"alias2": {Status: "disabled", Target: "cmd2"},
			"alias3": {Status: "auto", Target: "cmd3"},
			"alias6": {Status: "auto", Target: "cmd6"},
		},
	})

	touched, retired, err := snapstate.AutoAliasStatesDelta(s.state, []string{"alias-snap"})
	c.Assert(err, IsNil)

	c.Check(touched, HasLen, 1)
	which := touched["alias-snap"]
	sort.Strings(which)
	c.Check(which, DeepEquals, []string{"alias4", "alias5", "alias6"})

	c.Check(retired, DeepEquals, map[string][]string{
		"alias-snap": {"alias3"},
	})
}

func (s *snapmgrTestSuite) TestAutoAliasStatesDeltaAll(c *C) {
	seen := make(map[string]bool)
	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		seen[info.Name()] = true
		if info.Name() == "alias-snap" {
			return map[string]string{
				"alias1": "cmd1",
				"alias2": "cmd2",
				"alias4": "cmd4",
				"alias5": "cmd5",
			}, nil
		}
		return nil, nil
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})
	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", Revision: snap.R(2)},
		},
		Current: snap.R(2),
		Active:  true,
	})

	touched, retired, err := snapstate.AutoAliasStatesDelta(s.state, nil)
	c.Assert(err, IsNil)

	c.Check(touched, HasLen, 1)
	which := touched["alias-snap"]
	sort.Strings(which)
	c.Check(which, DeepEquals, []string{"alias1", "alias2", "alias4", "alias5"})

	c.Check(retired, HasLen, 0)

	c.Check(seen, DeepEquals, map[string]bool{
		"alias-snap": true,
		"other-snap": true,
	})
}

func (s *snapmgrTestSuite) TestAutoAliasStatesDeltaOverManual(c *C) {
	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.Name(), Equals, "alias-snap")
		return map[string]string{
			"alias1": "cmd1",
			"alias2": "cmd2",
		}, nil
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	s.state.Set("aliases-v2", map[string]map[string]*snapstate.AliasState{
		"alias-snap": {
			"alias1": {Status: "manual", Target: "cmd1"},
		},
	})

	touched, retired, err := snapstate.AutoAliasStatesDelta(s.state, []string{"alias-snap"})
	c.Assert(err, IsNil)

	c.Check(touched, HasLen, 1)
	which := touched["alias-snap"]
	sort.Strings(which)
	c.Check(which, DeepEquals, []string{"alias1", "alias2"})

	c.Check(retired, HasLen, 0)
}

func (s *snapmgrTestSuite) TestRefreshAliasStates(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.Name(), Equals, "alias-snap")
		return map[string]string{
			"alias1": "cmd1",
			"alias2": "cmd2",
			"alias4": "cmd4",
			"alias5": "cmd5",
		}, nil
	}

	info := snaptest.MockInfo(c, `
name: alias-snap
apps:
    cmd1:
    cmd2:
    cmd3:
    cmd4:
`, &snap.SideInfo{SnapID: "snap-id"})

	new, err := snapstate.RefreshAliasStates(s.state, info, nil)
	c.Assert(err, IsNil)
	c.Check(new, DeepEquals, map[string]*snapstate.AliasState{
		"alias1": {Status: "auto", Target: "cmd1"},
		"alias2": {Status: "auto", Target: "cmd2"},
		"alias4": {Status: "auto", Target: "cmd4"},
	})

	new, err = snapstate.RefreshAliasStates(s.state, info, map[string]*snapstate.AliasState{
		"alias1":  {Status: "disabled", Target: "cmd1old"},
		"alias5":  {Status: "disabled", Target: "cmd5"},
		"alias6":  {Status: "disabled", Target: "cmd6"},
		"alias4":  {Status: "overridden", Target: "cmd3", AutoTargetBak: "cmd4"},
		"manual3": {Status: "manual", Target: "cmd3"},
		"manual7": {Status: "manual", Target: "cmd7"},
	})
	c.Assert(err, IsNil)
	c.Check(new, DeepEquals, map[string]*snapstate.AliasState{
		"alias1":  {Status: "disabled", Target: "cmd1"},
		"alias2":  {Status: "disabled", Target: "cmd2"},
		"alias4":  {Status: "overridden", Target: "cmd3", AutoTargetBak: "cmd4"},
		"manual3": {Status: "manual", Target: "cmd3"},
	})
}

func (s *snapmgrTestSuite) TestCheckAliasStatesConflictsAgainstAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("aliases-v2", map[string]map[string]*snapstate.AliasState{
		"other-snap1": {
			"alias1": {Status: "disabled", Target: "cmd1"},
			"alias2": {Status: "disabled", Target: "cmd2"},
		},
		"other-snap2": {
			"alias2": {Status: "auto", Target: "cmd2"},
			"alias3": {Status: "auto", Target: "cmd3"},
		},
		"other-snap3": {
			"alias4": {Status: "manual", Target: "cmd8"},
			"alias5": {Status: "disabled", Target: "cmd5"},
		},
	})

	confl, err := snapstate.CheckAliasStatesConflicts(s.state, "alias-snap", map[string]*snapstate.AliasState{
		"alias1": {Status: "auto", Target: "cmd1"},
		"alias5": {Status: "auto", Target: "cmd5"},
	})
	c.Check(err, IsNil)
	c.Check(confl, IsNil)

	confl, err = snapstate.CheckAliasStatesConflicts(s.state, "alias-snap", map[string]*snapstate.AliasState{
		"alias1": {Status: "auto", Target: "cmd1"},
		"alias2": {Status: "auto", Target: "cmd2"},
		"alias3": {Status: "auto", Target: "cmd3"},
		"alias4": {Status: "auto", Target: "cmd4"},
	})
	c.Check(err, FitsTypeOf, &snapstate.AliasConflictError{})
	c.Check(confl, HasLen, 2)
	which := confl["other-snap2"]
	sort.Strings(which)
	c.Check(which, DeepEquals, []string{"alias2", "alias3"})
	c.Check(confl["other-snap3"], DeepEquals, []string{"alias4"})
}

func (s *snapmgrTestSuite) TestAliasConflictError(c *C) {
	e := &snapstate.AliasConflictError{Snap: "foo", Conflicts: map[string][]string{
		"bar": {"baz"},
	}}
	c.Check(e, ErrorMatches, `cannot enable alias "baz" for "foo", already enabled for "bar"`)

	e = &snapstate.AliasConflictError{Snap: "foo", Conflicts: map[string][]string{
		"bar": {"baz1", "baz2"},
	}}
	c.Check(e, ErrorMatches, `cannot enable aliases "baz1", "baz2" for "foo", already enabled for "bar"`)

	e = &snapstate.AliasConflictError{Snap: "foo", Conflicts: map[string][]string{
		"bar1": {"baz1"},
		"bar2": {"baz2"},
	}}
	c.Check(e, ErrorMatches, `cannot enable alias "baz." for "foo", already enabled for "bar." nor alias "baz." already enabled for "bar."`)
}

func (s *snapmgrTestSuite) TestCheckAliasStatesConflictsAgainstSnaps(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	confl, err := snapstate.CheckAliasStatesConflicts(s.state, "alias-snap", map[string]*snapstate.AliasState{
		"alias1": {Status: "auto", Target: "cmd1"},
	})
	c.Check(err, IsNil)
	c.Check(confl, IsNil)

	confl, err = snapstate.CheckAliasStatesConflicts(s.state, "alias-snap", map[string]*snapstate.AliasState{
		"alias1":    {Status: "auto", Target: "cmd1"},
		"some-snap": {Status: "auto", Target: "cmd1"},
	})
	c.Check(err, ErrorMatches, `cannot enable alias "some-snap" for "alias-snap", it conflicts with the command namespace of installed snap "some-snap"`)
	c.Check(confl, IsNil)

	confl, err = snapstate.CheckAliasStatesConflicts(s.state, "alias-snap", map[string]*snapstate.AliasState{
		"alias1":        {Status: "auto", Target: "cmd1"},
		"some-snap.foo": {Status: "auto", Target: "cmd1"},
	})
	c.Check(err, ErrorMatches, `cannot enable alias "some-snap.foo" for "alias-snap", it conflicts with the command namespace of installed snap "some-snap"`)
	c.Check(confl, IsNil)
}
