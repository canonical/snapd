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
	"testing"

	"gopkg.in/tomb.v2"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func TestHookManager(t *testing.T) { TestingT(t) }

type hookManagerSuite struct {
	state   *state.State
	manager *HookManager
}

var _ = Suite(&hookManagerSuite{})

func (s *hookManagerSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.state = state.New(nil)
	manager, err := Manager(s.state)
	c.Assert(err, IsNil)
	s.manager = manager
}

func (s *hookManagerSuite) TearDownTest(c *C) {
	s.manager.Stop()
	dirs.SetRootDir("")
}

func (s *hookManagerSuite) TestDoRunHookMissingHookSetupIsError(c *C) {
	// Create task that is specifically missing the hook reference
	s.state.Lock()
	task := s.state.NewTask("foo", "bar")
	s.state.Unlock()

	err := s.manager.doRunHook(task, &tomb.Tomb{})
	c.Check(err, NotNil)
	c.Check(err, ErrorMatches, "failed to extract hook from task.*")
}

func (s *hookManagerSuite) TestRunHookInstruction(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	taskSet, err := RunHook(s.state, "test-snap", snap.R(1), "test-hook")
	c.Assert(err, IsNil, Commentf("RunHook unexpectedly failed"))
	c.Assert(taskSet, NotNil, Commentf("Expected RunHook to provide a task set"))

	tasks := taskSet.Tasks()
	c.Assert(tasks, HasLen, 1, Commentf("Expected task set to contain 1 task"))

	task := tasks[0]
	c.Check(task.Kind(), Equals, "run-hook")

	var setup hookSetup
	err = task.Get("hook-setup", &setup)
	c.Check(err, IsNil, Commentf("Expected task to contain hook"))
	c.Check(setup.Snap, Equals, "test-snap")
	c.Check(setup.Revision, Equals, snap.R(1))
	c.Check(setup.Hook, Equals, "test-hook")
}
