// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2024 Canonical Ltd
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

package fdestate_test

import (
	"errors"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

func (s *fdeMgrSuite) TestCheckChangeConflict(c *C) {
	nop := func(t *state.Task, _ *tomb.Tomb) error { return nil }

	s.o.TaskRunner().AddHandler("with-keyslots", nop, nil)
	s.o.TaskRunner().AddHandler("without-keyslots", nop, nil)

	someKeyslotRef := fdestate.KeyslotRef{ContainerRole: "some-role", Name: "some-slot"}
	someOtherKeyslotRef := fdestate.KeyslotRef{ContainerRole: "some-role", Name: "some-other-slot"}

	s.st.Lock()
	defer s.st.Unlock()

	chg := s.st.NewChange("some-change", "")

	withKeyslots := s.st.NewTask("with-keyslots", "")
	withKeyslots.Set("keyslots", []fdestate.KeyslotRef{someKeyslotRef})
	chg.AddTask(withKeyslots)

	withoutKeyslots := s.st.NewTask("without-keyslots", "")
	chg.AddTask(withoutKeyslots)

	err := fdestate.CheckChangeConflict(s.st, []fdestate.KeyslotRef{someKeyslotRef})
	c.Assert(err, ErrorMatches, `key slot \(container-role: "some-role", name: "some-slot"\) has "some-change" change in progress`)
	var conflictErr *fdestate.ChangeConflictError
	c.Assert(errors.As(err, &conflictErr), Equals, true)
	c.Check(conflictErr.ChangeKind, Equals, "some-change")
	c.Check(conflictErr.KeyslotRef, Equals, fdestate.KeyslotRef{Name: "some-slot", ContainerRole: "some-role"})

	err = fdestate.CheckChangeConflict(s.st, []fdestate.KeyslotRef{someOtherKeyslotRef})
	c.Assert(err, IsNil)

	s.settle(c)

	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(withKeyslots.Status(), Equals, state.DoneStatus)

	// no ready tasks with "keyslots" exist
	err = fdestate.CheckChangeConflict(s.st, []fdestate.KeyslotRef{someKeyslotRef})
	c.Assert(err, IsNil)
}

func (s *fdeMgrSuite) TestChangeConflictErrorMessage(c *C) {
	someKeyslotRef := fdestate.KeyslotRef{ContainerRole: "some-role", Name: "some-slot"}

	err := fdestate.ChangeConflictError{KeyslotRef: someKeyslotRef}
	c.Check(err.Error(), Equals, `key slot (container-role: "some-role", name: "some-slot") has changes in progress`)

	err = fdestate.ChangeConflictError{KeyslotRef: someKeyslotRef, ChangeKind: "some-change"}
	c.Check(err.Error(), Equals, `key slot (container-role: "some-role", name: "some-slot") has "some-change" change in progress`)

	err = fdestate.ChangeConflictError{Message: "error message", KeyslotRef: someKeyslotRef, ChangeKind: "some-change"}
	c.Check(err.Error(), Equals, "error message")
}

func (s *fdeMgrSuite) TestChangeConflictErrorIs(c *C) {
	this := &fdestate.ChangeConflictError{
		KeyslotRef: fdestate.KeyslotRef{Name: "a", ContainerRole: "a"},
		ChangeKind: "a",
		Message:    "a",
	}
	that := &fdestate.ChangeConflictError{
		KeyslotRef: fdestate.KeyslotRef{Name: "b", ContainerRole: "b"},
		ChangeKind: "b",
		Message:    "b",
	}
	c.Check(this, testutil.ErrorIs, that)
}
