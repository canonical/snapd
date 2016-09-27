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

package configstate_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type tasksetsSuite struct {
	state *state.State
}

var _ = Suite(&tasksetsSuite{})

func (s *tasksetsSuite) SetUpTest(c *C) {
	s.state = state.New(nil)
}

func (s *tasksetsSuite) TestChange(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	taskset := configstate.Change(s.state, "test-snap", map[string]interface{}{
		"foo": "bar",
	})

	tasks := taskset.Tasks()
	c.Assert(tasks, HasLen, 1)
	task := tasks[0]

	c.Assert(task.Kind(), Equals, "run-hook")
	var setup hookstate.HookSetup
	err := task.Get("hook-setup", &setup)
	c.Check(err, IsNil)
	c.Check(setup.Snap, Equals, "test-snap")
	c.Check(setup.Revision, Equals, snap.Revision{})
	c.Check(setup.Hook, Equals, "apply-config")
}
