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
	st     *state.State
	t      *state.Task
	ut     *snapstate.UndoTracker
	retErr error
	run    func()
}

var _ = Suite(&undoTrackerSuite{})

func (s *undoTrackerSuite) SetUpTest(c *C) {
	s.st = state.New(nil)
	s.st.Lock()
	defer s.st.Unlock()
	chg := s.st.NewChange("test-change", "test change")
	s.t = s.st.NewTask("test-task", "test task")
	chg.AddTask(s.t)
	s.retErr = nil
	s.ut, s.run = snapstate.NewUndoTracker(s.t, &s.retErr)
}

func (s *undoTrackerSuite) TestRunDoesNothingOnNoError(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	called := false
	s.ut.Locked().AddUndo(func() error {
		called = true
		return nil
	})

	s.run()

	// undo not called
	c.Check(called, Equals, false)
}

func (s *undoTrackerSuite) TestRunDoesNothingOnWaitError(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	called := false
	s.ut.Locked().AddUndo(func() error {
		called = true
		return nil
	})

	s.retErr = &state.Wait{Reason: "waiting for reboot"}
	s.run()

	// undo not called
	c.Check(called, Equals, false)
}

func (s *undoTrackerSuite) TestRunDoesNothingOnRetryError(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	called := false
	s.ut.Locked().AddUndo(func() error {
		called = true
		return nil
	})

	s.retErr = &state.Retry{Reason: "retrying task"}
	s.run()

	// undo not called
	c.Check(called, Equals, false)
}

func (s *undoTrackerSuite) TestRunWithNoUndosRegisteredDoesNotPanic(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// no undos registered, should not panic
	s.retErr = errors.New("task failed")
	s.run()
}

func (s *undoTrackerSuite) TestRunExecutesUndosInReverseOrder(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	var order []int
	for i := 1; i <= 3; i++ {
		i := i // capture loop variable
		s.ut.Locked().AddUndo(func() error {
			order = append(order, i)
			return nil
		})
	}

	s.retErr = errors.New("task failed")
	s.run()

	// undos called in reverse order
	c.Check(order, DeepEquals, []int{3, 2, 1})
}

func (s *undoTrackerSuite) TestRunContinuesAfterUndoFailure(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	var order []int
	for i := 1; i <= 3; i++ {
		i := i // capture loop variable
		s.ut.Locked().AddUndo(func() error {
			order = append(order, i)
			if i == 2 {
				return errors.New("undo 2 failed")
			}
			return nil
		})
	}

	s.retErr = errors.New("task failed")
	s.run()

	// all three ran despite undo 2 failing
	c.Check(order, DeepEquals, []int{3, 2, 1})

	// undo error is logged to the task
	c.Check(s.t.Log(), HasLen, 1)
	c.Check(s.t.Log()[0], Matches, `.*cannot undo: undo 2 failed`)
}

func (s *undoTrackerSuite) TestUnlockedUndoRunsWithStateUnlocked(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	called := false
	s.ut.Unlocked().AddUndo(func() error {
		// try to lock the state. If Unlocked() correctly tags this
		// undo for unlocked execution, run will release the
		// lock before calling it, and this will succeed. Otherwise
		// it will deadlock and the test will fail
		s.st.Lock()
		defer s.st.Unlock()
		called = true
		return nil
	})

	s.retErr = errors.New("task failed")
	s.run()

	// undo was successfully called
	c.Check(called, Equals, true)
}

func (s *undoTrackerSuite) TestUnlockedMixedWithLockedUndos(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	var order []int

	s.ut.Locked().AddUndo(func() error {
		order = append(order, 1)
		return nil
	})

	s.ut.Unlocked().AddUndo(func() error {
		// try to lock the state. If Unlocked() correctly tags this
		// undo for unlocked execution, run will release the
		// lock before calling it, and this will succeed. Otherwise
		// it will deadlock and the test will fail
		s.st.Lock()
		defer s.st.Unlock()
		order = append(order, 2)
		return nil
	})

	s.ut.Locked().AddUndo(func() error {
		order = append(order, 3)
		return nil
	})

	s.retErr = errors.New("task failed")
	s.run()

	// undos called in reverse order
	c.Check(order, DeepEquals, []int{3, 2, 1})
}

func (s *undoTrackerSuite) TestRunCollectsAndLogsAllErrors(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.ut.Locked().AddUndo(func() error {
		return errors.New("locked undo failed")
	})

	s.ut.Unlocked().AddUndo(func() error {
		return errors.New("unlocked undo failed")
	})

	s.retErr = errors.New("task failed")
	s.run()

	// both errors logged (in reverse registration order)
	c.Check(s.t.Log(), HasLen, 2)
	c.Check(s.t.Log()[0], Matches, `.*cannot undo: unlocked undo failed`)
	c.Check(s.t.Log()[1], Matches, `.*cannot undo: locked undo failed`)
}

func (s *undoTrackerSuite) TestRunPanicsOnSecondCall(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.run()

	c.Assert(s.run, PanicMatches, "internal error: cannot call UndoTracker.run more than once")
}

func (s *undoTrackerSuite) TestAddUndoPanicsAfterRunAlreadyCalled(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.run()

	c.Assert(func() {
		s.ut.Locked().AddUndo(func() error { return nil })
	}, PanicMatches, "internal error: cannot register undo after undos execution has started")
}

func (s *undoTrackerSuite) TestUnlockedAddUndoPanicsAfterRunAlreadyCalled(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.run()

	c.Assert(func() {
		s.ut.Unlocked().AddUndo(func() error { return nil })
	}, PanicMatches, "internal error: cannot register undo after undos execution has started")
}

func (s *undoTrackerSuite) TestNullUndoerAddUndoDoesNothing(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	called := false
	snapstate.NullUndoer.AddUndo(func() error {
		called = true
		return nil
	})

	s.retErr = errors.New("task failed")
	s.run()

	// undo not called
	c.Check(called, Equals, false)
}

func (s *undoTrackerSuite) TestTODOUndoerAddUndoDoesNothing(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	called := false
	snapstate.TODOUndoer.AddUndo(func() error {
		called = true
		return nil
	})

	s.retErr = errors.New("task failed")
	s.run()

	// undo not called
	c.Check(called, Equals, false)
}
