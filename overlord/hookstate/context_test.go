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
	s.task = state.NewTask("test-task", "my test task")
	state.Unlock()

	s.setup = hookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}
	s.context = newContext(s.task, s.setup)
}

func (s *contextSuite) TestHookSetup(c *C) {
	c.Check(s.context.HookName(), Equals, "test-hook")
	c.Check(s.context.SnapName(), Equals, "test-snap")
}

func (s *contextSuite) TestSetAndGet(c *C) {
	s.context.Lock()
	defer s.context.Unlock()

	output, err := s.context.Get("foo")
	c.Check(err, NotNil)

	s.context.Set("foo", "bar")
	output, err = s.context.Get("foo")
	c.Check(err, IsNil, Commentf("Expected context to contain 'foo'"))
	c.Check(output, Equals, "bar")
}

func (s *contextSuite) TestSetPersistence(c *C) {
	s.context.Lock()
	defer s.context.Unlock()

	s.context.Set("foo", "bar")

	// Verify that "foo" is still "bar" within another context of the same hook
	// on the same task.
	anotherContext := newContext(s.task, s.setup)
	anotherContext.Lock()
	defer anotherContext.Unlock()

	output, err := anotherContext.Get("foo")
	c.Check(err, IsNil, Commentf("Expected new context to also contain 'foo'"))
	c.Check(output, Equals, "bar")
}

func (s *contextSuite) TestSetPersistenceIsHookSpecific(c *C) {
	s.context.Lock()
	defer s.context.Unlock()

	s.context.Set("foo", "bar")

	// Verify that "foo" is not "bar" within the context of another hook on the
	// same task.
	s.setup.Hook = "foo"
	anotherContext := newContext(s.task, s.setup)
	anotherContext.Lock()
	defer anotherContext.Unlock()

	_, err := anotherContext.Get("foo")
	c.Check(err, NotNil, Commentf("Expected new context to not contain 'foo'"))
}

func (s *contextSuite) TestGetIsolatedFromTask(c *C) {
	// Set data in the task itself
	s.task.State().Lock()
	s.task.Set("foo", "bar")
	s.task.State().Unlock()

	// Verify that "foo" is not set when asking for data from the hook context
	_, err := s.context.Get("foo")
	c.Check(err, NotNil, Commentf("Expected context data to be isolated from task"))
}
