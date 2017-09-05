// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

type stopSuite struct {
	mockContext *hookstate.Context
	mockHandler *hooktest.MockHandler

	restore func()
}

var _ = Suite(&stopSuite{})

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

func mockServiceControlFunc(testServiceControlInputs func(appInfos []*snap.AppInfo, inst *servicectl.AppInstruction)) {
	ctlcmd.SetServiceControlFunc(func(st *state.State, appInfos []*snap.AppInfo, inst *servicectl.AppInstruction) (*state.Change, error) {
		testServiceControlInputs(appInfos, inst)
		st.Lock()
		defer st.Unlock()
		chg := st.NewChange("service-control", "")
		chg.SetStatus(state.DoneStatus)
		return chg, nil
	})
}

func (s *stopSuite) SetUpTest(c *C) {
	oldRoot := dirs.GlobalRootDir
	dirs.SetRootDir(c.MkDir())
	oldServiceCtlFunc := ctlcmd.GetServiceControlFunc()
	s.restore = func() {
		dirs.SetRootDir(oldRoot)
		ctlcmd.SetServiceControlFunc(oldServiceCtlFunc)
	}

	s.mockHandler = hooktest.NewMockHandler()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	// mock installed snap
	info := snaptest.MockSnap(c, string(testSnapYaml), "", &snap.SideInfo{
		Revision: snap.R(1),
	})

	snapstate.Set(st, info.Name(), &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{
				RealName: info.Name(),
				Revision: info.Revision,
				SnapID:   "test-snap-id",
			},
		},
		Current: info.Revision,
	})

	task := st.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}

	var err error
	s.mockContext, err = hookstate.NewContext(task, task.State(), setup, s.mockHandler, "")
	c.Assert(err, IsNil)
}

func (s *stopSuite) TearDownTest(c *C) {
	s.restore()
}

func (s *stopSuite) TestStopCommand(c *C) {
	mockServiceControlFunc(func(appInfos []*snap.AppInfo, inst *servicectl.AppInstruction) {
		c.Assert(appInfos, HasLen, 1)
		c.Assert(appInfos[0].Name, Equals, "test-service")
		c.Assert(inst, DeepEquals, &servicectl.AppInstruction{
			Action: "stop",
			Names:  []string{"test-snap.test-service"},
			StopOptions: client.StopOptions{
				Disable: false,
			},
		},
		)
	})
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"stop", "test-snap.test-service"})
	c.Check(err, IsNil)
	c.Check(string(stderr), Equals, "")
	c.Check(string(stdout), Equals, "")
}
