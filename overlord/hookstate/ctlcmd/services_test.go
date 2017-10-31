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
	"fmt"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type servicectlSuite struct {
	testutil.BaseTest
	st          *state.State
	mockContext *hookstate.Context
	mockHandler *hooktest.MockHandler
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

func mockServiceChangeFunc(testServiceControlInputs func(appInfos []*snap.AppInfo, inst *servicestate.Instruction)) func() {
	return ctlcmd.MockServicestateControlFunc(func(st *state.State, appInfos []*snap.AppInfo, inst *servicestate.Instruction) (*state.TaskSet, error) {
		testServiceControlInputs(appInfos, inst)
		return nil, fmt.Errorf("forced error")
	})
}

func (s *servicectlSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	oldRoot := dirs.GlobalRootDir
	dirs.SetRootDir(c.MkDir())

	testutil.MockCommand(c, "systemctl", "")

	s.BaseTest.AddCleanup(func() {
		dirs.SetRootDir(oldRoot)
	})
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.mockHandler = hooktest.NewMockHandler()

	s.st = state.New(nil)
	s.st.Lock()
	defer s.st.Unlock()

	// mock installed snaps
	info1 := snaptest.MockSnap(c, string(testSnapYaml), "", &snap.SideInfo{
		Revision: snap.R(1),
	})
	info2 := snaptest.MockSnap(c, string(otherSnapYaml), "", &snap.SideInfo{
		Revision: snap.R(1),
	})
	snapstate.Set(s.st, info1.Name(), &snapstate.SnapState{
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
	snapstate.Set(s.st, info2.Name(), &snapstate.SnapState{
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

	task := s.st.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}

	var err error
	s.mockContext, err = hookstate.NewContext(task, task.State(), setup, s.mockHandler, "")
	c.Assert(err, IsNil)
}

func (s *servicectlSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *servicectlSuite) TestStopCommand(c *C) {
	var serviceChangeFuncCalled bool
	restore := mockServiceChangeFunc(func(appInfos []*snap.AppInfo, inst *servicestate.Instruction) {
		serviceChangeFuncCalled = true
		c.Assert(appInfos, HasLen, 1)
		c.Assert(appInfos[0].Name, Equals, "test-service")
		c.Assert(inst, DeepEquals, &servicestate.Instruction{
			Action: "stop",
			Names:  []string{"test-snap.test-service"},
			StopOptions: client.StopOptions{
				Disable: false,
			},
		},
		)
	})
	defer restore()
	_, _, err := ctlcmd.Run(s.mockContext, []string{"stop", "test-snap.test-service"})
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "forced error")
	c.Assert(serviceChangeFuncCalled, Equals, true)
}

func (s *servicectlSuite) TestStopCommandUnknownService(c *C) {
	var serviceChangeFuncCalled bool
	restore := mockServiceChangeFunc(func(appInfos []*snap.AppInfo, inst *servicestate.Instruction) {
		serviceChangeFuncCalled = true
	})
	defer restore()
	_, _, err := ctlcmd.Run(s.mockContext, []string{"stop", "test-snap.fooservice"})
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `unknown service: "test-snap.fooservice"`)
	c.Assert(serviceChangeFuncCalled, Equals, false)
}

func (s *servicectlSuite) TestStopCommandFailsOnOtherSnap(c *C) {
	var serviceChangeFuncCalled bool
	restore := mockServiceChangeFunc(func(appInfos []*snap.AppInfo, inst *servicestate.Instruction) {
		serviceChangeFuncCalled = true
	})
	defer restore()
	// verify that snapctl is not allowed to control services of other snaps (only the one of its hook)
	_, _, err := ctlcmd.Run(s.mockContext, []string{"stop", "other-snap.test-service"})
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, `unknown service: "other-snap.test-service"`)
	c.Assert(serviceChangeFuncCalled, Equals, false)
}

func (s *servicectlSuite) TestStartCommand(c *C) {
	var serviceChangeFuncCalled bool
	restore := mockServiceChangeFunc(func(appInfos []*snap.AppInfo, inst *servicestate.Instruction) {
		serviceChangeFuncCalled = true
		c.Assert(appInfos, HasLen, 1)
		c.Assert(appInfos[0].Name, Equals, "test-service")
		c.Assert(inst, DeepEquals, &servicestate.Instruction{
			Action: "start",
			Names:  []string{"test-snap.test-service"},
			StartOptions: client.StartOptions{
				Enable: false,
			},
		},
		)
	})
	defer restore()
	_, _, err := ctlcmd.Run(s.mockContext, []string{"start", "test-snap.test-service"})
	c.Check(err, NotNil)
	c.Check(err, ErrorMatches, "forced error")
	c.Assert(serviceChangeFuncCalled, Equals, true)
}

func (s *servicectlSuite) TestRestartCommand(c *C) {
	var serviceChangeFuncCalled bool
	restore := mockServiceChangeFunc(func(appInfos []*snap.AppInfo, inst *servicestate.Instruction) {
		serviceChangeFuncCalled = true
		c.Assert(appInfos, HasLen, 1)
		c.Assert(appInfos[0].Name, Equals, "test-service")
		c.Assert(inst, DeepEquals, &servicestate.Instruction{
			Action: "restart",
			Names:  []string{"test-snap.test-service"},
			RestartOptions: client.RestartOptions{
				Reload: false,
			},
		},
		)
	})
	defer restore()
	_, _, err := ctlcmd.Run(s.mockContext, []string{"restart", "test-snap.test-service"})
	c.Check(err, NotNil)
	c.Check(err, ErrorMatches, "forced error")
	c.Assert(serviceChangeFuncCalled, Equals, true)
}

func (s *servicectlSuite) TestQueuedCommands(c *C) {
	s.st.Lock()
	ts := configstate.Configure(s.st, "test-snap", nil, 0)
	chg := s.st.NewChange("configure change", "configure change")
	chg.AddAll(ts)
	s.st.Unlock()

	task := ts.Tasks()[0]
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "configure"}
	context, err := hookstate.NewContext(task, task.State(), setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	_, _, err = ctlcmd.Run(context, []string{"stop", "test-snap.test-service"})
	c.Check(err, IsNil)
	_, _, err = ctlcmd.Run(context, []string{"start", "test-snap.test-service"})
	c.Check(err, IsNil)
	_, _, err = ctlcmd.Run(context, []string{"restart", "test-snap.test-service"})
	c.Check(err, IsNil)

	s.st.Lock()
	defer s.st.Unlock()

	allTasks := chg.Tasks()
	c.Assert(allTasks, HasLen, 4)
	c.Check(allTasks[0].Summary(), Equals, `Run configure hook of "test-snap" snap if present`)
	c.Check(allTasks[1].Summary(), Equals, "stop of [test-snap.test-service]")
	c.Check(allTasks[2].Summary(), Equals, "start of [test-snap.test-service]")
	c.Check(allTasks[3].Summary(), Equals, "restart of [test-snap.test-service]")
}
