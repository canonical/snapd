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

package hookstate

import (
	"encoding/json"
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type contextSuite struct {
	context *Context
	task    *state.Task
	state   *state.State
	setup   *HookSetup

	componentContext *Context
	componentSetup   *HookSetup
	componentTask    *state.Task
}

var _ = Suite(&contextSuite{})

func (s *contextSuite) SetUpTest(c *C) {
	s.state = state.New(nil)
	s.state.Lock()
	defer s.state.Unlock()

	s.task = s.state.NewTask("test-task", "my test task")
	s.setup = &HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}
	var err error
	s.context, err = NewContext(s.task, s.task.State(), s.setup, nil, "")
	c.Check(err, IsNil)

	s.componentTask = s.state.NewTask("test-component-task", "my test component task")
	s.componentSetup = &HookSetup{
		Snap:              "test-snap",
		Revision:          snap.R(1),
		Hook:              "test-hook",
		Component:         "test-component",
		ComponentRevision: snap.R(1),
	}
	s.componentContext, err = NewContext(s.componentTask, s.componentTask.State(), s.componentSetup, nil, "")
	c.Check(err, IsNil)
}

func (s *contextSuite) TestHookSetup(c *C) {
	c.Check(s.context.HookName(), Equals, "test-hook")
	c.Check(s.context.InstanceName(), Equals, "test-snap")
	c.Check(s.context.IsSnapHook(), Equals, true)
	c.Check(s.context.IsComponentHook(), Equals, false)

	c.Check(s.componentContext.ComponentName(), Equals, "test-component")
	c.Check(s.componentContext.ComponentRevision(), Equals, snap.R(1))
	c.Check(s.componentContext.IsSnapHook(), Equals, false)
	c.Check(s.componentContext.IsComponentHook(), Equals, true)
}

func (s *contextSuite) TestHookSource(c *C) {
	c.Check(s.context.HookSource(), Equals, "test-snap")
	c.Check(s.componentContext.HookSource(), Equals, "test-snap+test-component")

	// insert an instance key for a quick test
	s.componentContext.setup.Snap = "test-snap_instance-key"
	defer func() { s.componentContext.setup.Snap = "test-snap" }()

	c.Check(s.componentContext.HookSource(), Equals, "test-snap_instance-key+test-component")
}

func (s *contextSuite) TestSetAndGet(c *C) {
	s.context.Lock()
	defer s.context.Unlock()

	var output string
	c.Check(s.context.Get("foo", &output), NotNil)

	s.context.Set("foo", "bar")
	c.Check(s.context.Get("foo", &output), IsNil, Commentf("Expected context to contain 'foo'"))
	c.Check(output, Equals, "bar")

	// Test another non-existing key, but after the context data was created.
	c.Check(s.context.Get("baz", &output), NotNil)
}

func (s *contextSuite) TestSetAndGetNumber(c *C) {
	s.context.Lock()
	defer s.context.Unlock()

	s.context.Set("num", 1234567890)

	var output interface{}
	c.Check(s.context.Get("num", &output), IsNil)
	c.Assert(output, Equals, json.Number("1234567890"))
}

func (s *contextSuite) TestSetPersistence(c *C) {
	s.context.Lock()
	s.context.Set("foo", "bar")
	s.context.Unlock()

	// Verify that "foo" is still "bar" within another context of the same hook
	// on the same task.
	anotherContext := &Context{task: s.task, state: s.task.State(), setup: s.setup}
	anotherContext.Lock()
	defer anotherContext.Unlock()

	var output string
	c.Check(anotherContext.Get("foo", &output), IsNil, Commentf("Expected new context to also contain 'foo'"))
	c.Check(output, Equals, "bar")
}

func (s *contextSuite) TestSetUnmarshalable(c *C) {
	s.context.Lock()
	defer s.context.Unlock()

	defer func() {
		c.Check(recover(), Matches, ".*cannot marshal context value.*", Commentf("Expected panic when attempting install"))
	}()

	s.context.Set("foo", func() {})
}

func (s *contextSuite) TestGetIsolatedFromTask(c *C) {
	// Set data in the task itself
	s.task.State().Lock()
	s.task.Set("foo", "bar")
	s.task.State().Unlock()

	s.context.Lock()
	defer s.context.Unlock()

	// Verify that "foo" is not set when asking for data from the hook context
	var output string
	c.Check(s.context.Get("foo", &output), NotNil, Commentf("Expected context data to be isolated from task"))
}

func (s *contextSuite) TestCache(c *C) {
	s.context.Lock()
	defer s.context.Unlock()

	c.Check(s.context.Cached("foo"), IsNil)

	s.context.Cache("foo", "bar")
	c.Check(s.context.Cached("foo"), Equals, "bar")

	// Test another non-existing key, but after the context cache was created.
	c.Check(s.context.Cached("baz"), IsNil)
}

func (s *contextSuite) TestDone(c *C) {
	s.context.Lock()
	defer s.context.Unlock()

	called := false
	s.context.OnDone(func() error {
		called = true
		return nil
	})

	s.context.Done()
	c.Check(called, Equals, true, Commentf("Expected finalizer to be called"))
}

func (s *contextSuite) TestEphemeralContextGetSet(c *C) {
	context, err := NewContext(nil, s.state, &HookSetup{Snap: "test-snap"}, nil, "")
	c.Assert(err, IsNil)
	context.Lock()
	defer context.Unlock()

	var output string
	c.Check(context.Get("foo", &output), NotNil)

	context.Set("foo", "bar")
	c.Check(context.Get("foo", &output), IsNil, Commentf("Expected context to contain 'foo'"))
	c.Check(output, Equals, "bar")

	// Test another non-existing key, but after the context data was created.
	c.Check(context.Get("baz", &output), NotNil)
}

func (s *contextSuite) TestChangeID(c *C) {
	context, err := NewContext(nil, s.state, &HookSetup{Snap: "test-snap"}, nil, "")
	c.Assert(err, IsNil)
	c.Check(context.ChangeID(), Equals, "")

	s.state.Lock()
	defer s.state.Unlock()

	task := s.state.NewTask("foo", "")
	context, err = NewContext(task, s.state, &HookSetup{Snap: "test-snap"}, nil, "")
	c.Assert(err, IsNil)
	c.Check(context.ChangeID(), Equals, "")

	chg := s.state.NewChange("bar", "")
	chg.AddTask(task)
	context, err = NewContext(task, s.state, &HookSetup{Snap: "test-snap"}, nil, "")
	c.Assert(err, IsNil)
	c.Check(context.ChangeID(), Equals, chg.ID())
}

func (s *contextSuite) TestChangeErrorf(c *C) {
	mockLog, restore := logger.MockLogger()
	defer restore()

	if v, ok := os.LookupEnv("SNAPD_DEBUG"); ok {
		os.Unsetenv("SNAPD_DEBUG")
		defer os.Setenv("SNAPD_DEBUG=%v", v)
	}

	s.state.Lock()
	task1 := s.state.NewTask("foo1", "summary foo1")
	task2 := s.state.NewTask("foo2", "summary foo2")
	s.state.Unlock()

	for _, tc := range []struct {
		task                 *state.Task
		ignoreError          bool
		expectedLoggerOutput string
		expectedTaskLog      string
	}{
		{nil, false, `.* context.go:.*: some error\n`, ``},
		{nil, true, `.* context.go:.*: some error\n`, ``},
		// ignore error hooks log errors to both logger and task
		{task1, true, `.* context.go:.*: ERROR task ` + task1.ID() + ` \(summary foo1\): some error\n`, `.* ERROR some error`},
		// normal hooks only log errors to the task
		{task2, false, ``, `.* ERROR some error`},
	} {
		hs := &HookSetup{Snap: "test-snap", IgnoreError: tc.ignoreError}
		context, err := NewContext(tc.task, s.state, hs, nil, "")
		c.Assert(err, IsNil)
		context.Lock()
		context.Errorf("some error")
		context.Unlock()
		c.Check(mockLog.String(), Matches, tc.expectedLoggerOutput)
		mockLog.Reset()
		if tc.task != nil {
			s.state.Lock()
			taskLog := tc.task.Log()
			s.state.Unlock()
			if tc.expectedTaskLog != "" {
				c.Assert(taskLog, HasLen, 1)
				c.Check(taskLog[0], Matches, tc.expectedTaskLog)
			} else {
				c.Assert(taskLog, HasLen, 0)
			}
		}
	}
}

func (s *contextSuite) TestChangeErrorfHookSetupNilPointerDoesNotCausePanic(c *C) {
	s.state.Lock()
	task := s.state.NewTask("foo", "")
	s.state.Unlock()

	var hookSetup *HookSetup = nil
	context, err := NewContext(task, s.state, hookSetup, nil, "")
	c.Assert(err, IsNil)
	// it's enough to check here that there is no panic from "nil" hookSetup
	context.Lock()
	context.Errorf("some error")
	context.Unlock()
}
