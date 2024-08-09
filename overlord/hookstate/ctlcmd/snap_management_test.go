// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"context"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type installSuite struct {
	testutil.BaseTest
	st          *state.State
	chg         *state.Change
	mockContext *hookstate.Context
	mockHandler *hooktest.MockHandler
}

var _ = Suite(&installSuite{})

const snapWithCompsYaml = `name: test-snap
version: 1.0
summary: test-snap
components:
  comp1:
    type: test
  comp2:
    type: test
`

func (s *installSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	oldRoot := dirs.GlobalRootDir
	dirs.SetRootDir(c.MkDir())

	s.BaseTest.AddCleanup(func() {
		dirs.SetRootDir(oldRoot)
	})
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.mockHandler = hooktest.NewMockHandler()

	s.st = state.New(nil)
	s.st.Lock()
	defer s.st.Unlock()

	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.st, repo)

	// mock installed snaps
	info1 := snaptest.MockSnapCurrent(c, string(snapWithCompsYaml), &snap.SideInfo{
		Revision: snap.R(1),
	})
	snapstate.Set(s.st, info1.InstanceName(), &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{
				RealName: info1.SnapName(),
				Revision: info1.Revision,
				SnapID:   "test-snap-id",
			},
		}),
		Current: info1.Revision,
	})

	s.chg = s.st.NewChange("install change", "install change")
	task := s.st.NewTask("test-task", "my test task")
	s.chg.AddTask(task)
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}

	var err error
	s.mockContext, err = hookstate.NewContext(task, task.State(), setup, s.mockHandler, "")
	c.Assert(err, IsNil)
}

func (s *installSuite) testMngmtCommand(c *C, cmd string) {
	s.st.Lock()
	task := s.st.NewTask("queued", "queued task")
	s.st.Unlock()

	switch cmd {
	case "install":
		ctlcmd.MockSnapstateInstallComponentsFunc(func(ctx context.Context, st *state.State, names []string, info *snap.Info, opts snapstate.Options) ([]*state.TaskSet, error) {
			c.Check(names, DeepEquals, []string{"comp1", "comp2"})
			c.Check(opts, DeepEquals, snapstate.Options{ExpectOneSnap: true,
				FromChange: s.mockContext.ChangeID()})
			var ts state.TaskSet
			ts.AddTask(task)
			return []*state.TaskSet{&ts}, nil
		})
	case "remove":
		ctlcmd.MockSnapstateRemoveComponentsFunc(func(st *state.State, snapName string, compNames []string, opts snapstate.RemoveComponentsOpts) ([]*state.TaskSet, error) {
			c.Check(compNames, DeepEquals, []string{"comp1", "comp2"})
			c.Check(opts, DeepEquals,
				snapstate.RemoveComponentsOpts{RefreshProfile: true,
					FromChange: s.mockContext.ChangeID()})
			var ts state.TaskSet
			ts.AddTask(task)
			return []*state.TaskSet{&ts}, nil
		})
	}

	_, _, err := ctlcmd.Run(s.mockContext, []string{cmd, "test-snap+comp1", "+comp2"}, 0)
	c.Assert(err, IsNil)
	s.st.Lock()
	// one is the task added in SetUpTest, the other is the queued task
	c.Assert(len(s.chg.Tasks()), Equals, 2)
	s.st.Unlock()
}

func (s *installSuite) TestInstallCommand(c *C) {
	s.testMngmtCommand(c, "install")
}

func (s *installSuite) TestRemoveCommand(c *C) {
	s.testMngmtCommand(c, "remove")
}

func (s *installSuite) testEphemeralMngmtCommand(c *C, cmd string) {
	s.st.Lock()
	task := s.st.NewTask("test", "test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1)}
	s.st.Unlock()

	switch cmd {
	case "install":
		ctlcmd.MockSnapstateInstallComponentsFunc(func(ctx context.Context, st *state.State, names []string, info *snap.Info, opts snapstate.Options) ([]*state.TaskSet, error) {
			c.Check(names, DeepEquals,
				[]string{"comp1", "comp2", "comp3", "comp4", "comp5", "comp6"})
			c.Check(opts, DeepEquals, snapstate.Options{ExpectOneSnap: true})
			var ts state.TaskSet
			ts.AddTask(task)
			return []*state.TaskSet{&ts}, nil
		})
	case "remove":
		ctlcmd.MockSnapstateRemoveComponentsFunc(func(st *state.State, snapName string, compNames []string, opts snapstate.RemoveComponentsOpts) ([]*state.TaskSet, error) {
			c.Check(compNames, DeepEquals,
				[]string{"comp1", "comp2", "comp3", "comp4", "comp5", "comp6"})
			c.Check(opts, DeepEquals,
				snapstate.RemoveComponentsOpts{RefreshProfile: true})
			var ts state.TaskSet
			ts.AddTask(task)
			return []*state.TaskSet{&ts}, nil
		})
	}

	var err error
	s.mockContext, err = hookstate.NewContext(nil, s.st, setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	chann := make(chan error)
	go func() {
		_, _, err := ctlcmd.Run(s.mockContext,
			[]string{cmd, "test-snap+comp1", "test-snap+comp2+comp3",
				"+comp4", "+comp5+comp6"}, 0)
		chann <- err
	}()

	// Wait for the change to be created and assigned to task
	var chg *state.Change
	for i := 0; i < 50; i++ {
		s.st.Lock()
		chg = task.Change()
		s.st.Unlock()
		if chg != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if chg == nil {
		c.Error("change not assigned to ephemeral snapctl command")
	}

	s.st.Lock()
	chg.SetStatus(state.DoneStatus)
	s.st.Unlock()

	err = <-chann
	c.Assert(err, IsNil)
}

func (s *installSuite) TestEphemeralInstallCommand(c *C) {
	s.testEphemeralMngmtCommand(c, "install")
}

func (s *installSuite) TestEphemeralRemoveCommand(c *C) {
	s.testEphemeralMngmtCommand(c, "remove")
}

func (s *installSuite) testMgmntCommandOtherSnap(c *C, cmd string) {
	_, _, err := ctlcmd.Run(s.mockContext, []string{cmd, "+comp1", "other-snap+comp2"}, 0)
	c.Assert(err, ErrorMatches, "cannot install snaps using snapctl")
}

func (s *installSuite) TestInstallCommandOtherSnap(c *C) {
	s.testMgmntCommandOtherSnap(c, "install")
}

func (s *installSuite) TestRemoveCommandOtherSnap(c *C) {
	s.testMgmntCommandOtherSnap(c, "remove")
}

func (s *installSuite) testMgmntCommandBadCompName(c *C, cmd string) {
	_, _, err := ctlcmd.Run(s.mockContext, []string{cmd, "+comp_1"}, 0)
	c.Assert(err, ErrorMatches, `invalid snap name: "comp_1"`)
}

func (s *installSuite) TestInstallCommandBadCompName(c *C) {
	s.testMgmntCommandBadCompName(c, "install")
}

func (s *installSuite) TestRemoveCommandBadCompName(c *C) {
	s.testMgmntCommandBadCompName(c, "remove")
}
