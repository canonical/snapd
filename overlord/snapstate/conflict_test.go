// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type conflictSuite struct{}

var _ = Suite(&conflictSuite{})

func (s *conflictSuite) TestChangeConflictErrorIs(c *C) {
	this := &snapstate.ChangeConflictError{
		Snap:       "a",
		ChangeKind: "a",
		Message:    "a",
		ChangeID:   "a",
	}
	that := &snapstate.ChangeConflictError{
		Snap:       "b",
		ChangeKind: "b",
		Message:    "b",
		ChangeID:   "b",
	}
	c.Check(this, testutil.ErrorIs, that)
}

func (s *conflictSuite) TestSnapsAffectedByTaskKind(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("test-task", "task")
	// no affected snaps
	snaps, err := snapstate.SnapsAffectedByTask(t)
	c.Assert(err, IsNil)
	c.Check(snaps, HasLen, 0)

	// register kind callback
	restore := snapstate.MockAffectedSnapsByKind(map[string]snapstate.AffectedSnapsFunc{
		"test-task": func(t *state.Task) ([]string, error) {
			return []string{"some-snap"}, nil
		},
	})
	defer restore()
	// affected snaps calculated by callback
	snaps, err = snapstate.SnapsAffectedByTask(t)
	c.Assert(err, IsNil)
	c.Check(snaps, DeepEquals, []string{"some-snap"})
}

func (s *conflictSuite) TestSnapsAffectedByTaskAttr(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("test-task", "task")
	t.Set("test-attr", true)
	// no affected snaps
	snaps, err := snapstate.SnapsAffectedByTask(t)
	c.Assert(err, IsNil)
	c.Check(snaps, HasLen, 0)

	// register kind callback
	restore := snapstate.MockAffectedSnapsByAttr(map[string]snapstate.AffectedSnapsFunc{
		"test-attr": func(t *state.Task) ([]string, error) {
			return []string{"some-snap"}, nil
		},
	})
	defer restore()
	// affected snaps calculated by callback
	snaps, err = snapstate.SnapsAffectedByTask(t)
	c.Assert(err, IsNil)
	c.Check(snaps, DeepEquals, []string{"some-snap"})
}

func (s *conflictSuite) TestSnapsAffectedByTaskSnapSetup(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("test-task", "task")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "some-snap",
		},
	})
	// affected snap based on snap setup
	snaps, err := snapstate.SnapsAffectedByTask(t)
	c.Assert(err, IsNil)
	c.Check(snaps, DeepEquals, []string{"some-snap"})
}
