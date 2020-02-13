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
	"fmt"
	"testing"

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"

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

func taskKinds(tasks []*state.Task) []string {
	kinds := make([]string, len(tasks))
	for i, task := range tasks {
		k := task.Kind()
		if k == "run-hook" {
			var hooksup hookstate.HookSetup
			if err := task.Get("hook-setup", &hooksup); err != nil {
				panic(err)
			}
			k = fmt.Sprintf("%s[%s]", k, hooksup.Hook)
		}
		kinds[i] = k
	}
	return kinds
}

func (s *ctlcmdSuite) TestHiddenCommand(c *C) {
	ctlcmd.AddHiddenMockCommand("mock-hidden")
	ctlcmd.AddMockCommand("mock-shown")
	defer ctlcmd.RemoveCommand("mock-hidden")
	defer ctlcmd.RemoveCommand("mock-shown")

	_, _, err := ctlcmd.Run(s.mockContext, []string{"--help"}, 0)
	// help message output is returned as *flags.Error with
	// Type as flags.ErrHelp
	c.Assert(err, FitsTypeOf, &flags.Error{})
	c.Check(err.(*flags.Error).Type, Equals, flags.ErrHelp)
	// snapctl is mentioned (not snapd)
	c.Check(err.Error(), testutil.Contains, "snapctl")
	// mock-shown is in the help message
	c.Check(err.Error(), testutil.Contains, "  mock-shown\n")
	// mock-hidden is not in the help message
	c.Check(err.Error(), Not(testutil.Contains), "  mock-hidden\n")
}
