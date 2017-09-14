// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/servicectl"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type servicectlSuite struct {
	mockContext *hookstate.Context
	mockHandler *hooktest.MockHandler

	restore func()
}

var _ = Suite(&servicectlSuite{})

const testSnapYaml = `name: test-snap
version: 1.0
summary: test-snap
apps:
 normal-app:
  command: bin/dummy
 test-service:
  command: bin/service
  daemon: simple
  reload-command: bin/reload
`

const otherSnapYaml = `name: other-snap
version: 1.0
summary: other-snap
apps:
 test-service:
  command: bin/service
  daemon: simple
  reload-command: bin/reload
`

func mockServiceControlFunc(testServiceControlInputs func(appInfos []*snap.AppInfo, inst *servicectl.Instruction)) func() {
	return ctlcmd.MockServiceControlFunc(func(st *state.State, appInfos []*snap.AppInfo, inst *servicectl.Instruction) (*state.Change, error) {
		testServiceControlInputs(appInfos, inst)
		st.Lock()
		defer st.Unlock()
		chg := st.NewChange("service-control", "")
		chg.SetStatus(state.DoneStatus)
		return chg, nil
	})
}

func (s *servicectlSuite) SetUpTest(c *C) {
	oldRoot := dirs.GlobalRootDir
	dirs.SetRootDir(c.MkDir())
	s.restore = func() {
		dirs.SetRootDir(oldRoot)
	}

	s.mockHandler = hooktest.NewMockHandler()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	// mock installed snaps
	info1 := snaptest.MockSnap(c, string(testSnapYaml), "", &snap.SideInfo{
		Revision: snap.R(1),
	})
	info2 := snaptest.MockSnap(c, string(otherSnapYaml), "", &snap.SideInfo{
		Revision: snap.R(1),
	})
	snapstate.Set(st, info1.Name(), &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{
				RealName: info1.Name(),
				Revision: info1.Revision,
				SnapID:   "test-snap-id",
			},
		},
		Current: info1.Revision,
	})
	snapstate.Set(st, info2.Name(), &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{
				RealName: info2.Name(),
				Revision: info2.Revision,
				SnapID:   "other-snap-id",
			},
		},
		Current: info2.Revision,
	})

	task := st.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}

	var err error
	s.mockContext, err = hookstate.NewContext(task, task.State(), setup, s.mockHandler, "")
	c.Assert(err, IsNil)
}

func (s *servicectlSuite) TearDownTest(c *C) {
	s.restore()
}

func (s *servicectlSuite) TestStopCommand(c *C) {
	var serviceCtlFuncCalled bool
	restore := mockServiceControlFunc(func(appInfos []*snap.AppInfo, inst *servicectl.Instruction) {
		serviceCtlFuncCalled = true
		c.Assert(appInfos, HasLen, 1)
		c.Assert(appInfos[0].Name, Equals, "test-service")
		c.Assert(inst, DeepEquals, &servicectl.Instruction{
			Action: "stop",
			Names:  []string{"test-snap.test-service"},
			StopOptions: client.StopOptions{
				Disable: false,
			},
		},
		)
	})
	defer restore()
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"stop", "test-snap.test-service"})
	c.Check(err, IsNil)
	c.Check(string(stderr), Equals, "")
	c.Check(string(stdout), Equals, "")
	c.Assert(serviceCtlFuncCalled, Equals, true)
}

func (s *servicectlSuite) TestStopCommandUnknownService(c *C) {
	var serviceCtlFuncCalled bool
	restore := mockServiceControlFunc(func(appInfos []*snap.AppInfo, inst *servicectl.Instruction) {
		serviceCtlFuncCalled = true
	})
	defer restore()
	_, _, err := ctlcmd.Run(s.mockContext, []string{"stop", "test-snap.fooservice"})
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `unknown service: "test-snap.fooservice"`)
	c.Assert(serviceCtlFuncCalled, Equals, false)
}

func (s *servicectlSuite) TestStopCommandFailsOnOtherSnap(c *C) {
	var serviceCtlFuncCalled bool
	restore := mockServiceControlFunc(func(appInfos []*snap.AppInfo, inst *servicectl.Instruction) {
		serviceCtlFuncCalled = true
	})
	defer restore()
	// verify that snapctl is not allowed to control services of other snaps (only the one of its hook)
	_, _, err := ctlcmd.Run(s.mockContext, []string{"stop", "other-snap.test-service"})
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, `unknown service: "other-snap.test-service"`)
	c.Assert(serviceCtlFuncCalled, Equals, false)
}

func (s *servicectlSuite) TestStartCommand(c *C) {
	var serviceCtlFuncCalled bool
	restore := mockServiceControlFunc(func(appInfos []*snap.AppInfo, inst *servicectl.Instruction) {
		serviceCtlFuncCalled = true
		c.Assert(appInfos, HasLen, 1)
		c.Assert(appInfos[0].Name, Equals, "test-service")
		c.Assert(inst, DeepEquals, &servicectl.Instruction{
			Action: "start",
			Names:  []string{"test-snap.test-service"},
			StartOptions: client.StartOptions{
				Enable: false,
			},
		},
		)
	})
	defer restore()
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"start", "test-snap.test-service"})
	c.Check(err, IsNil)
	c.Check(string(stderr), Equals, "")
	c.Check(string(stdout), Equals, "")
	c.Assert(serviceCtlFuncCalled, Equals, true)
}

func (s *servicectlSuite) TestRestartCommand(c *C) {
	var serviceCtlFuncCalled bool
	restore := mockServiceControlFunc(func(appInfos []*snap.AppInfo, inst *servicectl.Instruction) {
		serviceCtlFuncCalled = true
		c.Assert(appInfos, HasLen, 1)
		c.Assert(appInfos[0].Name, Equals, "test-service")
		c.Assert(inst, DeepEquals, &servicectl.Instruction{
			Action: "restart",
			Names:  []string{"test-snap.test-service"},
			RestartOptions: client.RestartOptions{
				Reload: false,
			},
		},
		)
	})
	defer restore()
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"restart", "test-snap.test-service"})
	c.Check(err, IsNil)
	c.Check(string(stderr), Equals, "")
	c.Check(string(stdout), Equals, "")
	c.Assert(serviceCtlFuncCalled, Equals, true)
}
