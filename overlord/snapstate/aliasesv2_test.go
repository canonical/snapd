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

	scenarios := []struct {
		autoDisabled    bool
		newAutoDisabled bool
		target          *snapstate.AliasTarget
		newTarget       *snapstate.AliasTarget
		ops             string
	}{
		{false, false, nil, auto1, "add"},
		{false, true, auto1, auto1, "rm"},
		{false, false, auto1, auto2, "rm add"},
		{false, false, auto1, nil, "rm"},
		{false, false, nil, manual1, "add"},
		{true, true, nil, manual1, "add"},
		{false, true, auto1, manual2, "rm add"},
		{false, false, manual2, nil, "rm"},
		{false, false, manual2, auto1, "rm add"},
		{false, false, manual1, auto1, ""},
		{true, false, manual1, auto1, ""},
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

		add1, rm1, err := snapstate.ApplyAliasesChange("alias-snap1", scenario.autoDisabled, prevAliases, scenario.newAutoDisabled, newAliases, s.fakeBackend, false)
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

		c.Check(add1, DeepEquals, add)
		c.Check(rm1, DeepEquals, rm)

		s.fakeBackend.ops = nil
	}
}

func (s *snapmgrTestSuite) TestApplyAliasesChangeMulti(c *C) {
	prevAliases := map[string]*snapstate.AliasTarget{
		"myalias0": {Auto: "cmd0"},
	}
	newAliases := map[string]*snapstate.AliasTarget{
		"myalias1": {Auto: "alias-snap1"},
	}

	_, _, err := snapstate.ApplyAliasesChange("alias-snap1", false, prevAliases, false, newAliases, s.fakeBackend, false)
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

func (s *snapmgrTestSuite) TestAutoAliasesDelta(c *C) {
	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.InstanceName(), Equals, "alias-snap")
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

	changed, dropped, err := snapstate.AutoAliasesDelta(s.state, []string{"alias-snap"})
	c.Assert(err, IsNil)

	c.Check(changed, HasLen, 1)
	which := changed["alias-snap"]
	sort.Strings(which)
	c.Check(which, DeepEquals, []string{"alias4", "alias5", "alias6"})

	c.Check(dropped, DeepEquals, map[string][]string{
		"alias-snap": {"alias3"},
	})
}

func (s *snapmgrTestSuite) TestAutoAliasesDeltaAll(c *C) {
	seen := make(map[string]bool)
	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		seen[info.InstanceName()] = true
		if info.InstanceName() == "alias-snap" {
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

	changed, dropped, err := snapstate.AutoAliasesDelta(s.state, nil)
	c.Assert(err, IsNil)

	c.Check(changed, HasLen, 1)
	which := changed["alias-snap"]
	sort.Strings(which)
	c.Check(which, DeepEquals, []string{"alias1", "alias2", "alias4", "alias5"})

	c.Check(dropped, HasLen, 0)

	c.Check(seen, DeepEquals, map[string]bool{
		"core":       true,
		"alias-snap": true,
		"other-snap": true,
	})
}

func (s *snapmgrTestSuite) TestAutoAliasesDeltaOverManual(c *C) {
	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.InstanceName(), Equals, "alias-snap")
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

	changed, dropped, err := snapstate.AutoAliasesDelta(s.state, []string{"alias-snap"})
	c.Assert(err, IsNil)

	c.Check(changed, HasLen, 1)
	which := changed["alias-snap"]
	sort.Strings(which)
	c.Check(which, DeepEquals, []string{"alias1", "alias2"})

	c.Check(dropped, HasLen, 0)
}

func (s *snapmgrTestSuite) TestRefreshAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.InstanceName(), Equals, "alias-snap")
		return map[string]string{
			"alias1": "cmd1",
			"alias2": "cmd2",
			"alias4": "cmd4",
			"alias5": "cmd5",
		}, nil
	}

	info := snaptest.MockInfo(c, `
name: alias-snap
version: 0
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
		Current:             snap.R(2),
		Active:              true,
		AutoAliasesDisabled: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2"},
		},
	})

	snapstate.Set(s.state, "other-snap2", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", Revision: snap.R(2)},
		},
		Current:             snap.R(2),
		Active:              true,
		AutoAliasesDisabled: false,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias2": {Auto: "cmd2"},
			"alias3": {Auto: "cmd3"},
		},
	})

	snapst3 := snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", Revision: snap.R(2)},
		},
		Current:             snap.R(2),
		Active:              true,
		AutoAliasesDisabled: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias4": {Manual: "cmd8"},
			"alias5": {Auto: "cmd5"},
		},
	}
	snapstate.Set(s.state, "other-snap3", &snapst3)

	confl, err := snapstate.CheckAliasesConflicts(s.state, "alias-snap", false, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias5": {Auto: "cmd5"},
	}, nil)
	c.Check(err, IsNil)
	c.Check(confl, IsNil)

	confl, err = snapstate.CheckAliasesConflicts(s.state, "alias-snap", false, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias3": {Auto: "cmd3"},
		"alias4": {Auto: "cmd4"},
	}, nil)
	c.Check(err, FitsTypeOf, &snapstate.AliasConflictError{})
	c.Check(confl, HasLen, 2)
	which := confl["other-snap2"]
	sort.Strings(which)
	c.Check(which, DeepEquals, []string{"alias2", "alias3"})
	c.Check(confl["other-snap3"], DeepEquals, []string{"alias4"})

	confl, err = snapstate.CheckAliasesConflicts(s.state, "alias-snap", true, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias3": {Auto: "cmd3"},
		"alias4": {Auto: "cmd4"},
	}, nil)
	c.Check(err, IsNil)
	c.Check(confl, IsNil)

	confl, err = snapstate.CheckAliasesConflicts(s.state, "other-snap4", false, map[string]*snapstate.AliasTarget{
		"alias2": {Manual: "cmd12"},
	}, nil)
	c.Check(err, FitsTypeOf, &snapstate.AliasConflictError{})
	c.Check(confl, HasLen, 1)
	which = confl["other-snap2"]
	sort.Strings(which)
	c.Check(which, DeepEquals, []string{"alias2"})

	snapst3En := snapst3
	snapst3En.AutoAliasesDisabled = false
	confl, err = snapstate.CheckAliasesConflicts(s.state, "alias-snap", false, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias5": {Auto: "cmd5"},
	}, map[string]*snapstate.SnapState{
		"other-snap3": &snapst3En,
	})
	c.Check(err, FitsTypeOf, &snapstate.AliasConflictError{})
	c.Check(confl, HasLen, 1)
	which = confl["other-snap3"]
	sort.Strings(which)
	c.Check(which, DeepEquals, []string{"alias5"})
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

	confl, err := snapstate.CheckAliasesConflicts(s.state, "alias-snap", false, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
	}, nil)
	c.Check(err, IsNil)
	c.Check(confl, IsNil)

	confl, err = snapstate.CheckAliasesConflicts(s.state, "alias-snap", false, map[string]*snapstate.AliasTarget{
		"alias1":    {Auto: "cmd1"},
		"some-snap": {Auto: "cmd1"},
	}, nil)
	c.Check(err, ErrorMatches, `cannot enable alias "some-snap" for "alias-snap", it conflicts with the command namespace of installed snap "some-snap"`)
	c.Check(confl, IsNil)

	confl, err = snapstate.CheckAliasesConflicts(s.state, "alias-snap", true, map[string]*snapstate.AliasTarget{
		"alias1":    {Auto: "cmd1"},
		"some-snap": {Auto: "cmd1"},
	}, nil)
	c.Check(err, IsNil)
	c.Check(confl, IsNil)

	confl, err = snapstate.CheckAliasesConflicts(s.state, "alias-snap", false, map[string]*snapstate.AliasTarget{
		"alias1":        {Auto: "cmd1"},
		"some-snap.foo": {Auto: "cmd1"},
	}, nil)
	c.Check(err, ErrorMatches, `cannot enable alias "some-snap.foo" for "alias-snap", it conflicts with the command namespace of installed snap "some-snap"`)
	c.Check(confl, IsNil)
}

func (s *snapmgrTestSuite) TestDisableAliases(c *C) {
	aliases := map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias3": {Manual: "manual3", Auto: "cmd3"},
		"alias4": {Manual: "manual4"},
	}

	dis, manuals := snapstate.DisableAliases(aliases)
	c.Check(dis, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias3": {Auto: "cmd3"},
	})
	c.Check(manuals, DeepEquals, map[string]string{
		"alias3": "manual3",
		"alias4": "manual4",
	})

	aliases = map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
	}
	dis, manuals = snapstate.DisableAliases(aliases)
	c.Check(dis, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
	})
	c.Check(manuals, DeepEquals, map[string]string(nil))
}

func (s *snapmgrTestSuite) TestAliasTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	ts, err := snapstate.Alias(s.state, "some-snap", "cmd1", "alias1")
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"alias",
	})
}

type changedAlias struct {
	Snap  string `json:"snap"`
	App   string `json:"app"`
	Alias string `json:"alias"`
}

type traceData struct {
	Added   []*changedAlias `json:"aliases-added,omitempty"`
	Removed []*changedAlias `json:"aliases-removed,omitempty"`
}

func (s *snapmgrTestSuite) TestAliasRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	chg := s.state.NewChange("alias", "manual alias")
	ts, err := snapstate.Alias(s.state, "alias-snap", "cmd1", "alias1")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))
	expected := fakeOps{
		{
			op:      "update-aliases",
			aliases: []*backend.Alias{{"alias1", "alias-snap.cmd1"}},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Manual: "cmd1"},
	})

	var trace traceData
	err = chg.Get("api-data", &trace)
	c.Assert(err, IsNil)
	c.Check(trace, DeepEquals, traceData{
		Added: []*changedAlias{{Snap: "alias-snap", App: "cmd1", Alias: "alias1"}},
	})
}

func (s *snapmgrTestSuite) TestAliasNoTarget(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	chg := s.state.NewChange("alias", "manual alias")
	ts, err := snapstate.Alias(s.state, "some-snap", "cmdno", "alias1")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.snapmgr.Ensure()
	s.snapmgr.Wait()
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot enable alias "alias1" for "some-snap", target application "cmdno" does not exist.*`)
}

func (s *snapmgrTestSuite) TestAliasTargetIsDaemon(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	chg := s.state.NewChange("alias", "manual alias")
	ts, err := snapstate.Alias(s.state, "alias-snap", "cmddaemon", "alias1")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.snapmgr.Ensure()
	s.snapmgr.Wait()
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot enable alias "alias1" for "alias-snap", target application "cmddaemon" is a daemon.*`)
}

func (s *snapmgrTestSuite) TestAliasInvalidAlias(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	_, err := snapstate.Alias(s.state, "some-snap", "cmd", ".alias")
	c.Assert(err, ErrorMatches, `invalid alias name: ".alias"`)
}

func (s *snapmgrTestSuite) TestAliasOverAutoRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2"},
		},
	})

	chg := s.state.NewChange("alias", "manual alias")
	ts, err := snapstate.Alias(s.state, "alias-snap", "cmd5", "alias1")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))
	expected := fakeOps{
		{
			op:        "update-aliases",
			rmAliases: []*backend.Alias{{"alias1", "alias-snap.cmd1"}},
			aliases:   []*backend.Alias{{"alias1", "alias-snap.cmd5"}},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Manual: "cmd5", Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
	})
}

func (s *snapmgrTestSuite) TestAliasUpdateChangeConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	ts, err := snapstate.Alias(s.state, "some-snap", "cmd1", "alias1")
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("alias", "...").AddAll(ts)

	_, err = snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "some-snap" has "alias" change in progress`)
}

func (s *snapmgrTestSuite) TestUpdateAliasChangeConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("update", "...").AddAll(ts)

	_, err = snapstate.Alias(s.state, "some-snap", "cmd1", "alias1")
	c.Assert(err, ErrorMatches, `snap "some-snap" has "update" change in progress`)
}

func (s *snapmgrTestSuite) TestAliasAliasConflict(c *C) {
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
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
		},
	})

	chg := s.state.NewChange("alias", "alias")
	ts, err := snapstate.Alias(s.state, "alias-snap", "cmd5", "alias1")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.snapmgr.Ensure()
	s.snapmgr.Wait()
	s.state.Lock()

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot enable alias "alias1" for "alias-snap", already enabled for "other-snap".*`)
}

func (s *snapmgrTestSuite) TestDisableAllAliasesTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	ts, err := snapstate.DisableAllAliases(s.state, "some-snap")
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"disable-aliases",
	})
}

func (s *snapmgrTestSuite) TestDisableAllAliasesRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Manual: "cmd5", Auto: "cmd1"},
			"alias2": {Auto: "cmd2"},
			"alias3": {Manual: "cmd3"},
		},
	})

	chg := s.state.NewChange("unalias", "unalias")
	ts, err := snapstate.DisableAllAliases(s.state, "alias-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))
	expected := fakeOps{
		{
			op: "update-aliases",
			rmAliases: []*backend.Alias{
				{"alias1", "alias-snap.cmd5"},
				{"alias2", "alias-snap.cmd2"},
				{"alias3", "alias-snap.cmd3"},
			},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
	})

	var trace traceData
	err = chg.Get("api-data", &trace)
	c.Assert(err, IsNil)
	c.Check(trace.Added, HasLen, 0)
	c.Check(trace.Removed, HasLen, 3)
}

func (s *snapmgrTestSuite) TestRemoveManualAliasTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Manual: "cmd5"},
		},
	})

	ts, snapName, err := snapstate.RemoveManualAlias(s.state, "alias1")
	c.Assert(err, IsNil)
	c.Check(snapName, Equals, "alias-snap")

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"unalias",
	})
	task := ts.Tasks()[0]
	var alias string
	err = task.Get("alias", &alias)
	c.Assert(err, IsNil)
	c.Check(alias, Equals, "alias1")
}

func (s *snapmgrTestSuite) TestRemoveManualAliasRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Manual: "cmd5"},
		},
	})

	chg := s.state.NewChange("alias", "manual alias")
	ts, _, err := snapstate.RemoveManualAlias(s.state, "alias1")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))
	expected := fakeOps{
		{
			op:        "update-aliases",
			rmAliases: []*backend.Alias{{"alias1", "alias-snap.cmd5"}},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, HasLen, 0)

	var trace traceData
	err = chg.Get("api-data", &trace)
	c.Assert(err, IsNil)
	c.Check(trace, DeepEquals, traceData{
		Removed: []*changedAlias{{Snap: "alias-snap", App: "cmd5", Alias: "alias1"}},
	})
}

func (s *snapmgrTestSuite) TestRemoveManualAliasOverAutoRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Manual: "cmd5", Auto: "cmd1"},
		},
	})

	chg := s.state.NewChange("alias", "manual alias")
	ts, _, err := snapstate.RemoveManualAlias(s.state, "alias1")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))
	expected := fakeOps{
		{
			op:        "update-aliases",
			rmAliases: []*backend.Alias{{"alias1", "alias-snap.cmd5"}},
			aliases:   []*backend.Alias{{"alias1", "alias-snap.cmd1"}},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
	})
}

func (s *snapmgrTestSuite) TestRemoveManualAliasNoAlias(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	_, _, err := snapstate.RemoveManualAlias(s.state, "alias1")
	c.Assert(err, ErrorMatches, `cannot find manual alias "alias1" in any snap`)
}

func (s *snapmgrTestSuite) TestUpdateDisableAllAliasesChangeConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Manual: "cmd5"},
		},
	})

	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("update", "...").AddAll(ts)

	_, err = snapstate.DisableAllAliases(s.state, "some-snap")
	c.Assert(err, ErrorMatches, `snap "some-snap" has "update" change in progress`)
}

func (s *snapmgrTestSuite) TestUpdateRemoveManualAliasChangeConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Manual: "cmd5"},
		},
	})

	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("update", "...").AddAll(ts)

	_, _, err = snapstate.RemoveManualAlias(s.state, "alias1")
	c.Assert(err, ErrorMatches, `snap "some-snap" has "update" change in progress`)
}

func (s *snapmgrTestSuite) TestDisableAllAliasesUpdateChangeConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	ts, err := snapstate.DisableAllAliases(s.state, "some-snap")
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("alias", "...").AddAll(ts)

	_, err = snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "some-snap" has "alias" change in progress`)
}

func (s *snapmgrTestSuite) TestRemoveManualAliasUpdateChangeConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Manual: "cmd5"},
		},
	})

	ts, _, err := snapstate.RemoveManualAlias(s.state, "alias1")
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("unalias", "...").AddAll(ts)

	_, err = snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "some-snap" has "unalias" change in progress`)
}

func (s *snapmgrTestSuite) TestPreferTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	ts, err := snapstate.Prefer(s.state, "some-snap")
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"prefer-aliases",
	})
}

func (s *snapmgrTestSuite) TestPreferRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current:             snap.R(11),
		Active:              true,
		AutoAliasesDisabled: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2"},
		},
	})

	chg := s.state.NewChange("prefer", "prefer")
	ts, err := snapstate.Prefer(s.state, "alias-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.snapmgr.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))
	expected := fakeOps{
		{
			op: "update-aliases",
			aliases: []*backend.Alias{
				{"alias1", "alias-snap.cmd1"},
				{"alias2", "alias-snap.cmd2"},
			},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
	})
}

func (s *snapmgrTestSuite) TestUpdatePreferChangeConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:              true,
		Sequence:            []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:             snap.R(7),
		SnapType:            "app",
		AutoAliasesDisabled: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
		},
	})

	ts, err := snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("update", "...").AddAll(ts)

	_, err = snapstate.Prefer(s.state, "some-snap")
	c.Assert(err, ErrorMatches, `snap "some-snap" has "update" change in progress`)
}

func (s *snapmgrTestSuite) TestPreferUpdateChangeConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:              true,
		Sequence:            []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:             snap.R(7),
		SnapType:            "app",
		AutoAliasesDisabled: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
		},
	})

	ts, err := snapstate.Prefer(s.state, "some-snap")
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("prefer", "...").AddAll(ts)

	_, err = snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "some-snap" has "prefer" change in progress`)
}
