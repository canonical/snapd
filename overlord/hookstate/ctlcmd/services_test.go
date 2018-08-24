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
	"sort"

	"golang.org/x/net/context"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/testutil"
)

type fakeStore struct {
	storetest.Store
}

func (f *fakeStore) SnapAction(_ context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, user *auth.UserState, opts *store.RefreshOptions) ([]*snap.Info, error) {
	if len(actions) == 1 && actions[0].Action == "install" {
		snapName, instanceKey := snap.SplitInstanceName(actions[0].InstanceName)
		if instanceKey != "" {
			panic(fmt.Sprintf("unexpected instance name %q in snap install action", actions[0].InstanceName))
		}

		return []*snap.Info{{
			SideInfo: snap.SideInfo{
				RealName: snapName,
				Revision: snap.R(2),
			},
			Architectures: []string{"all"},
		}}, nil
	}

	return []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "test-snap",
			Revision: snap.R(2),
			SnapID:   "test-snap-id",
		},
		Architectures: []string{"all"},
	}, {SideInfo: snap.SideInfo{
		RealName: "other-snap",
		Revision: snap.R(2),
		SnapID:   "other-snap-id",
	},
		Architectures: []string{"all"},
	}}, nil
}

type servicectlSuite struct {
	testutil.BaseTest
	st          *state.State
	fakeStore   fakeStore
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
	return ctlcmd.MockServicestateControlFunc(func(st *state.State, appInfos []*snap.AppInfo, inst *servicestate.Instruction, context *hookstate.Context) ([]*state.TaskSet, error) {
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

	snapstate.ReplaceStore(s.st, &s.fakeStore)

	// mock installed snaps
	info1 := snaptest.MockSnap(c, string(testSnapYaml), &snap.SideInfo{
		Revision: snap.R(1),
	})
	info2 := snaptest.MockSnap(c, string(otherSnapYaml), &snap.SideInfo{
		Revision: snap.R(1),
	})
	snapstate.Set(s.st, info1.InstanceName(), &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{
				RealName: info1.SnapName(),
				Revision: info1.Revision,
				SnapID:   "test-snap-id",
			},
		},
		Current: info1.Revision,
	})
	snapstate.Set(s.st, info2.InstanceName(), &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{
				RealName: info2.SnapName(),
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
	_, _, err := ctlcmd.Run(s.mockContext, []string{"stop", "test-snap.test-service"}, 0)
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
	_, _, err := ctlcmd.Run(s.mockContext, []string{"stop", "test-snap.fooservice"}, 0)
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
	_, _, err := ctlcmd.Run(s.mockContext, []string{"stop", "other-snap.test-service"}, 0)
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
	_, _, err := ctlcmd.Run(s.mockContext, []string{"start", "test-snap.test-service"}, 0)
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
	_, _, err := ctlcmd.Run(s.mockContext, []string{"restart", "test-snap.test-service"}, 0)
	c.Check(err, NotNil)
	c.Check(err, ErrorMatches, "forced error")
	c.Assert(serviceChangeFuncCalled, Equals, true)
}

func (s *servicectlSuite) TestConflictingChange(c *C) {
	s.st.Lock()
	task := s.st.NewTask("link-snap", "conflicting task")
	snapsup := snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "test-snap",
			SnapID:   "test-snap-id-1",
			Revision: snap.R(1),
		},
	}
	task.Set("snap-setup", snapsup)
	chg := s.st.NewChange("conflicting change", "install change")
	chg.AddTask(task)
	s.st.Unlock()

	_, _, err := ctlcmd.Run(s.mockContext, []string{"start", "test-snap.test-service"}, 0)
	c.Check(err, NotNil)
	c.Check(err, ErrorMatches, `snap "test-snap" has "conflicting change" change in progress`)
}

func (s *servicectlSuite) TestQueuedCommands(c *C) {
	s.st.Lock()

	chg := s.st.NewChange("install change", "install change")
	installed, tts, err := snapstate.InstallMany(s.st, []string{"one", "two"}, 0)
	c.Assert(err, IsNil)
	c.Check(installed, DeepEquals, []string{"one", "two"})
	c.Assert(tts, HasLen, 2)
	c.Assert(tts[0].Tasks(), HasLen, 13)
	c.Assert(tts[1].Tasks(), HasLen, 13)
	chg.AddAll(tts[0])
	chg.AddAll(tts[1])

	s.st.Unlock()

	for _, ts := range tts {
		tsTasks := ts.Tasks()
		// assumes configure task is last
		task := tsTasks[len(tsTasks)-1]
		c.Assert(task.Kind(), Equals, "run-hook")
		setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "configure"}
		context, err := hookstate.NewContext(task, task.State(), setup, s.mockHandler, "")
		c.Assert(err, IsNil)

		_, _, err = ctlcmd.Run(context, []string{"stop", "test-snap.test-service"}, 0)
		c.Check(err, IsNil)
		_, _, err = ctlcmd.Run(context, []string{"start", "test-snap.test-service"}, 0)
		c.Check(err, IsNil)
		_, _, err = ctlcmd.Run(context, []string{"restart", "test-snap.test-service"}, 0)
		c.Check(err, IsNil)
	}

	s.st.Lock()
	defer s.st.Unlock()

	for i := 1; i <= 2; i++ {
		laneTasks := chg.LaneTasks(i)
		c.Assert(laneTasks, HasLen, 16)
		c.Check(laneTasks[12].Summary(), Matches, `Run configure hook of .* snap if present`)
		c.Check(laneTasks[13].Summary(), Equals, "stop of [test-snap.test-service]")
		c.Check(laneTasks[14].Summary(), Equals, "start of [test-snap.test-service]")
		c.Check(laneTasks[15].Summary(), Equals, "restart of [test-snap.test-service]")
	}
}

func (s *servicectlSuite) TestQueuedCommandsUpdateMany(c *C) {
	oldAutoAliases := snapstate.AutoAliases
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = oldAutoAliases }()

	s.st.Lock()

	chg := s.st.NewChange("update many change", "update change")
	installed, tts, err := snapstate.UpdateMany(context.TODO(), s.st, []string{"test-snap", "other-snap"}, 0)
	c.Assert(err, IsNil)
	sort.Strings(installed)
	c.Check(installed, DeepEquals, []string{"other-snap", "test-snap"})
	c.Assert(tts, HasLen, 2)
	c.Assert(tts[0].Tasks(), HasLen, 18)
	c.Assert(tts[1].Tasks(), HasLen, 18)
	chg.AddAll(tts[0])
	chg.AddAll(tts[1])

	s.st.Unlock()

	for _, ts := range tts {
		tsTasks := ts.Tasks()
		// assumes configure task is last
		task := tsTasks[len(tsTasks)-1]
		c.Assert(task.Kind(), Equals, "run-hook")
		setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "configure"}
		context, err := hookstate.NewContext(task, task.State(), setup, s.mockHandler, "")
		c.Assert(err, IsNil)

		_, _, err = ctlcmd.Run(context, []string{"stop", "test-snap.test-service"}, 0)
		c.Check(err, IsNil)
		_, _, err = ctlcmd.Run(context, []string{"start", "test-snap.test-service"}, 0)
		c.Check(err, IsNil)
		_, _, err = ctlcmd.Run(context, []string{"restart", "test-snap.test-service"}, 0)
		c.Check(err, IsNil)
	}

	s.st.Lock()
	defer s.st.Unlock()

	for i := 1; i <= 2; i++ {
		laneTasks := chg.LaneTasks(i)
		c.Assert(laneTasks, HasLen, 21)
		c.Check(laneTasks[17].Summary(), Matches, `Run configure hook of .* snap if present`)
		c.Check(laneTasks[18].Summary(), Equals, "stop of [test-snap.test-service]")
		c.Check(laneTasks[19].Summary(), Equals, "start of [test-snap.test-service]")
		c.Check(laneTasks[20].Summary(), Equals, "restart of [test-snap.test-service]")
	}
}

func (s *servicectlSuite) TestQueuedCommandsSingleLane(c *C) {
	s.st.Lock()

	chg := s.st.NewChange("install change", "install change")
	ts, err := snapstate.Install(s.st, "one", "", snap.R(1), 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), HasLen, 13)
	chg.AddAll(ts)

	s.st.Unlock()

	tsTasks := ts.Tasks()
	// assumes configure task is last
	task := tsTasks[len(tsTasks)-1]
	c.Assert(task.Kind(), Equals, "run-hook")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "configure"}
	context, err := hookstate.NewContext(task, task.State(), setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	_, _, err = ctlcmd.Run(context, []string{"stop", "test-snap.test-service"}, 0)
	c.Check(err, IsNil)
	_, _, err = ctlcmd.Run(context, []string{"start", "test-snap.test-service"}, 0)
	c.Check(err, IsNil)
	_, _, err = ctlcmd.Run(context, []string{"restart", "test-snap.test-service"}, 0)
	c.Check(err, IsNil)

	s.st.Lock()
	defer s.st.Unlock()

	laneTasks := chg.LaneTasks(0)
	c.Assert(laneTasks, HasLen, 16)
	c.Check(laneTasks[12].Summary(), Matches, `Run configure hook of .* snap if present`)
	c.Check(laneTasks[13].Summary(), Equals, "stop of [test-snap.test-service]")
	c.Check(laneTasks[14].Summary(), Equals, "start of [test-snap.test-service]")
	c.Check(laneTasks[15].Summary(), Equals, "restart of [test-snap.test-service]")
}
