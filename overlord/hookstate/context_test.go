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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type contextSuite struct {
	context *Context
	task    *state.Task
	setup   hookSetup
}

var _ = Suite(&contextSuite{})

func (s *contextSuite) SetUpTest(c *C) {
	state := state.New(nil)
	state.Lock()
	defer state.Unlock()

	s.task = state.NewTask("test-task", "my test task")
	s.setup = hookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}
	s.context = &Context{task: s.task, setup: s.setup}
}

func (s *contextSuite) TestHookSetup(c *C) {
	c.Check(s.context.HookName(), Equals, "test-hook")
	c.Check(s.context.SnapName(), Equals, "test-snap")
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

func (s *contextSuite) TestSetPersistence(c *C) {
	s.context.Lock()
	s.context.Set("foo", "bar")
	s.context.Unlock()

	// Verify that "foo" is still "bar" within another context of the same hook
	// on the same task.
	anotherContext := &Context{task: s.task, setup: s.setup}
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
