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

		err := snapstate.ApplyAliasesChange(s.state, "alias-snap1", scenario.autoDisabled, prevAliases, scenario.newAutoDisabled, newAliases, s.fakeBackend)
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

	err := snapstate.ApplyAliasesChange(s.state, "alias-snap1", false, prevAliases, false, newAliases, s.fakeBackend)
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

	changed, dropped, err := snapstate.AutoAliasesDeltaV2(s.state, []string{"alias-snap"})
	c.Assert(err, IsNil)

	c.Check(changed, HasLen, 1)
	which := changed["alias-snap"]
	sort.Strings(which)
	c.Check(which, DeepEquals, []string{"alias4", "alias5", "alias6"})

	c.Check(dropped, DeepEquals, map[string][]string{
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

	changed, dropped, err := snapstate.AutoAliasesDeltaV2(s.state, nil)
	c.Assert(err, IsNil)

	c.Check(changed, HasLen, 1)
	which := changed["alias-snap"]
	sort.Strings(which)
	c.Check(which, DeepEquals, []string{"alias1", "alias2", "alias4", "alias5"})

	c.Check(dropped, HasLen, 0)

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

	changed, dropped, err := snapstate.AutoAliasesDeltaV2(s.state, []string{"alias-snap"})
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

	snapstate.Set(s.state, "other-snap3", &snapstate.SnapState{
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
	})

	confl, err := snapstate.CheckAliasesConflicts(s.state, "alias-snap", false, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias5": {Auto: "cmd5"},
	})
	c.Check(err, IsNil)
	c.Check(confl, IsNil)

	confl, err = snapstate.CheckAliasesConflicts(s.state, "alias-snap", false, map[string]*snapstate.AliasTarget{
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

	confl, err = snapstate.CheckAliasesConflicts(s.state, "alias-snap", true, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias3": {Auto: "cmd3"},
		"alias4": {Auto: "cmd4"},
	})
	c.Check(err, IsNil)
	c.Check(confl, IsNil)

	confl, err = snapstate.CheckAliasesConflicts(s.state, "other-snap4", false, map[string]*snapstate.AliasTarget{
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

	confl, err := snapstate.CheckAliasesConflicts(s.state, "alias-snap", false, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
	})
	c.Check(err, IsNil)
	c.Check(confl, IsNil)

	confl, err = snapstate.CheckAliasesConflicts(s.state, "alias-snap", false, map[string]*snapstate.AliasTarget{
		"alias1":    {Auto: "cmd1"},
		"some-snap": {Auto: "cmd1"},
	})
	c.Check(err, ErrorMatches, `cannot enable alias "some-snap" for "alias-snap", it conflicts with the command namespace of installed snap "some-snap"`)
	c.Check(confl, IsNil)

	confl, err = snapstate.CheckAliasesConflicts(s.state, "alias-snap", true, map[string]*snapstate.AliasTarget{
		"alias1":    {Auto: "cmd1"},
		"some-snap": {Auto: "cmd1"},
	})
	c.Check(err, IsNil)
	c.Check(confl, IsNil)

	confl, err = snapstate.CheckAliasesConflicts(s.state, "alias-snap", false, map[string]*snapstate.AliasTarget{
		"alias1":        {Auto: "cmd1"},
		"some-snap.foo": {Auto: "cmd1"},
	})
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

	dis := snapstate.DisableAliases(aliases)
	c.Check(dis, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias3": {Auto: "cmd3"},
	})
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
	s.settle()
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
	s.settle()
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
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
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
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
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
