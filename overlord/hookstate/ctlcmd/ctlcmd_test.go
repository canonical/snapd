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
	"strings"
	"testing"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
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

	s.mockContext = mylog.Check2(hookstate.NewContext(task, task.State(), setup, handler, ""))

}

func (s *ctlcmdSuite) TestNonExistingCommand(c *C) {
	stdout, stderr := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"foo"}, 0))
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
	c.Check(err, ErrorMatches, ".*[Uu]nknown command.*")
}

func (s *ctlcmdSuite) TestCommandOutput(c *C) {
	mockCommand := ctlcmd.AddMockCommand("mock")
	defer ctlcmd.RemoveCommand("mock")

	mockCommand.FakeStdout = "test stdout"
	mockCommand.FakeStderr = "test stderr"

	stdout, stderr := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"mock", "foo"}, 0))
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
			mylog.Check(task.Get("hook-setup", &hooksup))

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

	_, _ := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"--help"}, 0))
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

func (s *ctlcmdSuite) TestRootRequiredCommandFailure(c *C) {
	_, _ := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"start"}, 1000))

	c.Check(err, FitsTypeOf, &ctlcmd.ForbiddenCommandError{})
	c.Check(err.Error(), Equals, `cannot use "start" with uid 1000, try with sudo`)
}

func (s *ctlcmdSuite) TestRootRequiredCommandFailureParsingTweak(c *C) {
	_, _ := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"start", "--", "--help"}, 1000))

	c.Check(err, FitsTypeOf, &ctlcmd.ForbiddenCommandError{})
	c.Check(err.Error(), Equals, `cannot use "start" with uid 1000, try with sudo`)
}

func (s *ctlcmdSuite) TestRunNoArgsFailure(c *C) {
	_, _ := mylog.Check3(ctlcmd.Run(s.mockContext, []string{}, 0))
	c.Check(err, NotNil)
}

func (s *ctlcmdSuite) TestRunOnlyHelp(c *C) {
	_, _ := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"-h"}, 1000))
	c.Check(err, NotNil)
	c.Assert(strings.HasPrefix(err.Error(), "Usage:"), Equals, true)

	_, _ = mylog.Check3(ctlcmd.Run(s.mockContext, []string{"--help"}, 1000))
	c.Check(err, NotNil)
	c.Assert(strings.HasPrefix(err.Error(), "Usage:"), Equals, true)
}

func (s *ctlcmdSuite) TestRunHelpAtAnyPosition(c *C) {
	_, _ := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"start", "a", "-h"}, 1000))
	c.Check(err, NotNil)
	c.Assert(strings.HasPrefix(err.Error(), "Usage:"), Equals, true)

	_, _ = mylog.Check3(ctlcmd.Run(s.mockContext, []string{"start", "a", "b", "--help"}, 1000))
	c.Check(err, NotNil)
	c.Assert(strings.HasPrefix(err.Error(), "Usage:"), Equals, true)
}

func (s *ctlcmdSuite) TestRunNonRootAllowedCommandWithAllowedCmdAsArg(c *C) {
	// this test protects us against a future refactor introducing a bug that allows
	// a root-only command to run without root if an arg is in the nonRootAllowed list
	_, _ := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"set", "get", "a"}, 1000))
	c.Check(err, FitsTypeOf, &ctlcmd.ForbiddenCommandError{})
	c.Check(err.Error(), Equals, `cannot use "set" with uid 1000, try with sudo`)
}
