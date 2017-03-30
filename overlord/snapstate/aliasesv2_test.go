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

func (s *snapmgrTestSuite) TestAutoAliasesDeltaV2(c *C) {
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
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Manual: "cmdx", Auto: "cmd1"},
			"alias2": {Auto: "cmd2"},
			"alias3": {Auto: "cmd3"},
			"alias6": {Auto: "cmd6"},
		},
	})

	touched, retired, err := snapstate.AutoAliasesDeltaV2(s.state, []string{"alias-snap"})
	c.Assert(err, IsNil)

	c.Check(touched, HasLen, 1)
	which := touched["alias-snap"]
	sort.Strings(which)
	c.Check(which, DeepEquals, []string{"alias4", "alias5", "alias6"})

	c.Check(retired, DeepEquals, map[string][]string{
		"alias-snap": {"alias3"},
	})
}

func (s *snapmgrTestSuite) TestAutoAliasesDeltaV2All(c *C) {
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

	touched, retired, err := snapstate.AutoAliasesDeltaV2(s.state, nil)
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

func (s *snapmgrTestSuite) TestAutoAliasesDeltaV2OverManual(c *C) {
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
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Manual: "manual1"},
		},
	})

	touched, retired, err := snapstate.AutoAliasesDeltaV2(s.state, []string{"alias-snap"})
	c.Assert(err, IsNil)

	c.Check(touched, HasLen, 1)
	which := touched["alias-snap"]
	sort.Strings(which)
	c.Check(which, DeepEquals, []string{"alias1", "alias2"})

	c.Check(retired, HasLen, 0)
}

func (s *snapmgrTestSuite) TestRefreshAliases(c *C) {
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

	new, err := snapstate.RefreshAliases(s.state, info, nil)
	c.Assert(err, IsNil)
	c.Check(new, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias4": {Auto: "cmd4"},
	})

	new, err = snapstate.RefreshAliases(s.state, info, map[string]*snapstate.AliasTarget{
		"alias1":  {Auto: "cmd1old"},
		"alias5":  {Auto: "cmd5"},
		"alias6":  {Auto: "cmd6"},
		"alias4":  {Manual: "cmd3", Auto: "cmd4"},
		"manual3": {Manual: "cmd3"},
		"manual7": {Manual: "cmd7"},
	})
	c.Assert(err, IsNil)
	c.Check(new, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1":  {Auto: "cmd1"},
		"alias2":  {Auto: "cmd2"},
		"alias4":  {Manual: "cmd3", Auto: "cmd4"},
		"manual3": {Manual: "cmd3"},
	})
}

func (s *snapmgrTestSuite) TestCheckAliasesConflictsAgainstAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "other-snap1", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", Revision: snap.R(2)},
		},
		Current:       snap.R(2),
		Active:        true,
		AliasesStatus: snapstate.DisabledAliases,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2"},
		},
	})

	snapstate.Set(s.state, "other-snap2", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", Revision: snap.R(2)},
		},
		Current:       snap.R(2),
		Active:        true,
		AliasesStatus: snapstate.EnabledAliases,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias2": {Auto: "cmd2"},
			"alias3": {Auto: "cmd3"},
		},
	})

	snapstate.Set(s.state, "other-snap3", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", Revision: snap.R(2)},
		},
		Current:       snap.R(2),
		Active:        true,
		AliasesStatus: snapstate.DisabledAliases,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias4": {Manual: "cmd8"},
			"alias5": {Auto: "cmd5"},
		},
	})

	confl, err := snapstate.CheckAliasesConflicts(s.state, "alias-snap", snapstate.EnabledAliases, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias5": {Auto: "cmd5"},
	})
	c.Check(err, IsNil)
	c.Check(confl, IsNil)

	confl, err = snapstate.CheckAliasesConflicts(s.state, "alias-snap", snapstate.EnabledAliases, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias3": {Auto: "cmd3"},
		"alias4": {Auto: "cmd4"},
	})
	c.Check(err, FitsTypeOf, &snapstate.AliasConflictError{})
	c.Check(confl, HasLen, 2)
	which := confl["other-snap2"]
	sort.Strings(which)
	c.Check(which, DeepEquals, []string{"alias2", "alias3"})
	c.Check(confl["other-snap3"], DeepEquals, []string{"alias4"})

	confl, err = snapstate.CheckAliasesConflicts(s.state, "alias-snap", snapstate.DisabledAliases, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias3": {Auto: "cmd3"},
		"alias4": {Auto: "cmd4"},
	})
	c.Check(err, IsNil)
	c.Check(confl, IsNil)

	confl, err = snapstate.CheckAliasesConflicts(s.state, "other-snap4", snapstate.EnabledAliases, map[string]*snapstate.AliasTarget{
		"alias2": {Manual: "cmd12"},
	})
	c.Check(err, FitsTypeOf, &snapstate.AliasConflictError{})
	c.Check(confl, HasLen, 1)
	which = confl["other-snap2"]
	sort.Strings(which)
	c.Check(which, DeepEquals, []string{"alias2"})
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

func (s *snapmgrTestSuite) TestCheckAliasesConflictsAgainstSnaps(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	confl, err := snapstate.CheckAliasesConflicts(s.state, "alias-snap", snapstate.EnabledAliases, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
	})
	c.Check(err, IsNil)
	c.Check(confl, IsNil)

	confl, err = snapstate.CheckAliasesConflicts(s.state, "alias-snap", snapstate.EnabledAliases, map[string]*snapstate.AliasTarget{
		"alias1":    {Auto: "cmd1"},
		"some-snap": {Auto: "cmd1"},
	})
	c.Check(err, ErrorMatches, `cannot enable alias "some-snap" for "alias-snap", it conflicts with the command namespace of installed snap "some-snap"`)
	c.Check(confl, IsNil)

	confl, err = snapstate.CheckAliasesConflicts(s.state, "alias-snap", snapstate.DisabledAliases, map[string]*snapstate.AliasTarget{
		"alias1":    {Auto: "cmd1"},
		"some-snap": {Auto: "cmd1"},
	})
	c.Check(err, IsNil)
	c.Check(confl, IsNil)

	confl, err = snapstate.CheckAliasesConflicts(s.state, "alias-snap", snapstate.EnabledAliases, map[string]*snapstate.AliasTarget{
		"alias1":        {Auto: "cmd1"},
		"some-snap.foo": {Auto: "cmd1"},
	})
	c.Check(err, ErrorMatches, `cannot enable alias "some-snap.foo" for "alias-snap", it conflicts with the command namespace of installed snap "some-snap"`)
	c.Check(confl, IsNil)
}
