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

package ctlcmd_test

import (
	"testing"

	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type ctlcmdSuite struct {
	mockContext *hookstate.Context
}

var _ = Suite(&ctlcmdSuite{})

func (s *ctlcmdSuite) SetUpTest(c *C) {
	handler := hooktest.NewMockHandler()

	state := state.New(nil)
	state.Lock()
	defer state.Unlock()

	task := state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}

	var err error
	s.mockContext, err = hookstate.NewContext(task, task.State(), setup, handler, "")
	c.Assert(err, IsNil)
}

func (s *ctlcmdSuite) TestNonExistingCommand(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"foo"}, 0)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
	c.Check(err, ErrorMatches, ".*[Uu]nknown command.*")
}

func (s *ctlcmdSuite) TestCommandOutput(c *C) {
	mockCommand := ctlcmd.AddMockCommand("mock")
	defer ctlcmd.RemoveCommand("mock")

	mockCommand.FakeStdout = "test stdout"
	mockCommand.FakeStderr = "test stderr"

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"mock", "foo"}, 0)
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "test stdout")
	c.Check(string(stderr), Equals, "test stderr")
	c.Check(mockCommand.Args, DeepEquals, []string{"foo"})
}
