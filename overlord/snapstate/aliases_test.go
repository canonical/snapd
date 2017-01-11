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
	"fmt"

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

func (s *snapmgrTestSuite) TestDoSetupAliasesAuto(c *C) {
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
			"alias1": "auto",
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

func (s *snapmgrTestSuite) TestDoUndoSetupAliasesAuto(c *C) {
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
			"alias1": "auto",
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

func (s *snapmgrTestSuite) TestUpdateUnaliasChangeConflict(c *C) {
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

	_, err = snapstate.Unalias(s.state, "some-snap", []string{"alias1"})
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestUpdateResetAliasesChangeConflict(c *C) {
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

	_, err = snapstate.ResetAliases(s.state, "some-snap", []string{"alias1"})
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

func (s *snapmgrTestSuite) TestAliasAutoAliasConflict(c *C) {
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
		"other-snap": {"alias1": "auto"},
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
		"alias-snap": {"alias1": "enabled", "alias5": "auto"},
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
		"alias-snap": {"alias1": "enabled", "alias5": "auto"},
		"other-snap": {"alias2": "enabled"},
	})
}

func (s *snapmgrTestSuite) TestDoUndoClearAliasesConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("aliases", map[string]map[string]string{
		"alias-snap": {
			"alias1":  "enabled",
			"alias5":  "auto",
			"alias9":  "enabled",
			"alias10": "auto",
		},
		"other-snap": {"alias2": "enabled"},
	})

	grabAlias9_10 := func(t *state.Task, _ *tomb.Tomb) error {
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
				"alias2":  "enabled",
				"alias9":  "enabled",
				"alias10": "enabled",
			},
		})
		return nil
	}

	s.snapmgr.AddAdhocTaskHandler("grab-alias9_10", grabAlias9_10, nil)

	t := s.state.NewTask("clear-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	tgrab9_10 := s.state.NewTask("grab-alias9_10", "grab alias9&alias10 for other-snap")
	tgrab9_10.WaitFor(t)
	chg.AddTask(tgrab9_10)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(tgrab9_10)
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
		"alias-snap": {
			"alias1": "enabled",
			"alias5": "auto",
		},
		"other-snap": {
			"alias2":  "enabled",
			"alias9":  "enabled",
			"alias10": "enabled",
		},
	})

	c.Check(t.Log(), HasLen, 2)
	c.Check(t.Log()[0]+t.Log()[1], Matches, `.* ERROR cannot enable alias "alias9" for "alias-snap", already enabled for "other-snap".*`)
}

var statusesMatrix = []struct {
	alias        string
	beforeStatus string
	action       string
	status       string
	mutation     string
}{
	{"alias1", "", "alias", "enabled", "add"},
	{"alias1", "enabled", "alias", "enabled", "-"},
	{"alias1", "disabled", "alias", "enabled", "add"},
	{"alias1", "auto", "alias", "enabled", "-"},
	{"alias1", "", "unalias", "disabled", "-"},
	{"alias1", "enabled", "unalias", "disabled", "rm"},
	{"alias1", "disabled", "unalias", "disabled", "-"},
	{"alias1", "auto", "unalias", "disabled", "rm"},
	{"alias1", "", "reset", "", "-"},
	{"alias1", "enabled", "reset", "", "rm"},
	{"alias1", "disabled", "reset", "", "-"},
	{"alias1", "auto", "reset", "", "rm"}, // used to retire auto-aliases
	{"alias5", "", "reset", "auto", "add"},
	{"alias5", "enabled", "reset", "auto", "-"},
	{"alias5", "disabled", "reset", "auto", "add"},
	{"alias5", "auto", "reset", "auto", "-"},
	{"alias1gone", "", "reset", "", "-"},
	{"alias1gone", "enabled", "reset", "", "-"},
	{"alias1gone", "disabled", "reset", "", "-"},
	{"alias1gone", "auto", "reset", "", "-"},
	{"alias5gone", "", "reset", "", "-"},
	{"alias5gone", "enabled", "reset", "", "-"},
	{"alias5gone", "disabled", "reset", "", "-"},
	{"alias5gone", "auto", "reset", "", "-"},
}

func (s *snapmgrTestSuite) TestAliasMatrixRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	// alias1 is a non auto-alias
	// alias5 is an auto-alias
	// alias1gone is a non auto-alias and doesn't have an entry in the current snap revision anymore
	// alias5gone is an auto-alias and doesn't have an entry in the current snap revision anymore
	snapstate.AutoAliases = func(st *state.State, info *snap.Info) ([]string, error) {
		c.Check(info.Name(), Equals, "alias-snap")
		return []string{"alias5", "alias5gone"}, nil
	}
	cmds := map[string]string{
		"alias1": "cmd1",
		"alias5": "cmd5",
	}

	defer s.snapmgr.Stop()
	for _, scenario := range statusesMatrix {
		scenAlias := scenario.alias
		if scenario.beforeStatus != "" {
			s.state.Set("aliases", map[string]map[string]string{
				"alias-snap": {
					scenAlias: scenario.beforeStatus,
				},
			})
		} else {
			s.state.Set("aliases", nil)
		}

		chg := s.state.NewChange("scenario", "...")
		var err error
		var ts *state.TaskSet
		targets := []string{scenAlias}
		switch scenario.action {
		case "alias":
			ts, err = snapstate.Alias(s.state, "alias-snap", targets)
		case "unalias":
			ts, err = snapstate.Unalias(s.state, "alias-snap", targets)
		case "reset":
			ts, err = snapstate.ResetAliases(s.state, "alias-snap", targets)
		}
		c.Assert(err, IsNil)

		chg.AddAll(ts)

		s.state.Unlock()
		s.settle()
		s.state.Lock()

		c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("%#v: %v", scenario, chg.Err()))
		var aliases []*backend.Alias
		var rmAliases []*backend.Alias
		beAlias := &backend.Alias{Name: scenAlias, Target: fmt.Sprintf("alias-snap.%s", cmds[scenAlias])}
		switch scenario.mutation {
		case "-":
		case "add":
			aliases = []*backend.Alias{beAlias}
		case "rm":
			rmAliases = []*backend.Alias{beAlias}
		}

		comm := Commentf("%#v", scenario)
		expected := fakeOps{
			{
				op:        "update-aliases",
				aliases:   aliases,
				rmAliases: rmAliases,
			},
		}
		// start with an easier-to-read error if this fails:
		c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops(), comm)
		c.Check(s.fakeBackend.ops, DeepEquals, expected, comm)

		var allAliases map[string]map[string]string
		err = s.state.Get("aliases", &allAliases)
		c.Assert(err, IsNil)
		if scenario.status != "" {
			c.Check(allAliases, DeepEquals, map[string]map[string]string{
				"alias-snap": {scenAlias: scenario.status},
			}, comm)
		} else {
			c.Check(allAliases, HasLen, 0, comm)
		}

		s.fakeBackend.ops = nil
	}
}

func (s *snapmgrTestSuite) TestAliasMatrixTotalUndoRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	// alias1 is a non auto-alias
	// alias5 is an auto-alias
	// alias1gone is a non auto-alias and doesn't have an entry in the snap anymore
	// alias5gone is an auto-alias and doesn't have an entry in the snap any
	snapstate.AutoAliases = func(st *state.State, info *snap.Info) ([]string, error) {
		c.Check(info.Name(), Equals, "alias-snap")
		return []string{"alias5", "alias5gone"}, nil
	}
	cmds := map[string]string{
		"alias1": "cmd1",
		"alias5": "cmd5",
	}

	defer s.snapmgr.Stop()
	for _, scenario := range statusesMatrix {
		scenAlias := scenario.alias
		if scenario.beforeStatus != "" {
			s.state.Set("aliases", map[string]map[string]string{
				"alias-snap": {
					scenAlias: scenario.beforeStatus,
				},
			})
		} else {
			s.state.Set("aliases", nil)
		}

		chg := s.state.NewChange("scenario", "...")
		var err error
		var ts *state.TaskSet
		targets := []string{scenAlias}

		switch scenario.action {
		case "alias":
			ts, err = snapstate.Alias(s.state, "alias-snap", targets)
		case "unalias":
			ts, err = snapstate.Unalias(s.state, "alias-snap", targets)
		case "reset":
			ts, err = snapstate.ResetAliases(s.state, "alias-snap", targets)
		}
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

		c.Assert(chg.Status(), Equals, state.ErrorStatus, Commentf("%#v: %v", scenario, chg.Err()))
		var aliases []*backend.Alias
		var rmAliases []*backend.Alias
		beAlias := &backend.Alias{Name: scenAlias, Target: fmt.Sprintf("alias-snap.%s", cmds[scenAlias])}
		switch scenario.mutation {
		case "-":
		case "add":
			aliases = []*backend.Alias{beAlias}
		case "rm":
			rmAliases = []*backend.Alias{beAlias}
		}

		comm := Commentf("%#v", scenario)
		expected := fakeOps{
			{
				op:        "update-aliases",
				aliases:   aliases,
				rmAliases: rmAliases,
			},
			{
				op:      "matching-aliases",
				aliases: aliases,
			},
			{
				op:      "missing-aliases",
				aliases: rmAliases,
			},
			{
				op:        "update-aliases",
				aliases:   rmAliases,
				rmAliases: aliases,
			},
		}
		// start with an easier-to-read error if this fails:
		c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops(), comm)
		c.Check(s.fakeBackend.ops, DeepEquals, expected, comm)

		var allAliases map[string]map[string]string
		err = s.state.Get("aliases", &allAliases)
		c.Assert(err, IsNil)
		if scenario.beforeStatus != "" {
			c.Check(allAliases, DeepEquals, map[string]map[string]string{
				"alias-snap": {scenAlias: scenario.beforeStatus},
			}, comm)
		} else {
			c.Check(allAliases, HasLen, 0, comm)
		}

		s.fakeBackend.ops = nil
	}
}

func (s *snapmgrTestSuite) TestDisabledSnapResetAliasesRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  false,
	})

	// alias1 is a non auto-alias
	// alias5 is an auto-alias
	// alias1gone is a non auto-alias and doesn't have an entry in the current snap revision anymore
	// alias5gone is an auto-alias and doesn't have an entry in the current snap revision anymore
	snapstate.AutoAliases = func(st *state.State, info *snap.Info) ([]string, error) {
		c.Check(info.Name(), Equals, "alias-snap")
		return []string{"alias5", "alias5gone"}, nil
	}

	defer s.snapmgr.Stop()
	for _, scenario := range statusesMatrix {
		if scenario.action != "reset" {
			// we reuse the scenarios but here want to test only reset i.e. ResetAliases for the disabled snap case (the other actions are still unsupported for disabled snaps)
			continue
		}

		scenAlias := scenario.alias
		if scenario.beforeStatus != "" {
			s.state.Set("aliases", map[string]map[string]string{
				"alias-snap": {
					scenAlias: scenario.beforeStatus,
				},
			})
		} else {
			s.state.Set("aliases", nil)
		}

		chg := s.state.NewChange("scenario", "...")
		var err error
		var ts *state.TaskSet
		targets := []string{scenAlias}
		ts, err = snapstate.ResetAliases(s.state, "alias-snap", targets)
		c.Assert(err, IsNil)

		chg.AddAll(ts)

		s.state.Unlock()
		s.settle()
		s.state.Lock()

		c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("%#v: %v", scenario, chg.Err()))

		comm := Commentf("%#v", scenario)
		// no mutation
		c.Check(s.fakeBackend.ops, HasLen, 0, comm)

		var allAliases map[string]map[string]string
		err = s.state.Get("aliases", &allAliases)
		c.Assert(err, IsNil)
		if scenario.status != "" {
			c.Check(allAliases, DeepEquals, map[string]map[string]string{
				"alias-snap": {scenAlias: scenario.status},
			}, comm)
		} else {
			c.Check(allAliases, HasLen, 0, comm)
		}

		s.fakeBackend.ops = nil
	}
}

func (s *snapmgrTestSuite) TestDisabledSnapResetAliasesTotalUndoRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  false,
	})

	// alias1 is a non auto-alias
	// alias5 is an auto-alias
	// alias1gone is a non auto-alias and doesn't have an entry in the snap anymore
	// alias5gone is an auto-alias and doesn't have an entry in the snap any
	snapstate.AutoAliases = func(st *state.State, info *snap.Info) ([]string, error) {
		c.Check(info.Name(), Equals, "alias-snap")
		return []string{"alias5", "alias5gone"}, nil
	}

	defer s.snapmgr.Stop()
	for _, scenario := range statusesMatrix {
		if scenario.action != "reset" {
			// we reuse the scenarios but here want to test only reset i.e. ResetAliases for the disabled snap case (the other actions are still unsupported for disabled snaps)
			continue
		}

		scenAlias := scenario.alias
		if scenario.beforeStatus != "" {
			s.state.Set("aliases", map[string]map[string]string{
				"alias-snap": {
					scenAlias: scenario.beforeStatus,
				},
			})
		} else {
			s.state.Set("aliases", nil)
		}

		chg := s.state.NewChange("scenario", "...")
		var err error
		var ts *state.TaskSet
		targets := []string{scenAlias}

		ts, err = snapstate.ResetAliases(s.state, "alias-snap", targets)
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

		c.Assert(chg.Status(), Equals, state.ErrorStatus, Commentf("%#v: %v", scenario, chg.Err()))

		comm := Commentf("%#v", scenario)
		// no mutation
		c.Check(s.fakeBackend.ops, HasLen, 0, comm)

		var allAliases map[string]map[string]string
		err = s.state.Get("aliases", &allAliases)
		c.Assert(err, IsNil)
		if scenario.beforeStatus != "" {
			c.Check(allAliases, DeepEquals, map[string]map[string]string{
				"alias-snap": {scenAlias: scenario.beforeStatus},
			}, comm)
		} else {
			c.Check(allAliases, HasLen, 0, comm)
		}

		s.fakeBackend.ops = nil
	}
}

func (s *snapmgrTestSuite) TestUnliasTotalUndoRunThroughAliasConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	defer s.snapmgr.Stop()
	s.state.Set("aliases", map[string]map[string]string{
		"alias-snap": {
			"alias1": "enabled",
		},
	})

	chg := s.state.NewChange("scenario", "...")
	ts, err := snapstate.Unalias(s.state, "alias-snap", []string{"alias1"})
	c.Assert(err, IsNil)

	chg.AddAll(ts)

	tasks := ts.Tasks()
	last := tasks[len(tasks)-1]

	grabAlias1 := func(t *state.Task, _ *tomb.Tomb) error {
		st := t.State()
		st.Lock()
		defer st.Unlock()

		var allAliases map[string]map[string]string
		err := st.Get("aliases", &allAliases)
		c.Assert(err, IsNil)
		c.Assert(allAliases, DeepEquals, map[string]map[string]string{
			"alias-snap": {
				"alias1": "disabled",
			},
		})

		st.Set("aliases", map[string]map[string]string{
			"alias-snap": {
				"alias1": "disabled",
			},
			"other-snap": {
				"alias1": "enabled",
			},
		})
		return nil
	}

	s.snapmgr.AddAdhocTaskHandler("grab-alias1", grabAlias1, nil)

	tgrab1 := s.state.NewTask("grab-alias1", "grab alias1 for other-snap")
	tgrab1.WaitFor(last)
	chg.AddTask(tgrab1)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(tgrab1)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 5; i++ {
		s.snapmgr.Ensure()
		s.snapmgr.Wait()
	}

	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.ErrorStatus, Commentf("%v", chg.Err()))
	rmAliases := []*backend.Alias{{"alias1", "alias-snap.cmd1"}}

	expected := fakeOps{
		{
			op:        "update-aliases",
			rmAliases: rmAliases,
		},
		{
			op: "matching-aliases",
		},
		{
			op: "missing-aliases",
		},
		{
			op: "update-aliases",
		},
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	var allAliases map[string]map[string]string
	err = s.state.Get("aliases", &allAliases)
	c.Assert(err, IsNil)
	c.Check(allAliases, DeepEquals, map[string]map[string]string{
		"other-snap": {
			"alias1": "enabled",
		},
	})

	c.Check(last.Log(), HasLen, 1)
	c.Check(last.Log()[0], Matches, `.* ERROR cannot enable alias "alias1" for "alias-snap", already enabled for "other-snap"`)

}

func (s *snapmgrTestSuite) TestAutoAliasesDelta(c *C) {
	snapstate.AutoAliases = func(st *state.State, info *snap.Info) ([]string, error) {
		c.Check(info.Name(), Equals, "alias-snap")
		return []string{"alias1", "alias2", "alias4", "alias5"}, nil
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

	s.state.Set("aliases", map[string]map[string]string{
		"alias-snap": {
			"alias1": "enabled",
			"alias2": "disabled",
			"alias3": "auto",
		},
	})

	new, retired, err := snapstate.AutoAliasesDelta(s.state, []string{"alias-snap"})
	c.Assert(err, IsNil)

	c.Check(new, DeepEquals, map[string][]string{
		"alias-snap": {"alias4", "alias5"},
	})

	c.Check(retired, DeepEquals, map[string][]string{
		"alias-snap": {"alias3"},
	})
}

func (s *snapmgrTestSuite) TestAutoAliasesDeltaAll(c *C) {
	seen := make(map[string]bool)
	snapstate.AutoAliases = func(st *state.State, info *snap.Info) ([]string, error) {
		seen[info.Name()] = true
		if info.Name() == "alias-snap" {
			return []string{"alias1", "alias2", "alias4", "alias5"}, nil
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

	new, retired, err := snapstate.AutoAliasesDelta(s.state, nil)
	c.Assert(err, IsNil)

	c.Check(new, DeepEquals, map[string][]string{
		"alias-snap": {"alias1", "alias2", "alias4", "alias5"},
	})

	c.Check(retired, HasLen, 0)

	c.Check(seen, DeepEquals, map[string]bool{
		"alias-snap": true,
		"other-snap": true,
	})
}

func (s *snapmgrTestSuite) TestDoSetAutoAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) ([]string, error) {
		c.Check(info.Name(), Equals, "alias-snap")
		return []string{"alias1", "alias2", "alias4", "alias5"}, nil
	}

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
			"alias2": "auto",
			"alias3": "auto",
			"alias5": "disabled",
		},
	})

	t := s.state.NewTask("set-auto-aliases", "test")
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
		"alias-snap": {
			"alias1": "enabled",
			"alias2": "auto",
			"alias4": "auto",
			"alias5": "disabled",
		},
	})
}

func (s *snapmgrTestSuite) TestDoUndoSetAutoAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) ([]string, error) {
		c.Check(info.Name(), Equals, "alias-snap")
		return []string{"alias1", "alias2", "alias4", "alias5"}, nil
	}

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
			"alias2": "auto",
			"alias3": "auto",
			"alias5": "disabled",
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
		s.snapmgr.Ensure()
		s.snapmgr.Wait()
	}

	s.state.Lock()

	c.Check(t.Status(), Equals, state.UndoneStatus, Commentf("%v", chg.Err()))

	var allAliases map[string]map[string]string
	err := s.state.Get("aliases", &allAliases)
	c.Assert(err, IsNil)
	c.Check(allAliases, DeepEquals, map[string]map[string]string{
		"alias-snap": {
			"alias1": "enabled",
			"alias2": "auto",
			"alias3": "auto",
			"alias5": "disabled",
		},
	})
}

func (s *snapmgrTestSuite) TestDoSetAutoAliasesConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) ([]string, error) {
		c.Check(info.Name(), Equals, "alias-snap")
		return []string{"alias1", "alias2", "alias4", "alias5"}, nil
	}

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
			"alias3": "auto",
			"alias5": "disabled",
		},
		"other-snap": {
			"alias4": "enabled",
		},
	})

	t := s.state.NewTask("set-auto-aliases", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "alias-snap"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.snapmgr.Ensure()
	s.snapmgr.Wait()

	s.state.Lock()

	c.Check(t.Status(), Equals, state.ErrorStatus, Commentf("%v", chg.Err()))
	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot enable alias "alias4" for "alias-snap", already enabled for "other-snap".*`)
}

func (s *snapmgrTestSuite) TestAliases(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// nothing
	aliases, err := snapstate.Aliases(s.state)
	c.Assert(err, IsNil)
	c.Check(aliases, HasLen, 0)

	// snaps with aliases
	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11)},
		},
		Current: snap.R(11),
		Active:  true,
	})

	snapstate.Set(s.state, "alias-snap2", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap2", Revision: snap.R(12)},
		},
		Current: snap.R(12),
		Active:  true,
	})

	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", Revision: snap.R(2)},
		},
		Current: snap.R(2),
		Active:  true,
	})

	s.state.Set("aliases", map[string]map[string]string{
		"alias-snap": {
			"alias1": "enabled",
			"alias5": "auto",
			"alias3": "disabled",
		},
		"alias-snap2": {
			"alias2": "enabled",
		},
	})

	aliases, err = snapstate.Aliases(s.state)
	c.Assert(err, IsNil)
	c.Check(aliases, DeepEquals, map[string]map[string]string{
		"alias-snap": {
			"alias1": "enabled",
			"alias5": "auto",
			"alias3": "disabled",
		},
		"alias-snap2": {
			"alias2": "enabled",
		},
	})
}
