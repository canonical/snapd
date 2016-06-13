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

package hookstate_test

import (
	"regexp"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func TestHookManager(t *testing.T) { TestingT(t) }

type hookManagerSuite struct {
	state       *state.State
	manager     *hookstate.HookManager
	mockHandler *mockHandler
}

var _ = Suite(&hookManagerSuite{})

func (s *hookManagerSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.state = state.New(nil)
	manager, err := hookstate.Manager(s.state)
	c.Assert(err, IsNil)
	s.manager = manager
}

func (s *hookManagerSuite) TearDownTest(c *C) {
	s.manager.Stop()
	dirs.SetRootDir("")
}

func (s *hookManagerSuite) TestSmoke(c *C) {
	s.manager.Ensure()
	s.manager.Wait()
}

func (s *hookManagerSuite) TestHookTask(c *C) {
	s.state.Lock()
	task := hookstate.HookTask(s.state, "test summary", "test-snap", snap.R(1), "test-hook")
	c.Assert(task, NotNil, Commentf("Expected HookTask to return a task"))

	change := s.state.NewChange("kind", "summary")
	change.AddTask(task)
	s.state.Unlock()

	// Register a handler generator for the "test-hook" hook
	var calledContext *hookstate.Context
	mockHandler := newMockHandler()
	mockHandlerGenerator := func(context *hookstate.Context) hookstate.Handler {
		calledContext = context
		return mockHandler
	}

	s.manager.Register(regexp.MustCompile("test-hook"), mockHandlerGenerator)

	s.manager.Ensure()
	s.manager.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(calledContext, NotNil, Commentf("Expected handler generator to be called with a valid context"))
	c.Check(calledContext.SnapName(), Equals, "test-snap")
	c.Check(calledContext.SnapRevision(), Equals, snap.R(1))
	c.Check(calledContext.HookName(), Equals, "test-hook")

	c.Check(mockHandler.beforeCalled, Equals, true)
	c.Check(mockHandler.doneCalled, Equals, true)
	c.Check(mockHandler.errorCalled, Equals, false)

	c.Check(task.Kind(), Equals, "run-hook")
	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(change.Status(), Equals, state.DoneStatus)
}

type mockHandler struct {
	beforeCalled bool
	doneCalled   bool
	errorCalled  bool
	err          error
}

func newMockHandler() *mockHandler {
	return &mockHandler{
		beforeCalled: false,
		doneCalled:   false,
		errorCalled:  false,
		err:          nil,
	}
}

func (h *mockHandler) Before() error {
	h.beforeCalled = true
	return nil
}

func (h *mockHandler) Done() error {
	h.doneCalled = true
	return nil
}

func (h *mockHandler) Error(err error) error {
	h.err = err
	h.errorCalled = true
	return nil
}
