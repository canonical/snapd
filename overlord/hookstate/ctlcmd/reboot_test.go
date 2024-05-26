// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type rebootSuite struct {
	testutil.BaseTest
	state       *state.State
	mockContext *hookstate.Context
	mockHandler *hooktest.MockHandler
	hookTask    *state.Task
	restartTask *state.Task
}

var _ = Suite(&rebootSuite{})

func (s *rebootSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())

	s.mockHandler = hooktest.NewMockHandler()

	s.state = state.New(nil)
	s.state.Lock()
	defer s.state.Unlock()
	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(42), Hook: "install-device"}

	ctx := mylog.Check2(hookstate.NewContext(task, s.state, setup, s.mockHandler, ""))

	s.mockContext = ctx

	s.hookTask = task
	s.restartTask = s.state.NewTask("restart-task", "task managing restart")
}

func (s *rebootSuite) TestBadHook(c *C) {
	s.state.Lock()
	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(42), Hook: "configure"}
	s.state.Unlock()

	ctx := mylog.Check2(hookstate.NewContext(task, s.state, setup, s.mockHandler, ""))


	_, _ = mylog.Check3(ctlcmd.Run(ctx, []string{"reboot", "--halt"}, 0))
	c.Assert(err, ErrorMatches, `cannot use reboot command outside of gadget install-device hook`)
}

func (s *rebootSuite) TestBadArgs(c *C) {
	type tableT struct {
		args []string
		err  string
	}
	table := []tableT{
		{
			[]string{"reboot"},
			"either --halt or --poweroff must be specified",
		}, {
			[]string{"reboot", "--halt", "--poweroff"},
			"cannot specify both --halt and --poweroff",
		}, {
			[]string{"reboot", "--foo"},
			"unknown flag `foo'",
		},
	}

	for i, t := range table {
		_, _ := mylog.Check3(ctlcmd.Run(s.mockContext, t.args, 0))
		c.Check(err, ErrorMatches, t.err, Commentf("%d", i))
	}
}

func (s *rebootSuite) TestRegularRunHalt(c *C) {
	s.state.Lock()
	s.hookTask.Set("restart-task", s.restartTask.ID())
	chg := s.state.NewChange("install-device", "install-device")
	chg.AddTask(s.restartTask)
	s.state.Unlock()

	_, _ := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"reboot", "--halt"}, 0))


	s.state.Lock()
	defer s.state.Unlock()
	var rebootOpts devicestate.RebootOptions
	mylog.Check(s.restartTask.Get("reboot", &rebootOpts))


	c.Check(rebootOpts.Op, Equals, devicestate.RebootHaltOp)
}

func (s *rebootSuite) TestRegularRunPoweroff(c *C) {
	s.state.Lock()
	s.hookTask.Set("restart-task", s.restartTask.ID())
	chg := s.state.NewChange("install-device", "install-device")
	chg.AddTask(s.restartTask)
	s.state.Unlock()

	_, _ := mylog.Check3(ctlcmd.Run(s.mockContext, []string{"reboot", "--poweroff"}, 0))


	s.state.Lock()
	defer s.state.Unlock()
	var rebootOpts devicestate.RebootOptions
	mylog.Check(s.restartTask.Get("reboot", &rebootOpts))


	c.Check(rebootOpts.Op, Equals, devicestate.RebootPoweroffOp)
}
