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
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

func TestHookManager(t *testing.T) { TestingT(t) }

type hookManagerSuite struct {
	state       *state.State
	manager     *hookstate.HookManager
	mockHandler *hooktest.MockHandler
	task        *state.Task
	change      *state.Change
	command     *testutil.MockCmd
}

var _ = Suite(&hookManagerSuite{})

func (s *hookManagerSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.state = state.New(nil)
	manager, err := hookstate.Manager(s.state)
	c.Assert(err, IsNil)
	s.manager = manager

	s.state.Lock()
	s.task = hookstate.HookTask(s.state, "test summary", "test-snap", snap.R(1), "test-hook")
	c.Assert(s.task, NotNil, Commentf("Expected HookTask to return a task"))

	s.change = s.state.NewChange("kind", "summary")
	s.change.AddTask(s.task)
	s.state.Unlock()

	s.command = testutil.MockCommand(c, "snap", "")
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
	// Register a handler generator for the "test-hook" hook
	var calledContext *hookstate.Context
	mockHandler := hooktest.NewMockHandler()
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

	c.Check(s.command.Calls(), DeepEquals, [][]string{[]string{
		"snap", "run", "test-snap", "--hook", "test-hook", "-r", "1",
	}})

	c.Check(mockHandler.BeforeCalled, Equals, true)
	c.Check(mockHandler.DoneCalled, Equals, true)
	c.Check(mockHandler.ErrorCalled, Equals, false)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.DoneStatus)
	c.Check(s.change.Status(), Equals, state.DoneStatus)
}

func (s *hookManagerSuite) TestHookTaskHandlesHookError(c *C) {
	// Register a handler generator for the "test-hook" hook
	mockHandler := hooktest.NewMockHandler()
	mockHandlerGenerator := func(context *hookstate.Context) hookstate.Handler {
		return mockHandler
	}

	// Force the snap command to exit 1, and print something to stderr
	s.command = testutil.MockCommand(
		c, "snap", ">&2 echo 'hook failed at user request'; exit 1")

	s.manager.Register(regexp.MustCompile("test-hook"), mockHandlerGenerator)

	s.manager.Ensure()
	s.manager.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(mockHandler.BeforeCalled, Equals, true)
	c.Check(mockHandler.DoneCalled, Equals, false)
	c.Check(mockHandler.ErrorCalled, Equals, true)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, regexp.MustCompile(".*failed at user request.*"))
}

func (s *hookManagerSuite) TestHookTaskCanKillHook(c *C) {
	// Register a handler generator for the "test-hook" hook
	mockHandler := hooktest.NewMockHandler()
	mockHandlerGenerator := func(context *hookstate.Context) hookstate.Handler {
		return mockHandler
	}

	// Force the snap command to hang
	s.command = testutil.MockCommand(c, "snap", "while true; do sleep 1; done")

	s.manager.Register(regexp.MustCompile("test-hook"), mockHandlerGenerator)

	s.manager.Ensure()
	completed := make(chan struct{})
	go func() {
		s.manager.Wait()
		close(completed)
	}()

	// Abort the change, which should kill the hanging hook, and wait for the
	// task to complete.
	s.state.Lock()
	s.change.Abort()
	s.state.Unlock()
	s.manager.Ensure()
	<-completed

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(mockHandler.BeforeCalled, Equals, true)
	c.Check(mockHandler.DoneCalled, Equals, false)
	c.Check(mockHandler.ErrorCalled, Equals, true)
	c.Check(mockHandler.Err, ErrorMatches, ".*hook \"test-hook\" aborted.*")

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, regexp.MustCompile(".*hook \"test-hook\" aborted.*"))
}

func (s *hookManagerSuite) TestHookTaskCorrectlyIncludesContext(c *C) {
	// Register a handler generator for the "test-hook" hook
	mockHandler := hooktest.NewMockHandler()
	mockHandlerGenerator := func(context *hookstate.Context) hookstate.Handler {
		return mockHandler
	}

	// Force the snap command to exit with a failure and print to stderr so we
	// can catch and verify it.
	s.command = testutil.MockCommand(
		c, "snap", ">&2 echo \"SNAP_CONTEXT=$SNAP_CONTEXT\"; exit 1")

	s.manager.Register(regexp.MustCompile("test-hook"), mockHandlerGenerator)

	s.manager.Ensure()
	s.manager.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(mockHandler.BeforeCalled, Equals, true)
	c.Check(mockHandler.DoneCalled, Equals, false)
	c.Check(mockHandler.ErrorCalled, Equals, true)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, regexp.MustCompile(".*SNAP_CONTEXT=\\S+"))
}

func (s *hookManagerSuite) TestHookTaskHandlerBeforeError(c *C) {
	// Register a handler generator for the "test-hook" hook
	mockHandler := hooktest.NewMockHandler()
	mockHandler.BeforeError = true
	mockHandlerGenerator := func(context *hookstate.Context) hookstate.Handler {
		return mockHandler
	}

	s.manager.Register(regexp.MustCompile("test-hook"), mockHandlerGenerator)

	s.manager.Ensure()
	s.manager.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(mockHandler.BeforeCalled, Equals, true)
	c.Check(mockHandler.DoneCalled, Equals, false)
	c.Check(mockHandler.ErrorCalled, Equals, false)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, regexp.MustCompile(".*before failed at user request.*"))
}

func (s *hookManagerSuite) TestHookTaskHandlerDoneError(c *C) {
	// Register a handler generator for the "test-hook" hook
	mockHandler := hooktest.NewMockHandler()
	mockHandler.DoneError = true
	mockHandlerGenerator := func(context *hookstate.Context) hookstate.Handler {
		return mockHandler
	}

	s.manager.Register(regexp.MustCompile("test-hook"), mockHandlerGenerator)

	s.manager.Ensure()
	s.manager.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(mockHandler.BeforeCalled, Equals, true)
	c.Check(mockHandler.DoneCalled, Equals, true)
	c.Check(mockHandler.ErrorCalled, Equals, false)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, regexp.MustCompile(".*done failed at user request.*"))
}

func (s *hookManagerSuite) TestHookTaskHandlerErrorError(c *C) {
	// Register a handler generator for the "test-hook" hook
	mockHandler := hooktest.NewMockHandler()
	mockHandler.ErrorError = true
	mockHandlerGenerator := func(context *hookstate.Context) hookstate.Handler {
		return mockHandler
	}

	// Force the snap command to simply exit 1, so the handler Error() runs
	s.command = testutil.MockCommand(c, "snap", "exit 1")

	s.manager.Register(regexp.MustCompile("test-hook"), mockHandlerGenerator)

	s.manager.Ensure()
	s.manager.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(mockHandler.BeforeCalled, Equals, true)
	c.Check(mockHandler.DoneCalled, Equals, false)
	c.Check(mockHandler.ErrorCalled, Equals, true)

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, regexp.MustCompile(".*error failed at user request.*"))
}

func (s *hookManagerSuite) TestHookWithoutHandlerIsError(c *C) {
	// Note that we do NOT register a handler

	s.manager.Ensure()
	s.manager.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)
	checkTaskLogContains(c, s.task, regexp.MustCompile(".*no registered handlers for hook \"test-hook\".*"))
}

func (s *hookManagerSuite) TestHookWithMultipleHandlersIsError(c *C) {
	mockHandlerGenerator := func(context *hookstate.Context) hookstate.Handler {
		return hooktest.NewMockHandler()
	}

	// Register multiple times for this hook
	s.manager.Register(regexp.MustCompile("test-hook"), mockHandlerGenerator)
	s.manager.Register(regexp.MustCompile("test-hook"), mockHandlerGenerator)

	s.manager.Ensure()
	s.manager.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.task.Kind(), Equals, "run-hook")
	c.Check(s.task.Status(), Equals, state.ErrorStatus)
	c.Check(s.change.Status(), Equals, state.ErrorStatus)

	checkTaskLogContains(c, s.task, regexp.MustCompile(".*2 handlers registered for hook \"test-hook\".*"))
}

func checkTaskLogContains(c *C, task *state.Task, pattern *regexp.Regexp) {
	found := false
	for _, message := range task.Log() {
		if pattern.MatchString(message) {
			found = true
		}
	}

	c.Check(found, Equals, true, Commentf("Expected to find regex %q in task log: %v", pattern, task.Log()))
}
