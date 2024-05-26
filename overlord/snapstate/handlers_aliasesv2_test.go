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

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func (s *snapmgrTestSuite) TestDoRemoveAliasesRefreshAppAwarenessDisabled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current:             snap.R(11),
		Active:              true,
		AutoAliasesDisabled: true,
		AliasesPending:      false,
		Aliases: map[string]*snapstate.AliasTarget{
			"manual1": {Manual: "cmd1"},
		},
	})

	// enable experimental refresh-app-awareness-ux
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness-ux", true)
	tr.Commit()
	// With refresh-app-awareness disabled
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness", false)
	tr.Commit()

	t := s.state.NewTask("remove-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	t.Set("remove-reason", "refresh")
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus)
	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "alias-snap",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.AliasesPending, Equals, true)
}

func (s *snapmgrTestSuite) TestDoUndoRemoveAliasesRefreshAppAwarenessDisabled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current:             snap.R(11),
		Active:              true,
		AutoAliasesDisabled: true,
		AliasesPending:      false,
		Aliases: map[string]*snapstate.AliasTarget{
			"manual1": {Manual: "cmd1"},
		},
	})

	// enable experimental refresh-app-awareness-ux
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness-ux", true)
	tr.Commit()
	// With refresh-app-awareness disabled
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness", false)
	tr.Commit()

	t := s.state.NewTask("remove-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	t.Set("remove-reason", "refresh")
	chg := s.state.NewChange("sample", "...")
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
			op:   "remove-snap-aliases",
			name: "alias-snap",
		},
		{
			op:      "update-aliases",
			aliases: []*backend.Alias{{Name: "manual1", Target: "alias-snap.cmd1"}},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.AliasesPending, Equals, false)
}

func (s *snapmgrTestSuite) TestDoRemoveAliasesExcludeFromRefreshAppAwareness(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current:             snap.R(11),
		Active:              true,
		AutoAliasesDisabled: true,
		AliasesPending:      false,
		Aliases: map[string]*snapstate.AliasTarget{
			"manual1": {Manual: "cmd1"},
		},
	})

	// enable experimental refresh-app-awareness-ux
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness-ux", true)
	tr.Commit()
	// With excluded from refresh-app-awareness
	restore := snapstate.MockExcludeFromRefreshAppAwareness(func(t snap.Type) bool {
		return true
	})
	defer restore()

	t := s.state.NewTask("remove-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	t.Set("remove-reason", "refresh")
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus)
	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "alias-snap",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.AliasesPending, Equals, true)
}

func (s *snapmgrTestSuite) TestDoRemoveAliasesRemoveReasonNotRefresh(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current:             snap.R(11),
		Active:              true,
		AutoAliasesDisabled: true,
		AliasesPending:      false,
		Aliases: map[string]*snapstate.AliasTarget{
			"manual1": {Manual: "cmd1"},
		},
	})

	t := s.state.NewTask("remove-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	t.Set("remove-reason", "not-refresh")
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus)
	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "alias-snap",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.AliasesPending, Equals, true)
}

func (s *snapmgrTestSuite) TestDoRemoveAliasesSkipped(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current:             snap.R(11),
		Active:              true,
		AutoAliasesDisabled: true,
		AliasesPending:      false,
		Aliases: map[string]*snapstate.AliasTarget{
			"manual1": {Manual: "cmd1"},
		},
	})

	// enable experimental refresh-app-awareness-ux
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness-ux", true)
	tr.Commit()
	// refresh-app-awareness should be enabled by default
	t := s.state.NewTask("remove-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	t.Set("remove-reason", "refresh")
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus)
	// no actual removal is done
	c.Assert(len(s.fakeBackend.ops), Equals, 0)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	// no aliases were removed, check AliasesPending is false
	c.Check(snapst.AliasesPending, Equals, false)
}

func (s *snapmgrTestSuite) TestDoUndoRemoveAliasesSkipped(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current:             snap.R(11),
		Active:              true,
		AutoAliasesDisabled: true,
		AliasesPending:      false,
		Aliases: map[string]*snapstate.AliasTarget{
			"manual1": {Manual: "cmd1"},
		},
	})

	// enable experimental refresh-app-awareness-ux
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness-ux", true)
	tr.Commit()
	// refresh-app-awareness should be enabled by default
	t := s.state.NewTask("remove-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	t.Set("remove-reason", "refresh")
	chg := s.state.NewChange("sample", "...")
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
	// no actual removal is done so no removal is undone
	c.Assert(len(s.fakeBackend.ops), Equals, 0)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.AliasesPending, Equals, false)
}

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current: snap.R(11),
		Active:  true,
	})

	t := s.state.NewTask("set-auto-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
	chg := s.state.NewChange("sample", "...")
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
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "other-snap", Revision: snap.R(3)},
		}),
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
	chg := s.state.NewChange("sample", "...")
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "other-snap", Revision: snap.R(3)},
		}),
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
	chg := s.state.NewChange("sample", "...")
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
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current: snap.R(11),
		Active:  true,
	})

	t := s.state.NewTask("set-auto-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
		Flags:    snapstate.Flags{Unaliased: true},
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current:        snap.R(11),
		Active:         true,
		AliasesPending: false,
	})

	t := s.state.NewTask("set-auto-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
		Flags:    snapstate.Flags{Unaliased: true},
	})
	chg := s.state.NewChange("sample", "...")
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
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, HasLen, 0)
}

func (s *snapmgrTestSuite) TestDoSetupAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus)
	expected := fakeOps{
		{
			op:      "update-aliases",
			aliases: []*backend.Alias{{Name: "manual1", Target: "alias-snap.cmd1"}},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.AliasesPending, Equals, false)
}

func (s *snapmgrTestSuite) TestDoUndoSetupAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
	chg := s.state.NewChange("sample", "...")
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
			aliases: []*backend.Alias{{Name: "manual1", Target: "alias-snap.cmd1"}},
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
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.AliasesPending, Equals, true)
}

func (s *snapmgrTestSuite) TestDoSetupAliasesAuto(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus)
	expected := fakeOps{
		{
			op:      "update-aliases",
			aliases: []*backend.Alias{{Name: "alias1", Target: "alias-snap.cmd1"}},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
}

func (s *snapmgrTestSuite) TestDoUndoSetupAliasesAuto(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
	chg := s.state.NewChange("sample", "...")
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
			aliases: []*backend.Alias{{Name: "alias1", Target: "alias-snap.cmd1"}},
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
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, true)
}

func (s *snapmgrTestSuite) TestDoSetupAliasesNothing(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current: snap.R(11),
		Active:  true,
	})

	t := s.state.NewTask("setup-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("sample", "...")
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
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
}

func (s *snapmgrTestSuite) TestDoUndoSetupAliasesNothing(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current: snap.R(11),
		Active:  true,
	})

	t := s.state.NewTask("setup-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("sample", "...")
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
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, true)
}

func (s *snapmgrTestSuite) TestDoSetupAliasesAutoPruneOldAliases(c *C) {
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current:             snap.R(11),
		Active:              true,
		AutoAliasesDisabled: false,
		AliasesPending:      false,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2x"},
			"alias3": {Auto: "cmd3"},
		},
	})

	snapsup := snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	}

	// enable experimental refresh-app-awareness-ux
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness-ux", true)
	tr.Commit()
	// remove-aliases + refresh-app-awareness task triggers pruning
	// refresh-app-awareness should be enabled by default
	removeAliasesTask := s.state.NewTask("remove-aliases", "test")
	removeAliasesTask.Set("snap-setup", &snapsup)
	removeAliasesTask.Set("remove-reason", "refresh")

	setAutoAliasesTask := s.state.NewTask("set-auto-aliases", "test")
	setAutoAliasesTask.Set("snap-setup", &snapsup)
	setAutoAliasesTask.WaitFor(removeAliasesTask)

	setupAliasesTask := s.state.NewTask("setup-aliases", "test")
	setupAliasesTask.Set("snap-setup", &snapsup)
	setupAliasesTask.WaitFor(setAutoAliasesTask)

	chg := s.state.NewChange("sample", "...")
	chg.AddTask(removeAliasesTask)
	chg.AddTask(setAutoAliasesTask)
	chg.AddTask(setupAliasesTask)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(removeAliasesTask.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))
	c.Check(setAutoAliasesTask.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))
	c.Check(setupAliasesTask.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	expected := fakeOps{
		{
			// notice no "remove-snap-aliases" op because it was skipped
			// setup-aliases prunes old aliases and only updates changed aliases
			op: "update-aliases",
			aliases: []*backend.Alias{
				{Name: "alias2", Target: "alias-snap.cmd2"},
				{Name: "alias4", Target: "alias-snap.cmd4"},
			},
			rmAliases: []*backend.Alias{
				{Name: "alias2", Target: "alias-snap.cmd2x"},
				{Name: "alias3", Target: "alias-snap.cmd3"},
			},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias4": {Auto: "cmd4"},
	})
}

func (s *snapmgrTestSuite) TestDoUndoSetupAliasesAutoPruneOldAliases(c *C) {
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current:             snap.R(11),
		Active:              true,
		AutoAliasesDisabled: false,
		AliasesPending:      false,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2x"},
			"alias3": {Auto: "cmd3"},
		},
	})

	snapsup := snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	}

	// enable experimental refresh-app-awareness-ux
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness-ux", true)
	tr.Commit()
	// remove-aliases + refresh-app-awareness task triggers pruning
	// refresh-app-awareness should be enabled by default
	removeAliasesTask := s.state.NewTask("remove-aliases", "test")
	removeAliasesTask.Set("snap-setup", &snapsup)
	removeAliasesTask.Set("remove-reason", "refresh")

	setAutoAliasesTask := s.state.NewTask("set-auto-aliases", "test")
	setAutoAliasesTask.Set("snap-setup", &snapsup)
	setAutoAliasesTask.WaitFor(removeAliasesTask)

	setupAliasesTask := s.state.NewTask("setup-aliases", "test")
	setupAliasesTask.Set("snap-setup", &snapsup)
	setupAliasesTask.WaitFor(setAutoAliasesTask)

	errTask := s.state.NewTask("error-trigger", "provoking total undo")
	errTask.WaitFor(setupAliasesTask)

	chg := s.state.NewChange("sample", "...")
	chg.AddTask(removeAliasesTask)
	chg.AddTask(setAutoAliasesTask)
	chg.AddTask(setupAliasesTask)
	chg.AddTask(errTask)

	s.state.Unlock()

	for i := 0; i < 10; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(removeAliasesTask.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))
	c.Check(setAutoAliasesTask.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))
	c.Check(setupAliasesTask.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))

	expected := fakeOps{
		{
			// notice no "remove-snap-aliases" op because it was skipped
			// setup-aliases prunes old aliases and only updates changed aliases
			op: "update-aliases",
			aliases: []*backend.Alias{
				{Name: "alias2", Target: "alias-snap.cmd2"},
				{Name: "alias4", Target: "alias-snap.cmd4"},
			},
			rmAliases: []*backend.Alias{
				{Name: "alias2", Target: "alias-snap.cmd2x"},
				{Name: "alias3", Target: "alias-snap.cmd3"},
			},
		},
		{
			// notice no "remove-snap-aliases" op because it was skipped
			// undo
			op: "update-aliases",
			aliases: []*backend.Alias{
				{Name: "alias2", Target: "alias-snap.cmd2x"},
				{Name: "alias3", Target: "alias-snap.cmd3"},
			},
			rmAliases: []*backend.Alias{
				{Name: "alias2", Target: "alias-snap.cmd2"},
				{Name: "alias4", Target: "alias-snap.cmd4"},
			},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2x"},
		"alias3": {Auto: "cmd3"},
	})
}

func (s *snapmgrTestSuite) TestDoUndoSetupAliasesAutoErrorMidwayPruneOldAliases(c *C) {
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current:             snap.R(11),
		Active:              true,
		AutoAliasesDisabled: false,
		AliasesPending:      false,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2x"},
			"alias3": {Auto: "cmd3"},
		},
	})

	snapsup := snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	}

	// enable experimental refresh-app-awareness-ux
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness-ux", true)
	tr.Commit()
	// remove-aliases + refresh-app-awareness task triggers pruning
	// refresh-app-awareness should be enabled by default
	removeAliasesTask := s.state.NewTask("remove-aliases", "test")
	removeAliasesTask.Set("snap-setup", &snapsup)
	removeAliasesTask.Set("remove-reason", "refresh")

	setAutoAliasesTask := s.state.NewTask("set-auto-aliases", "test")
	setAutoAliasesTask.Set("snap-setup", &snapsup)
	setAutoAliasesTask.WaitFor(removeAliasesTask)

	setupAliasesTask := s.state.NewTask("setup-aliases", "test")
	setupAliasesTask.Set("snap-setup", &snapsup)
	setupAliasesTask.WaitFor(setAutoAliasesTask)

	chg := s.state.NewChange("sample", "...")
	chg.AddTask(removeAliasesTask)
	chg.AddTask(setAutoAliasesTask)
	chg.AddTask(setupAliasesTask)

	expected := fakeOps{
		{
			// notice no "remove-snap-aliases" op because it was skipped
			// setup-aliases prunes old aliases and only updates changed aliases
			op: "update-aliases",
			aliases: []*backend.Alias{
				{Name: "alias2", Target: "alias-snap.cmd2"},
				{Name: "alias4", Target: "alias-snap.cmd4"},
			},
			rmAliases: []*backend.Alias{
				{Name: "alias2", Target: "alias-snap.cmd2x"},
				{Name: "alias3", Target: "alias-snap.cmd3"},
			},
		},
		{
			// notice no "remove-snap-aliases" op because it was skipped
			// undo
			op: "update-aliases",
			aliases: []*backend.Alias{
				{Name: "alias2", Target: "alias-snap.cmd2x"},
				{Name: "alias3", Target: "alias-snap.cmd3"},
			},
			rmAliases: []*backend.Alias{
				{Name: "alias2", Target: "alias-snap.cmd2"},
				{Name: "alias4", Target: "alias-snap.cmd4"},
			},
		},
	}

	var backendCalledFromSetupAliases int
	var backendCalledFromSetAutoAliases int
	s.fakeBackend.maybeInjectErr = func(op *fakeOp) error {
		var currentTask *state.Task
		for _, t := range chg.Tasks() {
			if t.Status() == state.DoingStatus || t.Status() == state.UndoingStatus {
				currentTask = t
				break
			}
		}
		if currentTask == nil {
			c.Errorf("unexpected nil task")
		}

		switch currentTask.Kind() {
		case "setup-aliases":
			backendCalledFromSetupAliases++
			// double-check setup-aliases do did the correct op
			c.Check(currentTask.Status(), Equals, state.DoingStatus)
			c.Check(op, DeepEquals, &expected[0])
			// trigger error in the middle of setup-aliases
			return fmt.Errorf("setup-aliases failed in the middle")
		case "set-auto-aliases":
			backendCalledFromSetAutoAliases++
			// double-check set-auto-aliases undo did the correct op
			c.Check(currentTask.Status(), Equals, state.UndoingStatus)
			c.Check(op, DeepEquals, &expected[1])
		default:
			c.Errorf("unexpected task: %s", currentTask.Kind())
		}

		return nil
	}

	s.state.Unlock()

	for i := 0; i < 10; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(removeAliasesTask.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))
	c.Check(setAutoAliasesTask.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))
	c.Check(setupAliasesTask.Status(), Equals, state.ErrorStatus, Commentf("%v", chg.Err()))

	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	c.Assert(backendCalledFromSetupAliases, Equals, 1)
	c.Assert(backendCalledFromSetAutoAliases, Equals, 1)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2x"},
		"alias3": {Auto: "cmd3"},
	})
	c.Assert(chg.Err(), ErrorMatches, `(?s).*setup-aliases failed in the middle.*`)
}

func (s *snapmgrTestSuite) TestDoUndoSetupAliasesAutoPruneOldAliasesConflict(c *C) {
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current:             snap.R(11),
		Active:              true,
		AutoAliasesDisabled: false,
		AliasesPending:      false,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2x"},
			"alias3": {Auto: "cmd3"},
		},
	})

	otherSnapState := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "other-snap", Revision: snap.R(3)},
		}),
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

	snapsup := snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	}

	// enable experimental refresh-app-awareness-ux
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness-ux", true)
	tr.Commit()
	// remove-aliases + refresh-app-awareness task triggers pruning
	// refresh-app-awareness should be enabled by default
	removeAliasesTask := s.state.NewTask("remove-aliases", "test")
	removeAliasesTask.Set("snap-setup", &snapsup)
	removeAliasesTask.Set("remove-reason", "refresh")

	setAutoAliasesTask := s.state.NewTask("set-auto-aliases", "test")
	setAutoAliasesTask.Set("snap-setup", &snapsup)
	setAutoAliasesTask.WaitFor(removeAliasesTask)

	setupAliasesTask := s.state.NewTask("setup-aliases", "test")
	setupAliasesTask.Set("snap-setup", &snapsup)
	setupAliasesTask.WaitFor(setAutoAliasesTask)

	grab3Task := s.state.NewTask("grab-alias3", "grab alias3 for other-snap")
	grab3Task.WaitFor(setupAliasesTask)

	errTask := s.state.NewTask("error-trigger", "provoking total undo")
	errTask.WaitFor(setupAliasesTask)

	chg := s.state.NewChange("sample", "...")
	chg.AddTask(removeAliasesTask)
	chg.AddTask(setAutoAliasesTask)
	chg.AddTask(setupAliasesTask)
	chg.AddTask(grab3Task)
	chg.AddTask(errTask)

	s.state.Unlock()

	for i := 0; i < 10; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(removeAliasesTask.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))
	c.Check(setAutoAliasesTask.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))
	c.Check(setupAliasesTask.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))

	expected := fakeOps{
		{
			// setup-aliases prunes old aliases and only updates changed aliases
			op: "update-aliases",
			aliases: []*backend.Alias{
				{Name: "alias2", Target: "alias-snap.cmd2"},
				{Name: "alias4", Target: "alias-snap.cmd4"},
			},
			rmAliases: []*backend.Alias{
				{Name: "alias2", Target: "alias-snap.cmd2x"},
				{Name: "alias3", Target: "alias-snap.cmd3"},
			},
		},
		{
			// undo
			op: "update-aliases",
			rmAliases: []*backend.Alias{
				{Name: "alias1", Target: "alias-snap.cmd1"},
				{Name: "alias2", Target: "alias-snap.cmd2"},
				{Name: "alias4", Target: "alias-snap.cmd4"},
			},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2x"},
		"alias3": {Auto: "cmd3"},
	})

	c.Assert(setupAliasesTask.Log(), HasLen, 0)
	// set-auto-aliases undo should handle alias conflict
	c.Assert(setAutoAliasesTask.Log(), HasLen, 1)
	c.Check(setAutoAliasesTask.Log()[0], Matches, `.* ERROR cannot reinstate alias state because of conflicts, disabling: cannot enable alias "alias3" for "alias-snap", already enabled for "other-snap".*`)
}

func (s *snapmgrTestSuite) TestDoPruneAutoAliasesAuto(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
	chg := s.state.NewChange("sample", "...")
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
				{Name: "alias2", Target: "alias-snap.cmd2"},
				{Name: "alias3", Target: "alias-snap.cmd3"},
			},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
	})
}

func (s *snapmgrTestSuite) TestDoPruneAutoAliasesAutoPending(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	// pending: nothing to do on disk
	c.Assert(s.fakeBackend.ops, HasLen, 0)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
	})
}

func (s *snapmgrTestSuite) TestDoPruneAutoAliasesManualAndDisabled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
	chg := s.state.NewChange("sample", "...")
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
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
	chg := s.state.NewChange("sample", "...")
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
				{Name: "alias2", Target: "alias-snap.cmd2x"},
				{Name: "alias3", Target: "alias-snap.cmd3"},
			},
			aliases: []*backend.Alias{
				{Name: "alias2", Target: "alias-snap.cmd2"},
				{Name: "alias4", Target: "alias-snap.cmd4"},
			},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
	chg := s.state.NewChange("sample", "...")
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
				{Name: "alias2", Target: "alias-snap.cmd2x"},
				{Name: "alias3", Target: "alias-snap.cmd3"},
			},
			aliases: []*backend.Alias{
				{Name: "alias2", Target: "alias-snap.cmd2"},
				{Name: "alias4", Target: "alias-snap.cmd4"},
			},
		},
		{
			op: "update-aliases",
			aliases: []*backend.Alias{
				{Name: "alias2", Target: "alias-snap.cmd2x"},
				{Name: "alias3", Target: "alias-snap.cmd3"},
			},
			rmAliases: []*backend.Alias{
				{Name: "alias2", Target: "alias-snap.cmd2"},
				{Name: "alias4", Target: "alias-snap.cmd4"},
			},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current:        snap.R(11),
		Active:         true,
		AliasesPending: false,
	})

	t := s.state.NewTask("refresh-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("sample", "...")
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
				{Name: "alias1", Target: "alias-snap.cmd1"},
				{Name: "alias2", Target: "alias-snap.cmd2"},
				{Name: "alias4", Target: "alias-snap.cmd4"},
			},
		},
		{
			op: "update-aliases",
			rmAliases: []*backend.Alias{
				{Name: "alias1", Target: "alias-snap.cmd1"},
				{Name: "alias2", Target: "alias-snap.cmd2"},
				{Name: "alias4", Target: "alias-snap.cmd4"},
			},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	// pending: nothing to do on disk
	c.Assert(s.fakeBackend.ops, HasLen, 0)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
	chg := s.state.NewChange("sample", "...")
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
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current: snap.R(11),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
			"alias2": {Auto: "cmd2x"},
			"alias3": {Auto: "cmd3"},
		},
	})
	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "other-snap", Revision: snap.R(3)},
		}),
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
	chg := s.state.NewChange("sample", "...")
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
				{RealName: "other-snap", Revision: snap.R(3)},
			}),
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
	chg := s.state.NewChange("sample", "...")
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
				{Name: "alias2", Target: "alias-snap.cmd2x"},
				{Name: "alias3", Target: "alias-snap.cmd3"},
			},
			aliases: []*backend.Alias{
				{Name: "alias2", Target: "alias-snap.cmd2"},
				{Name: "alias4", Target: "alias-snap.cmd4"},
			},
		},
		{
			op: "update-aliases",
			rmAliases: []*backend.Alias{
				{Name: "alias1", Target: "alias-snap.cmd1"},
				{Name: "alias2", Target: "alias-snap.cmd2"},
				{Name: "alias4", Target: "alias-snap.cmd4"},
			},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2x"},
		"alias3": {Auto: "cmd3"},
	})

	var snapst2 snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "other-snap", &snapst2))


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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
	chg := s.state.NewChange("sample", "...")
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
				{Name: "alias1", Target: "alias-snap.cmd5"},
				{Name: "alias2", Target: "alias-snap.cmd2"},
				{Name: "alias3", Target: "alias-snap.cmd3"},
			},
		},
		{
			op: "update-aliases",
			aliases: []*backend.Alias{
				{Name: "alias1", Target: "alias-snap.cmd5"},
				{Name: "alias2", Target: "alias-snap.cmd2"},
				{Name: "alias3", Target: "alias-snap.cmd3"},
			},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "other-alias-snap1", Revision: snap.R(3)},
		}),
		Current: snap.R(3),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
		},
	})
	snapstate.Set(s.state, "other-alias-snap2", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "other-alias-snap2", Revision: snap.R(3)},
		}),
		Current:        snap.R(3),
		Active:         true,
		AliasesPending: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias2": {Manual: "cmd2"},
			"aliasx": {Manual: "cmdx"},
		},
	})
	snapstate.Set(s.state, "other-alias-snap3", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "other-alias-snap3", Revision: snap.R(3)},
		}),
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
	chg := s.state.NewChange("sample", "...")
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
			{Name: "alias1", Target: "alias-snap.cmd1"},
			{Name: "alias2", Target: "alias-snap.cmd2"},
			{Name: "alias3", Target: "alias-snap.cmd3"},
		},
	})

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias3": {Auto: "cmd3"},
	})

	var otherst1 snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "other-alias-snap1", &otherst1))

	c.Check(otherst1.AutoAliasesDisabled, Equals, true)
	c.Check(otherst1.AliasesPending, Equals, false)
	c.Check(otherst1.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
	})

	var otherst2 snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "other-alias-snap2", &otherst2))

	c.Check(otherst2.AutoAliasesDisabled, Equals, false)
	c.Check(otherst2.AliasesPending, Equals, true)
	c.Check(otherst2.Aliases, HasLen, 0)

	var otherst3 snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "other-alias-snap3", &otherst3))

	c.Check(otherst3.AutoAliasesDisabled, Equals, true)
	c.Check(otherst3.AliasesPending, Equals, false)
	c.Check(otherst3.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias3": {Auto: "cmd3"},
	})

	var trace traceData
	mylog.Check(chg.Get("api-data", &trace))

	c.Check(trace.Added, HasLen, 3)
	c.Check(trace.Removed, HasLen, 4)
}

func (s *snapmgrTestSuite) TestDoUndoPreferAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "other-alias-snap1", Revision: snap.R(3)},
		}),
		Current: snap.R(3),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
		},
	})
	snapstate.Set(s.state, "other-alias-snap2", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "other-alias-snap2", Revision: snap.R(3)},
		}),
		Current:        snap.R(3),
		Active:         true,
		AliasesPending: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias2": {Manual: "cmd2"},
			"aliasx": {Manual: "cmdx"},
		},
	})
	snapstate.Set(s.state, "other-alias-snap3", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "other-alias-snap3", Revision: snap.R(3)},
		}),
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
	chg := s.state.NewChange("sample", "...")
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
			{Name: "alias1", Target: "alias-snap.cmd1"},
			{Name: "alias2", Target: "alias-snap.cmd2"},
			{Name: "alias3", Target: "alias-snap.cmd3"},
		},
	})
	c.Assert(s.fakeBackend.ops[4].aliases, HasLen, 1)
	c.Assert(s.fakeBackend.ops[4].rmAliases, HasLen, 0)
	c.Assert(s.fakeBackend.ops[5].aliases, HasLen, 1)
	c.Assert(s.fakeBackend.ops[5].rmAliases, HasLen, 0)

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias3": {Auto: "cmd3"},
	})

	var otherst1 snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "other-alias-snap1", &otherst1))

	c.Check(otherst1.AutoAliasesDisabled, Equals, false)
	c.Check(otherst1.AliasesPending, Equals, false)
	c.Check(otherst1.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
	})

	var otherst2 snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "other-alias-snap2", &otherst2))

	c.Check(otherst2.AutoAliasesDisabled, Equals, false)
	c.Check(otherst2.AliasesPending, Equals, true)
	c.Check(otherst2.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias2": {Manual: "cmd2"},
	})

	var otherst3 snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "other-alias-snap3", &otherst3))

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "other-alias-snap1", Revision: snap.R(3)},
		}),
		Current: snap.R(3),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Auto: "cmd1"},
		},
	})
	snapstate.Set(s.state, "other-alias-snap2", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "other-alias-snap2", Revision: snap.R(3)},
		}),
		Current:        snap.R(3),
		Active:         true,
		AliasesPending: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias2": {Manual: "cmd2"},
			"aliasx": {Manual: "cmdx"},
		},
	})
	snapstate.Set(s.state, "other-alias-snap3", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "other-alias-snap3", Revision: snap.R(3)},
		}),
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
		mylog.Check(snapstate.Get(st, "other-alias-snap1", &snapst1))

		mylog.Check(snapstate.Get(st, "other-alias-snap2", &snapst2))

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
	chg := s.state.NewChange("sample", "...")
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
			{Name: "alias1", Target: "alias-snap.cmd1"},
			{Name: "alias2", Target: "alias-snap.cmd2"},
			{Name: "alias3", Target: "alias-snap.cmd3"},
		},
	})
	c.Assert(s.fakeBackend.ops[4], DeepEquals, fakeOp{
		op: "update-aliases",
		aliases: []*backend.Alias{
			{Name: "alias3", Target: "other-alias-snap3.cmd5"},
		},
	})

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias3": {Auto: "cmd3"},
	})

	var otherst1 snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "other-alias-snap1", &otherst1))

	c.Check(otherst1.AutoAliasesDisabled, Equals, true)
	c.Check(otherst1.AliasesPending, Equals, false)
	c.Check(otherst1.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias5": {Auto: "cmd5"},
	})

	var otherst2 snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "other-alias-snap2", &otherst2))

	c.Check(otherst2.AutoAliasesDisabled, Equals, false)
	c.Check(otherst2.AliasesPending, Equals, true)
	c.Check(otherst2.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias5": {Manual: "cmd5"},
	})

	var otherst3 snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "other-alias-snap3", &otherst3))

	c.Check(otherst3.AutoAliasesDisabled, Equals, false)
	c.Check(otherst3.AliasesPending, Equals, false)
	c.Check(otherst3.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias3": {Manual: "cmd5", Auto: "cmd3"},
	})
}

func (s *snapmgrTestSuite) TestDoSetAutoAliasesFirstInstallPrefer(c *C) {
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current: snap.R(11),
		Active:  true,
	})

	t := s.state.NewTask("set-auto-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
		Flags:    snapstate.Flags{Prefer: true},
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, true)
	c.Check(snapst.AliasesPending, Equals, true)
	c.Check(snapst.Aliases, DeepEquals, map[string]*snapstate.AliasTarget{
		"alias1": {Auto: "cmd1"},
		"alias2": {Auto: "cmd2"},
		"alias4": {Auto: "cmd4"},
	})
}

func (s *snapmgrTestSuite) TestDoUndoSetAutoAliasesFirstInstallPrefer(c *C) {
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		}),
		Current:        snap.R(11),
		Active:         true,
		AliasesPending: false,
	})

	t := s.state.NewTask("set-auto-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
		Flags:    snapstate.Flags{Prefer: true},
	})
	chg := s.state.NewChange("sample", "...")
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
	mylog.Check(snapstate.Get(s.state, "alias-snap", &snapst))


	c.Check(snapst.AutoAliasesDisabled, Equals, false)
	c.Check(snapst.AliasesPending, Equals, false)
	c.Check(snapst.Aliases, HasLen, 0)
}
