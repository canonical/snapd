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

	"gopkg.in/tomb.v2"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

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
	c.Check(err, ErrorMatches, "cannot extract hook setup from task.*")
}

func (s *hookManagerSuite) TestRunHookInstruction(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	task := HookTask(s.state, "test summary", "test-snap", snap.R(1), "test-hook")
	c.Assert(task, NotNil, Commentf("Expected HookTask to return a task"))
	c.Check(task.Kind(), Equals, "run-hook")

	var setup hookSetup
	err := task.Get("hook-setup", &setup)
	c.Check(err, IsNil, Commentf("Expected task to contain hook setup"))
	c.Check(setup.Snap, Equals, "test-snap")
	c.Check(setup.Revision, Equals, snap.R(1))
	c.Check(setup.Hook, Equals, "test-hook")
}

func (s *hookManagerSuite) TestHookSetupJsonMarshal(c *C) {
	hookSetup := hookSetup{Snap: "snap-name", Revision: snap.R(1), Hook: "hook-name"}
	out, err := json.Marshal(hookSetup)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "{\"snap\":\"snap-name\",\"revision\":\"1\",\"hook\":\"hook-name\"}")
}

func (s *hookManagerSuite) TestHookSetupJsonUnmarshal(c *C) {
	out, err := json.Marshal(hookSetup{Snap: "snap-name", Revision: snap.R(1), Hook: "hook-name"})
	c.Assert(err, IsNil)

	var setup hookSetup
	err = json.Unmarshal(out, &setup)
	c.Assert(err, IsNil)
	c.Check(setup.Snap, Equals, "snap-name")
	c.Check(setup.Revision, Equals, snap.R(1))
	c.Check(setup.Hook, Equals, "hook-name")
}
