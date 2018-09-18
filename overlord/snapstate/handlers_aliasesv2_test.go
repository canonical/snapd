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
	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func (s *snapmgrTestSuite) TestDoSetAutoAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.InstanceName(), Equals, "alias-snap")
		return map[string]string{
			"alias1": "cmd1",
			"alias2": "cmd2",
			"alias4": "cmd4",
		}, nil
	}

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current:             snap.R(11),
		Active:              true,
		AutoAliasesDisabled: false,
		AliasesPending:      true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2x"},
			"alias3": {Auto: "cmd3"},
		},
	})

	t := s.state.NewTask("set-auto-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias4": {Auto: "cmd4"},
	})
}

func (s *snapmgrTestSuite) TestDoSetAutoAliasesFirstInstall(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.InstanceName(), Equals, "alias-snap")
		return map[string]string{
			"alias1": "cmd1",
			"alias2": "cmd2",
			"alias4": "cmd4",
		}, nil
	}

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	t := s.state.NewTask("set-auto-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, true)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias4": {Auto: "cmd4"},
	})
}

func (s *snapmgrTestSuite) TestDoUndoSetAutoAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.InstanceName(), Equals, "alias-snap")
		return map[string]string{
			"alias1": "cmd1",
			"alias2": "cmd2",
			"alias4": "cmd4",
		}, nil
	}

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current:        snap.R(11),
		Active:         true,
		AliasesPending: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2x"},
			"alias3": {Auto: "cmd3"},
		},
	})

	t := s.state.NewTask("set-auto-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(t.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AliasesPending, Equals, true)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2x"},
		"alias3": {Auto: "cmd3"},
	})
}

func (s *snapmgrTestSuite) TestDoSetAutoAliasesConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.InstanceName(), Equals, "alias-snap")
		return map[string]string{
			"alias1": "cmd1",
			"alias2": "cmd2",
			"alias4": "cmd4",
		}, nil
	}

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current:        snap.R(11),
		Active:         true,
		AliasesPending: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2x"},
			"alias3": {Auto: "cmd3"},
		},
	})
	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", Revision: snap.R(3)},
		},
		Current:        snap.R(3),
		Active:         true,
		AliasesPending: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias4": {Auto: "cmd4"},
		},
	})

	t := s.state.NewTask("set-auto-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.ErrorStatus, Commentf("%v", chg.Err()))
	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot enable alias "alias4" for "alias-snap", already enabled for "other-snap".*`)
}

func (s *snapmgrTestSuite) TestDoUndoSetAutoAliasesConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.InstanceName(), Equals, "alias-snap")
		return map[string]string{
			"alias1": "cmd1",
			"alias2": "cmd2",
			"alias4": "cmd4",
		}, nil
	}

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current:        snap.R(11),
		Active:         true,
		AliasesPending: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2x"},
			"alias3": {Auto: "cmd3"},
		},
	})

	otherSnapState := &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", Revision: snap.R(3)},
		},
		Current:             snap.R(3),
		Active:              true,
		AutoAliasesDisabled: false,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias5": {Auto: "cmd5"},
		},
	}
	snapstate.Set(s.state, "other-snap", otherSnapState)

	grabAlias3 := func(t *state.Task, _ *tomb.Tomb) error {
		st := t.State()
		st.Lock()
		defer st.Unlock()

		otherSnapState.Aliases = map[string]*snapstate.AliasTarget{
			"alias3": {Auto: "cmd3"},
			"alias5": {Auto: "cmd5"},
		}
		snapstate.Set(s.state, "other-snap", otherSnapState)

		return nil
	}

	s.o.TaskRunner().AddHandler("grab-alias3", grabAlias3, nil)

	t := s.state.NewTask("set-auto-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	tgrab3 := s.state.NewTask("grab-alias3", "grab alias3 for other-snap")
	tgrab3.WaitFor(t)
	chg.AddTask(tgrab3)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 5; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(t.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.AliasesPending, Equals, true)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2x"},
		"alias3": {Auto: "cmd3"},
	})

	c.Check(t.Log(), HasLen, 1)
	c.Check(t.Log()[0], Matches, `.* ERROR cannot reinstate alias state because of conflicts, disabling: cannot enable alias "alias3" for "alias-snap", already enabled for "other-snap".*`)
}

func (s *snapmgrTestSuite) TestDoSetAutoAliasesFirstInstallUnaliased(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.InstanceName(), Equals, "alias-snap")
		return map[string]string{
			"alias1": "cmd1",
			"alias2": "cmd2",
			"alias4": "cmd4",
		}, nil
	}

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	t := s.state.NewTask("set-auto-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
		Flags:    snapstate.Flags{Unaliased: true},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.AliasesPending, Equals, true)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias4": {Auto: "cmd4"},
	})
}

func (s *snapmgrTestSuite) TestDoUndoSetAutoAliasesFirstInstallUnaliased(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.InstanceName(), Equals, "alias-snap")
		return map[string]string{
			"alias1": "cmd1",
			"alias2": "cmd2",
			"alias4": "cmd4",
		}, nil
	}

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	t := s.state.NewTask("set-auto-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
		Flags:    snapstate.Flags{Unaliased: true},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(t.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, true)
	c.Check(snapst.Aliases, HasLen, 0)
}

func (s *snapmgrTestSuite) TestDoSetupAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current:             snap.R(11),
		Active:              true,
		AutoAliasesDisabled: true,
		AliasesPending:      true,
		Aliases: map[string]*snapstate.AliasTarget{
			"manual1": {Manual: "cmd1"},
		},
	})

	t := s.state.NewTask("setup-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus)
	expected := fakeOps{
		{
			op:      "update-aliases",
			aliases: []*backend.Alias{{"manual1", "alias-snap.cmd1"}},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.AliasesPending, Equals, false)
}

func (s *snapmgrTestSuite) TestDoUndoSetupAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current:             snap.R(11),
		Active:              true,
		AutoAliasesDisabled: true,
		AliasesPending:      true,
		Aliases: map[string]*snapstate.AliasTarget{
			"manual1": {Manual: "cmd1"},
		},
	})

	t := s.state.NewTask("setup-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(t.Status(), Equals, state.UndoneStatus)
	expected := fakeOps{
		{
			op:      "update-aliases",
			aliases: []*backend.Alias{{"manual1", "alias-snap.cmd1"}},
		},
		{
			op:   "remove-snap-aliases",
			name: "alias-snap",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.AliasesPending, Equals, true)
}

func (s *snapmgrTestSuite) TestDoSetupAliasesAuto(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current:             snap.R(11),
		Active:              true,
		AutoAliasesDisabled: false,
		AliasesPending:      true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
		},
	})

	t := s.state.NewTask("setup-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus)
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
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
}

func (s *snapmgrTestSuite) TestDoUndoSetupAliasesAuto(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current:             snap.R(11),
		Active:              true,
		AutoAliasesDisabled: false,
		AliasesPending:      true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
		},
	})

	t := s.state.NewTask("setup-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(t.Status(), Equals, state.UndoneStatus)
	expected := fakeOps{
		{
			op:      "update-aliases",
			aliases: []*backend.Alias{{"alias1", "alias-snap.cmd1"}},
		},
		{
			op:   "remove-snap-aliases",
			name: "alias-snap",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, true)
}

func (s *snapmgrTestSuite) TestDoSetupAliasesNothing(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	t := s.state.NewTask("setup-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus)
	expected := fakeOps{
		{
			op: "update-aliases",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
}

func (s *snapmgrTestSuite) TestDoUndoSetupAliasesNothing(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	t := s.state.NewTask("setup-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(t.Status(), Equals, state.UndoneStatus)
	expected := fakeOps{
		{
			op: "update-aliases",
		},
		{
			op:   "remove-snap-aliases",
			name: "alias-snap",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, true)
}

func (s *snapmgrTestSuite) TestDoPruneAutoAliasesAuto(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current:        snap.R(11),
		Active:         true,
		AliasesPending: false,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2"},
			"alias3": {Auto: "cmd3"},
		},
	})

	t := s.state.NewTask("prune-auto-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	t.Set("aliases", []string{"alias2", "alias3"})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	expected := fakeOps{
		{
			op: "update-aliases",
			rmAliases: []*backend.Alias{
				{"alias2", "alias-snap.cmd2"},
				{"alias3", "alias-snap.cmd3"},
			},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
	})
}

func (s *snapmgrTestSuite) TestDoPruneAutoAliasesAutoPending(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current:        snap.R(11),
		Active:         true,
		AliasesPending: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2"},
			"alias3": {Auto: "cmd3"},
		},
	})

	t := s.state.NewTask("prune-auto-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	t.Set("aliases", []string{"alias2", "alias3"})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	// pending: nothing to do on disk
	c.Assert(s.fakeBackend.ops, HasLen, 0)

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
	})
}

func (s *snapmgrTestSuite) TestDoPruneAutoAliasesManualAndDisabled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current:             snap.R(11),
		Active:              true,
		AutoAliasesDisabled: true,
		AliasesPending:      false,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2"},
			"alias3": {Manual: "cmdx", Auto: "cmd3"},
			"alias4": {Manual: "cmd4"},
		},
	})

	t := s.state.NewTask("prune-auto-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	t.Set("aliases", []string{"alias2", "alias3", "alias4"})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	expected := fakeOps{
		{
			op: "update-aliases",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias3": {Manual: "cmdx"},
		"alias4": {Manual: "cmd4"},
	})
}

func (s *snapmgrTestSuite) TestDoRefreshAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.InstanceName(), Equals, "alias-snap")
		return map[string]string{
			"alias1": "cmd1",
			"alias2": "cmd2",
			"alias4": "cmd4",
		}, nil
	}

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current:        snap.R(11),
		Active:         true,
		AliasesPending: false,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2x"},
			"alias3": {Auto: "cmd3"},
		},
	})

	t := s.state.NewTask("refresh-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	expected := fakeOps{
		{
			op: "update-aliases",
			rmAliases: []*backend.Alias{
				{"alias2", "alias-snap.cmd2x"},
				{"alias3", "alias-snap.cmd3"},
			},
			aliases: []*backend.Alias{
				{"alias2", "alias-snap.cmd2"},
				{"alias4", "alias-snap.cmd4"},
			},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias4": {Auto: "cmd4"},
	})
}

func (s *snapmgrTestSuite) TestDoUndoRefreshAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.InstanceName(), Equals, "alias-snap")
		return map[string]string{
			"alias1": "cmd1",
			"alias2": "cmd2",
			"alias4": "cmd4",
		}, nil
	}

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current:        snap.R(11),
		Active:         true,
		AliasesPending: false,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2x"},
			"alias3": {Auto: "cmd3"},
		},
	})

	t := s.state.NewTask("refresh-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(t.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))

	expected := fakeOps{
		{
			op: "update-aliases",
			rmAliases: []*backend.Alias{
				{"alias2", "alias-snap.cmd2x"},
				{"alias3", "alias-snap.cmd3"},
			},
			aliases: []*backend.Alias{
				{"alias2", "alias-snap.cmd2"},
				{"alias4", "alias-snap.cmd4"},
			},
		},
		{
			op: "update-aliases",
			aliases: []*backend.Alias{
				{"alias2", "alias-snap.cmd2x"},
				{"alias3", "alias-snap.cmd3"},
			},
			rmAliases: []*backend.Alias{
				{"alias2", "alias-snap.cmd2"},
				{"alias4", "alias-snap.cmd4"},
			},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2x"},
		"alias3": {Auto: "cmd3"},
	})
}

func (s *snapmgrTestSuite) TestDoUndoRefreshAliasesFromEmpty(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.InstanceName(), Equals, "alias-snap")
		return map[string]string{
			"alias1": "cmd1",
			"alias2": "cmd2",
			"alias4": "cmd4",
		}, nil
	}

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current:        snap.R(11),
		Active:         true,
		AliasesPending: false,
	})

	t := s.state.NewTask("refresh-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(t.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))

	expected := fakeOps{
		{
			op: "update-aliases",
			aliases: []*backend.Alias{
				{"alias1", "alias-snap.cmd1"},
				{"alias2", "alias-snap.cmd2"},
				{"alias4", "alias-snap.cmd4"},
			},
		},
		{
			op: "update-aliases",
			rmAliases: []*backend.Alias{
				{"alias1", "alias-snap.cmd1"},
				{"alias2", "alias-snap.cmd2"},
				{"alias4", "alias-snap.cmd4"},
			},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, HasLen, 0)
}

func (s *snapmgrTestSuite) TestDoRefreshAliasesPending(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.InstanceName(), Equals, "alias-snap")
		return map[string]string{
			"alias1": "cmd1",
			"alias2": "cmd2",
			"alias4": "cmd4",
		}, nil
	}

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current:        snap.R(11),
		Active:         true,
		AliasesPending: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2x"},
			"alias3": {Auto: "cmd3"},
		},
	})

	t := s.state.NewTask("refresh-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	// pending: nothing to do on disk
	c.Assert(s.fakeBackend.ops, HasLen, 0)

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, true)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias4": {Auto: "cmd4"},
	})
}

func (s *snapmgrTestSuite) TestDoUndoRefreshAliasesPending(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.InstanceName(), Equals, "alias-snap")
		return map[string]string{
			"alias1": "cmd1",
			"alias2": "cmd2",
			"alias4": "cmd4",
		}, nil
	}

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current:        snap.R(11),
		Active:         true,
		AliasesPending: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2x"},
			"alias3": {Auto: "cmd3"},
		},
	})

	t := s.state.NewTask("refresh-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(t.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))

	// pending: nothing to do on disk
	c.Assert(s.fakeBackend.ops, HasLen, 0)

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, true)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2x"},
		"alias3": {Auto: "cmd3"},
	})
}

func (s *snapmgrTestSuite) TestDoRefreshAliasesConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.InstanceName(), Equals, "alias-snap")
		return map[string]string{
			"alias1": "cmd1",
			"alias2": "cmd2",
			"alias4": "cmd4",
		}, nil
	}

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2x"},
			"alias3": {Auto: "cmd3"},
		},
	})
	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", Revision: snap.R(3)},
		},
		Current: snap.R(3),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias4": {Auto: "cmd4"},
		},
	})

	t := s.state.NewTask("refresh-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.ErrorStatus, Commentf("%v", chg.Err()))
	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot enable alias "alias4" for "alias-snap", already enabled for "other-snap".*`)
}

func (s *snapmgrTestSuite) TestDoUndoRefreshAliasesConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		c.Check(info.InstanceName(), Equals, "alias-snap")
		return map[string]string{
			"alias1": "cmd1",
			"alias2": "cmd2",
			"alias4": "cmd4",
		}, nil
	}

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current:        snap.R(11),
		Active:         true,
		AliasesPending: false,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2x"},
			"alias3": {Auto: "cmd3"},
		},
	})

	grabAlias3 := func(t *state.Task, _ *tomb.Tomb) error {
		st := t.State()
		st.Lock()
		defer st.Unlock()

		snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
			Sequence: []*snap.SideInfo{
				{RealName: "other-snap", Revision: snap.R(3)},
			},
			Current:        snap.R(3),
			Active:         true,
			AliasesPending: false,
			Aliases: map[string]*snapstate.AliasTarget{
				"alias3": {Auto: "cmd3x"},
			},
		})

		return nil
	}

	s.o.TaskRunner().AddHandler("grab-alias3", grabAlias3, nil)

	t := s.state.NewTask("refresh-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	tgrab3 := s.state.NewTask("grab-alias3", "grab alias3 for other-snap")
	tgrab3.WaitFor(t)
	chg.AddTask(tgrab3)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 5; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(t.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))

	expected := fakeOps{
		{
			op: "update-aliases",
			rmAliases: []*backend.Alias{
				{"alias2", "alias-snap.cmd2x"},
				{"alias3", "alias-snap.cmd3"},
			},
			aliases: []*backend.Alias{
				{"alias2", "alias-snap.cmd2"},
				{"alias4", "alias-snap.cmd4"},
			},
		},
		{
			op: "update-aliases",
			rmAliases: []*backend.Alias{
				{"alias1", "alias-snap.cmd1"},
				{"alias2", "alias-snap.cmd2"},
				{"alias4", "alias-snap.cmd4"},
			},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2x"},
		"alias3": {Auto: "cmd3"},
	})

	var snapst2 snapstate.SnapState
	err = snapstate.Get(s.state, "other-snap", &snapst2)
	c.Assert(err, IsNil)

	c.Check(snapst2.AutoAliasesDisabled, Equals, false)
	c.Check(snapst2.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias3": {Auto: "cmd3x"},
	})

	c.Check(t.Log(), HasLen, 1)
	c.Check(t.Log()[0], Matches, `.* ERROR cannot reinstate alias state because of conflicts, disabling: cannot enable alias "alias3" for "alias-snap", already enabled for "other-snap".*`)
}

func (s *snapmgrTestSuite) TestDoUndoDisableAliases(c *C) {
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

	t := s.state.NewTask("disable-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	c.Check(t.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))

	expected := fakeOps{
		{
			op: "update-aliases",
			rmAliases: []*backend.Alias{
				{"alias1", "alias-snap.cmd5"},
				{"alias2", "alias-snap.cmd2"},
				{"alias3", "alias-snap.cmd3"},
			},
		},
		{
			op: "update-aliases",
			aliases: []*backend.Alias{
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
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Manual: "cmd5", Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias3": {Manual: "cmd3"},
	})
}

func (s *snapmgrTestSuite) TestDoPreferAliases(c *C) {
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
			"alias3": {Auto: "cmd3"},
		},
	})
	snapstate.Set(s.state, "other-alias-snap1", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-alias-snap1", Revision: snap.R(3)},
		},
		Current: snap.R(3),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
		},
	})
	snapstate.Set(s.state, "other-alias-snap2", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-alias-snap2", Revision: snap.R(3)},
		},
		Current:        snap.R(3),
		Active:         true,
		AliasesPending: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias2": {Manual: "cmd2"},
			"aliasx": {Manual: "cmdx"},
		},
	})
	snapstate.Set(s.state, "other-alias-snap3", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-alias-snap3", Revision: snap.R(3)},
		},
		Current: snap.R(3),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias3": {Manual: "cmd5", Auto: "cmd3"},
		},
	})

	t := s.state.NewTask("prefer-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()
	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}
	s.state.Lock()
	c.Check(t.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, []string{"update-aliases", "update-aliases", "update-aliases"})
	c.Assert(s.fakeBackend.ops[0].aliases, HasLen, 0)
	c.Assert(s.fakeBackend.ops[0].rmAliases, HasLen, 1)
	c.Assert(s.fakeBackend.ops[1].aliases, HasLen, 0)
	c.Assert(s.fakeBackend.ops[1].rmAliases, HasLen, 1)
	c.Assert(s.fakeBackend.ops[2], DeepEquals, fakeOp{
		op: "update-aliases",
		aliases: []*backend.Alias{
			{"alias1", "alias-snap.cmd1"},
			{"alias2", "alias-snap.cmd2"},
			{"alias3", "alias-snap.cmd3"},
		},
	})

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias3": {Auto: "cmd3"},
	})

	var otherst1 snapstate.SnapState
	err = snapstate.Get(s.state, "other-alias-snap1", &otherst1)
	c.Assert(err, IsNil)
	c.Check(otherst1.AutoAliasesDisabled, Equals, true)
	c.Check(otherst1.AliasesPending, Equals, false)
	c.Check(otherst1.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
	})

	var otherst2 snapstate.SnapState
	err = snapstate.Get(s.state, "other-alias-snap2", &otherst2)
	c.Assert(err, IsNil)
	c.Check(otherst2.AutoAliasesDisabled, Equals, false)
	c.Check(otherst2.AliasesPending, Equals, true)
	c.Check(otherst2.Aliases, HasLen, 0)

	var otherst3 snapstate.SnapState
	err = snapstate.Get(s.state, "other-alias-snap3", &otherst3)
	c.Assert(err, IsNil)
	c.Check(otherst3.AutoAliasesDisabled, Equals, true)
	c.Check(otherst3.AliasesPending, Equals, false)
	c.Check(otherst3.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias3": {Auto: "cmd3"},
	})

	var trace traceData
	err = chg.Get("api-data", &trace)
	c.Assert(err, IsNil)
	c.Check(trace.Added, HasLen, 3)
	c.Check(trace.Removed, HasLen, 4)
}

func (s *snapmgrTestSuite) TestDoUndoPreferAliases(c *C) {
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
			"alias3": {Auto: "cmd3"},
		},
	})
	snapstate.Set(s.state, "other-alias-snap1", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-alias-snap1", Revision: snap.R(3)},
		},
		Current: snap.R(3),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
		},
	})
	snapstate.Set(s.state, "other-alias-snap2", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-alias-snap2", Revision: snap.R(3)},
		},
		Current:        snap.R(3),
		Active:         true,
		AliasesPending: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias2": {Manual: "cmd2"},
			"aliasx": {Manual: "cmdx"},
		},
	})
	snapstate.Set(s.state, "other-alias-snap3", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-alias-snap3", Revision: snap.R(3)},
		},
		Current: snap.R(3),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias3": {Manual: "cmd5", Auto: "cmd3"},
		},
	})

	t := s.state.NewTask("prefer-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()
	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}
	s.state.Lock()
	c.Check(t.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))

	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), HasLen, 6)
	c.Assert(s.fakeBackend.ops[3], DeepEquals, fakeOp{
		op: "update-aliases",
		rmAliases: []*backend.Alias{
			{"alias1", "alias-snap.cmd1"},
			{"alias2", "alias-snap.cmd2"},
			{"alias3", "alias-snap.cmd3"},
		},
	})
	c.Assert(s.fakeBackend.ops[4].aliases, HasLen, 1)
	c.Assert(s.fakeBackend.ops[4].rmAliases, HasLen, 0)
	c.Assert(s.fakeBackend.ops[5].aliases, HasLen, 1)
	c.Assert(s.fakeBackend.ops[5].rmAliases, HasLen, 0)

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias3": {Auto: "cmd3"},
	})

	var otherst1 snapstate.SnapState
	err = snapstate.Get(s.state, "other-alias-snap1", &otherst1)
	c.Assert(err, IsNil)
	c.Check(otherst1.AutoAliasesDisabled, Equals, false)
	c.Check(otherst1.AliasesPending, Equals, false)
	c.Check(otherst1.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
	})

	var otherst2 snapstate.SnapState
	err = snapstate.Get(s.state, "other-alias-snap2", &otherst2)
	c.Assert(err, IsNil)
	c.Check(otherst2.AutoAliasesDisabled, Equals, false)
	c.Check(otherst2.AliasesPending, Equals, true)
	c.Check(otherst2.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias2": {Manual: "cmd2"},
	})

	var otherst3 snapstate.SnapState
	err = snapstate.Get(s.state, "other-alias-snap3", &otherst3)
	c.Assert(err, IsNil)
	c.Check(otherst3.AutoAliasesDisabled, Equals, false)
	c.Check(otherst3.AliasesPending, Equals, false)
	c.Check(otherst3.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias3": {Manual: "cmd5", Auto: "cmd3"},
	})
}

func (s *snapmgrTestSuite) TestDoUndoPreferAliasesConflict(c *C) {
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
			"alias3": {Auto: "cmd3"},
		},
	})
	snapstate.Set(s.state, "other-alias-snap1", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-alias-snap1", Revision: snap.R(3)},
		},
		Current: snap.R(3),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
		},
	})
	snapstate.Set(s.state, "other-alias-snap2", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-alias-snap2", Revision: snap.R(3)},
		},
		Current:        snap.R(3),
		Active:         true,
		AliasesPending: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias2": {Manual: "cmd2"},
			"aliasx": {Manual: "cmdx"},
		},
	})
	snapstate.Set(s.state, "other-alias-snap3", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-alias-snap3", Revision: snap.R(3)},
		},
		Current: snap.R(3),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias3": {Manual: "cmd5", Auto: "cmd3"},
		},
	})

	conflictAlias5 := func(t *state.Task, _ *tomb.Tomb) error {
		st := t.State()
		st.Lock()
		defer st.Unlock()

		var snapst1, snapst2 snapstate.SnapState
		err := snapstate.Get(st, "other-alias-snap1", &snapst1)
		c.Assert(err, IsNil)
		err = snapstate.Get(st, "other-alias-snap2", &snapst2)
		c.Assert(err, IsNil)
		snapst1.Aliases = map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias5": {Auto: "cmd5"},
		}
		snapst2.Aliases = map[string]*snapstate.AliasTarget{
			"alias5": {Manual: "cmd5"},
		}
		snapstate.Set(st, "other-alias-snap1", &snapst1)
		snapstate.Set(st, "other-alias-snap2", &snapst2)

		return nil
	}

	s.o.TaskRunner().AddHandler("conflict-alias5", conflictAlias5, nil)

	t := s.state.NewTask("prefer-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	tconflict5 := s.state.NewTask("conflict-alias5", "create conflict on alias5")
	tconflict5.WaitFor(t)
	chg.AddTask(tconflict5)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(tconflict5)
	chg.AddTask(terr)

	s.state.Unlock()
	for i := 0; i < 5; i++ {
		s.se.Ensure()
		s.se.Wait()
	}
	s.state.Lock()
	c.Check(t.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))

	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), HasLen, 5)
	c.Assert(s.fakeBackend.ops[3], DeepEquals, fakeOp{
		op: "update-aliases",
		rmAliases: []*backend.Alias{
			{"alias1", "alias-snap.cmd1"},
			{"alias2", "alias-snap.cmd2"},
			{"alias3", "alias-snap.cmd3"},
		},
	})
	c.Assert(s.fakeBackend.ops[4], DeepEquals, fakeOp{
		op: "update-aliases",
		aliases: []*backend.Alias{
			{"alias3", "other-alias-snap3.cmd5"},
		},
	})

	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "alias-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias3": {Auto: "cmd3"},
	})

	var otherst1 snapstate.SnapState
	err = snapstate.Get(s.state, "other-alias-snap1", &otherst1)
	c.Assert(err, IsNil)
	c.Check(otherst1.AutoAliasesDisabled, Equals, true)
	c.Check(otherst1.AliasesPending, Equals, false)
	c.Check(otherst1.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias5": {Auto: "cmd5"},
	})

	var otherst2 snapstate.SnapState
	err = snapstate.Get(s.state, "other-alias-snap2", &otherst2)
	c.Assert(err, IsNil)
	c.Check(otherst2.AutoAliasesDisabled, Equals, false)
	c.Check(otherst2.AliasesPending, Equals, true)
	c.Check(otherst2.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias5": {Manual: "cmd5"},
	})

	var otherst3 snapstate.SnapState
	err = snapstate.Get(s.state, "other-alias-snap3", &otherst3)
	c.Assert(err, IsNil)
	c.Check(otherst3.AutoAliasesDisabled, Equals, false)
	c.Check(otherst3.AliasesPending, Equals, false)
	c.Check(otherst3.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias3": {Manual: "cmd5", Auto: "cmd3"},
	})
}
