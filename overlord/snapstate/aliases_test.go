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

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
)

func (s *snapmgrTestSuite) TestUpdateUnaliasChangeConflict(c *C) {
	c.Skip("new semantics are wip")
	/*
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
	*/
}

func (s *snapmgrTestSuite) TestUnliasTotalUndoRunThroughAliasConflict(c *C) {
	c.Skip("new semantics are wip")
	/*

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
	*/
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
