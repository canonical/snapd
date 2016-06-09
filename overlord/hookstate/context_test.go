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
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func TestContext(t *testing.T) { TestingT(t) }

type contextSuite struct {
	context *hookstate.Context
}

var _ = Suite(&contextSuite{})

func (s *contextSuite) SetUpTest(c *C) {
	state := state.New(nil)
	state.Lock()
	defer state.Unlock()

	task := state.NewTask("test-task", "my test task")
	hookSetup := hookstate.NewHookSetup("test-snap", snap.R(1), "test-hook")
	s.context = hookstate.NewContext(task, hookSetup)
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
}
