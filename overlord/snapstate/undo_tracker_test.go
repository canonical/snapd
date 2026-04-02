// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

type undoTrackerSuite struct {
	st *state.State
	t  *state.Task
	ut *snapstate.UndoTracker
}

var _ = Suite(&undoTrackerSuite{})

func (s *undoTrackerSuite) SetUpTest(c *C) {
	s.st = state.New(nil)
	s.st.Lock()
	defer s.st.Unlock()
	chg := s.st.NewChange("test-change", "test change")
	s.t = s.st.NewTask("test-task", "test task")
	chg.AddTask(s.t)

	s.ut = snapstate.NewUndoTracker(s.t)
}

func (s *undoTrackerSuite) TestNewUndoTrackerPanicsOnNilTask(c *C) {
	c.Check(func() { snapstate.NewUndoTracker(nil) }, Panics, "internal error: task cannot be nil")
}

func (s *undoTrackerSuite) TestRunOnErrorPanicsOnNilErrorPointer(c *C) {
	s.st.Lock()
	defer s.st.Unlock()
	c.Check(func() { s.ut.RunOnError(nil) }, Panics, "internal error: retErr pointer cannot be nil")
}

func (s *undoTrackerSuite) TestRunOnErrorDoesNothingOnNoError(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	called := false
	s.ut.Add(func() error {
		called = true
		return nil
	})

	var retErr error
	s.ut.RunOnError(&retErr)

	// undo not called
	c.Check(called, Equals, false)
}

func (s *undoTrackerSuite) TestRunOnErrorDoesNothingOnWaitError(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	called := false
	s.ut.Add(func() error {
		called = true
		return nil
	})

	var retErr error = &state.Wait{Reason: "waiting for reboot"}
	s.ut.RunOnError(&retErr)

	// undo not called
	c.Check(called, Equals, false)
}

func (s *undoTrackerSuite) TestRunOnErrorNoUndoesRegistered(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// no undoes registered, should not panic
	retErr := errors.New("task failed")
	s.ut.RunOnError(&retErr)
}

func (s *undoTrackerSuite) TestRunOnErrorRunsUndoesInReverseOrder(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	var order []int
	for i := 1; i <= 3; i++ {
		i := i // capture loop variable
		s.ut.Add(func() error {
			order = append(order, i)
			return nil
		})
	}

	retErr := errors.New("task failed")
	s.ut.RunOnError(&retErr)

	// undoes called in reverse order
	c.Check(order, DeepEquals, []int{3, 2, 1})
}

func (s *undoTrackerSuite) TestRunOnErrorContinuesAfterUndoFailure(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	var order []int
	for i := 1; i <= 3; i++ {
		i := i // capture loop variable
		s.ut.Add(func() error {
			order = append(order, i)
			if i == 2 {
				return errors.New("undo 2 failed")
			}
			return nil
		})
	}

	retErr := errors.New("task failed")
	s.ut.RunOnError(&retErr)

	// all three ran despite undo 2 failing
	c.Check(order, DeepEquals, []int{3, 2, 1})

	// undo error is logged to the task
	c.Check(s.t.Log(), HasLen, 1)
	c.Check(s.t.Log()[0], Matches, `.*cannot undo: undo 2 failed`)
}

func (s *undoTrackerSuite) TestAddUnlockedReleasesStateLock(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	called := false
	s.ut.AddUnlocked(func() error {
		// try to lock the state. If AddUnlocked correctly
		// released the lock, this will succeed, otherwise
		// it will deadlock and the test will fail
		s.st.Lock()
		defer s.st.Unlock()
		called = true
		return nil
	})

	retErr := errors.New("task failed")
	s.ut.RunOnError(&retErr)

	// undo was successfully called
	c.Check(called, Equals, true)
}

func (s *undoTrackerSuite) TestAddUnlockedMixedWithAdd(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	var order []int

	s.ut.Add(func() error {
		order = append(order, 1)
		return nil
	})

	s.ut.AddUnlocked(func() error {
		// try to lock the state. If AddUnlocked correctly
		// released the lock, this will succeed, otherwise
		// it will deadlock and the test will fail
		s.st.Lock()
		defer s.st.Unlock()
		order = append(order, 2)
		return nil
	})

	s.ut.Add(func() error {
		order = append(order, 3)
		return nil
	})

	retErr := errors.New("task failed")
	s.ut.RunOnError(&retErr)

	// undoes called in reverse order
	c.Check(order, DeepEquals, []int{3, 2, 1})
}
