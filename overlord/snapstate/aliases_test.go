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
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func (s *snapmgrTestSuite) TestDoSetupAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})
	s.state.Set("aliases", map[string]map[string]string{
		"alias-snap": {
			"alias1": "enabled",
		},
	})

	t := s.state.NewTask("setup-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.snapmgr.Ensure()
	s.snapmgr.Wait()

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
}

func (s *snapmgrTestSuite) TestDoUndoSetupAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})
	s.state.Set("aliases", map[string]map[string]string{
		"alias-snap": {
			"alias1": "enabled",
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
		s.snapmgr.Ensure()
		s.snapmgr.Wait()
	}

	s.state.Lock()

	c.Check(t.Status(), Equals, state.UndoneStatus)
	expected := fakeOps{
		{
			op:      "update-aliases",
			aliases: []*backend.Alias{{"alias1", "alias-snap.cmd1"}},
		},
		{
			op:      "matching-aliases",
			aliases: []*backend.Alias{{"alias1", "alias-snap.cmd1"}},
		},
		{
			op:        "update-aliases",
			rmAliases: []*backend.Alias{{"alias1", "alias-snap.cmd1"}},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)
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

	ts, err := snapstate.Alias(s.state, "some-snap", []string{"alias"})
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

	chg := s.state.NewChange("alias", "enable an alias")
	ts, err := snapstate.Alias(s.state, "alias-snap", []string{"alias1"})
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

	var allAliases map[string]map[string]string
	err = s.state.Get("aliases", &allAliases)
	c.Assert(err, IsNil)
	c.Check(allAliases, DeepEquals, map[string]map[string]string{
		"alias-snap": {"alias1": "enabled"},
	})
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

	_, err = snapstate.Alias(s.state, "some-snap", []string{"alias1"})
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
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

	ts, err := snapstate.Alias(s.state, "some-snap", []string{"alias1"})
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("alias", "...").AddAll(ts)

	_, err = snapstate.Update(s.state, "some-snap", "some-channel", snap.R(0), s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestAliasTotalUndoRunthrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})
	s.state.Set("aliases", map[string]map[string]string{
		"other-snap": {"alias7": "enabled"},
	})

	chg := s.state.NewChange("alias", "enable an alias")
	ts, err := snapstate.Alias(s.state, "alias-snap", []string{"alias1"})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	tasks := ts.Tasks()
	last := tasks[len(tasks)-1]

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(last)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.snapmgr.Ensure()
		s.snapmgr.Wait()
	}

	s.state.Lock()

	c.Check(chg.Status(), Equals, state.ErrorStatus, Commentf("%v", chg.Err()))
	expected := fakeOps{
		{
			op:      "update-aliases",
			aliases: []*backend.Alias{{"alias1", "alias-snap.cmd1"}},
		},
		{
			op:      "matching-aliases",
			aliases: []*backend.Alias{{"alias1", "alias-snap.cmd1"}},
		},
		{
			op:        "update-aliases",
			rmAliases: []*backend.Alias{{"alias1", "alias-snap.cmd1"}},
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Check(s.fakeBackend.ops, DeepEquals, expected)

	var allAliases map[string]map[string]string
	err = s.state.Get("aliases", &allAliases)
	c.Assert(err, IsNil)
	c.Check(allAliases, DeepEquals, map[string]map[string]string{
		"other-snap": {"alias7": "enabled"},
	})
}

func (s *snapmgrTestSuite) TestAliasNoAlias(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	chg := s.state.NewChange("alias", "enable an alias")
	ts, err := snapstate.Alias(s.state, "some-snap", []string{"alias1"})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()

	s.snapmgr.Ensure()
	s.snapmgr.Wait()

	s.state.Lock()

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot enable alias "alias1" for "some-snap", no such alias.*`)
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
	s.state.Set("aliases", map[string]map[string]string{
		"other-snap": {"alias1": "enabled"},
	})

	chg := s.state.NewChange("alias", "enable an alias")
	ts, err := snapstate.Alias(s.state, "alias-snap", []string{"alias1"})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()

	s.snapmgr.Ensure()
	s.snapmgr.Wait()

	s.state.Lock()

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot enable alias "alias1" for "alias-snap", already enabled for "other-snap".*`)
}

func (s *snapmgrTestSuite) TestAliasSnapCommandSpaceConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})
	// the command namespace of this one will conflict
	snapstate.Set(s.state, "alias1", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias1", Revision: snap.R(3)},
		},
		Current: snap.R(3),
	})

	chg := s.state.NewChange("alias", "enable an alias")
	ts, err := snapstate.Alias(s.state, "alias-snap", []string{"alias1.cmd1"})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()

	s.snapmgr.Ensure()
	s.snapmgr.Wait()

	s.state.Lock()

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot enable alias "alias1.cmd1" for "alias-snap", it conflicts with the command namespace of installed snap "alias1".*`)
}

func (s *snapmgrTestSuite) TestDoClearAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("aliases", map[string]map[string]string{
		"alias-snap": {"alias1": "enabled"},
		"other-snap": {"alias2": "enabled"},
	})

	t := s.state.NewTask("clear-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.snapmgr.Ensure()
	s.snapmgr.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	var allAliases map[string]map[string]string
	err := s.state.Get("aliases", &allAliases)
	c.Assert(err, IsNil)
	c.Check(allAliases, DeepEquals, map[string]map[string]string{
		"other-snap": {"alias2": "enabled"},
	})
}

func (s *snapmgrTestSuite) TestDoUndoClearAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("aliases", map[string]map[string]string{
		"alias-snap": {"alias1": "enabled"},
		"other-snap": {"alias2": "enabled"},
	})

	t := s.state.NewTask("clear-aliases", "test")
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
		s.snapmgr.Ensure()
		s.snapmgr.Wait()
	}

	s.state.Lock()

	c.Check(t.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))

	var allAliases map[string]map[string]string
	err := s.state.Get("aliases", &allAliases)
	c.Assert(err, IsNil)

	c.Check(allAliases, DeepEquals, map[string]map[string]string{
		"alias-snap": {"alias1": "enabled"},
		"other-snap": {"alias2": "enabled"},
	})
}

func (s *snapmgrTestSuite) TestDoUndoClearAliasesConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("aliases", map[string]map[string]string{
		"alias-snap": {
			"alias1": "enabled",
			"alias9": "enabled",
		},
		"other-snap": {"alias2": "enabled"},
	})

	grabAlias9 := func(t *state.Task, _ *tomb.Tomb) error {
		st := t.State()
		st.Lock()
		defer st.Unlock()

		var allAliases map[string]map[string]string
		err := st.Get("aliases", &allAliases)
		c.Assert(err, IsNil)
		c.Assert(allAliases, DeepEquals, map[string]map[string]string{
			"other-snap": {"alias2": "enabled"},
		})

		st.Set("aliases", map[string]map[string]string{
			"other-snap": {
				"alias2": "enabled",
				"alias9": "enabled",
			},
		})
		return nil
	}

	s.snapmgr.AddAdhocTaskHandler("grab-alias9", grabAlias9, nil)

	t := s.state.NewTask("clear-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	tgrab9 := s.state.NewTask("grab-alias9", "grab alias9 for other-snap")
	tgrab9.WaitFor(t)
	chg.AddTask(tgrab9)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(tgrab9)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 5; i++ {
		s.snapmgr.Ensure()
		s.snapmgr.Wait()
	}

	s.state.Lock()

	c.Check(t.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))

	var allAliases map[string]map[string]string
	err := s.state.Get("aliases", &allAliases)
	c.Assert(err, IsNil)

	c.Check(allAliases, DeepEquals, map[string]map[string]string{
		"alias-snap": {"alias1": "enabled"},
		"other-snap": {
			"alias2": "enabled",
			"alias9": "enabled",
		},
	})

	c.Check(t.Log(), HasLen, 1)
	c.Check(t.Log()[0], Matches, `.* ERROR cannot enable alias "alias9" for "alias-snap", already enabled for "other-snap"`)
}
