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
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

func (s *fdeMgrSuite) TestCheckFDEChangeConflict(c *C) {
	var chgToErr = map[string]string{
		"fde-efi-secureboot-db-update": "external EFI DBX update in progress, no other FDE changes allowed until this is done",
		"fde-replace-recovery-key":     "replacing recovery key in progress, no other FDE changes allowed until this is done",
		"fde-change-passphrase":        "changing passphrase in progress, no other FDE changes allowed until this is done",
		"some-fde-change":              "FDE change in progress, no other FDE changes allowed until this is done",

		"some-change": "",
	}

	s.runner.AddHandler("fde-op", func(_ *state.Task, _ *tomb.Tomb) error { return nil }, nil)
	s.runner.AddHandler("nop", func(_ *state.Task, _ *tomb.Tomb) error { return nil }, nil)

	for chgKind, expectedErr := range chgToErr {
		s.st.Lock()

		chg := s.st.NewChange(chgKind, "")
		var tsk *state.Task
		if chgKind == "some-fde-change" {
			tsk = s.st.NewTask("fde-op", "")
		} else {
			tsk = s.st.NewTask("nop", "")
		}
		chg.AddTask(tsk)

		if expectedErr != "" {
			err := fdestate.CheckFDEChangeConflict(s.st)
			c.Assert(err, ErrorMatches, expectedErr)
			var conflictErr *snapstate.ChangeConflictError
			c.Assert(errors.As(err, &conflictErr), Equals, true)
			c.Check(conflictErr.ChangeKind, Equals, chgKind)
			c.Check(conflictErr.ChangeID, Equals, chg.ID())
		} else {
			err := fdestate.CheckFDEChangeConflict(s.st)
			c.Assert(err, IsNil)
		}

		s.settle(c)

		c.Check(chg.Status(), Equals, state.DoneStatus)
		c.Check(tsk.Status(), Equals, state.DoneStatus)

		// no ready changes exist
		err := fdestate.CheckFDEChangeConflict(s.st)
		c.Assert(err, IsNil)

		s.st.Unlock()
	}

}

func (s *fdeMgrSuite) TestAddProtectedKeysAffectedSnaps(c *C) {
	st := s.st
	onClassic := true
	s.startedManager(c, onClassic)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	st.Lock()
	defer st.Unlock()

	tsk := st.NewTask("foo", "foo task")

	names, err := fdestate.AddProtectedKeysAffectedSnaps(tsk)
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{
		"pc",        // gadget
		"pc-kernel", // kernel
		"core20",    // base
	})
}
