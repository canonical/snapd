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

package snapstate_test

import (
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snaptest"
	. "gopkg.in/check.v1"
)

type handlersComponentsSuite struct {
	baseHandlerSuite
}

var _ = Suite(&handlersComponentsSuite{})

func (s *handlersComponentsSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)
	s.AddCleanup(snapstatetest.MockDeviceModel(DefaultModel()))
}

func (s *handlersComponentsSuite) TestComponentSetupTaskFirstTask(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// make a new task which will be the component-setup-task for
	// other tasks and write a ComponentSetup to it
	t := s.state.NewTask("prepare-component", "test")
	const snapName = "mysnap"
	const compName = "mycomp"
	compRev := snap.R(7)
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	compsup := snapstate.NewComponentSetup(csi, snap.KernelModulesComponent, "")
	t.Set("component-setup", compsup)
	s.state.NewChange("sample", "...").AddTask(t)

	// Check that the returned task contains the data
	setupTask, err := snapstate.ComponentSetupTask(t)
	c.Assert(err, IsNil)
	var newcompsup snapstate.ComponentSetup
	err = setupTask.Get("component-setup", &newcompsup)
	c.Assert(err, IsNil)
}

func (s *handlersComponentsSuite) TestComponentSetupTaskLaterTask(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	t := s.state.NewTask("prepare-component", "test")

	const snapName = "mysnap"
	const compName = "mycomp"
	compRev := snap.R(7)
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	compsup := snapstate.NewComponentSetup(csi, snap.KernelModulesComponent, "")
	// setup component-setup for the first task
	t.Set("component-setup", compsup)

	// make a new task and reference the first one in component-setup-task
	t2 := s.state.NewTask("next-task-comp", "test2")
	t2.Set("component-setup-task", t.ID())

	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)
	chg.AddTask(t2)

	// Check that the returned task contains the data
	setupTask, err := snapstate.ComponentSetupTask(t2)
	c.Assert(err, IsNil)
	var newcompsup snapstate.ComponentSetup
	err = setupTask.Get("component-setup", &newcompsup)
	c.Assert(err, IsNil)
	// and is the expected task
	c.Assert(setupTask.ID(), Equals, t.ID())
}

func (s *handlersComponentsSuite) TestComponentSetupsForTaskComponentInstall(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	t := s.state.NewTask("prepare-component", "test")

	const snapName = "mysnap"
	const compName = "mycomp"
	compRev := snap.R(7)
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	compsup := snapstate.NewComponentSetup(csi, snap.KernelModulesComponent, "")
	// setup component-setup for the first task
	t.Set("component-setup", compsup)
	t.Set("snap-setup", snapstate.SnapSetup{
		Version: "1.0",
	})

	// make a new task and reference the first one in component-setup-task
	t2 := s.state.NewTask("next-task-comp", "test2")
	t2.Set("component-setup-task", t.ID())
	t2.Set("snap-setup-task", t.ID())

	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)
	chg.AddTask(t2)

	t2.SetStatus(state.DoingStatus)
	t.SetStatus(state.DoingStatus)

	setups, err := snapstate.ComponentSetupsForTask(t2)
	c.Assert(err, IsNil)

	c.Assert(setups, HasLen, 1)
	c.Check(setups[0], DeepEquals, compsup)

	setups, err = snapstate.ComponentSetupsForTask(t)
	c.Assert(err, IsNil)

	c.Assert(setups, HasLen, 1)
	c.Check(setups[0], DeepEquals, compsup)
}

func (s *handlersComponentsSuite) TestComponentSetupsForTaskSnapWithoutComponents(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	t := s.state.NewTask("prepare-component", "test")

	t.Set("snap-setup", snapstate.SnapSetup{
		Version: "1.0",
	})

	// make a new task and reference the first one in component-setup-task
	t2 := s.state.NewTask("next-task-comp", "test2")
	t2.Set("snap-setup-task", t.ID())

	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)
	chg.AddTask(t2)

	setups, err := snapstate.ComponentSetupsForTask(t2)
	c.Assert(err, IsNil)

	c.Check(setups, HasLen, 0)

	setups, err = snapstate.ComponentSetupsForTask(t)
	c.Assert(err, IsNil)

	c.Check(setups, HasLen, 0)
}

func (s *handlersComponentsSuite) TestComponentInfoFromComponentSetup(c *C) {
	s.testComponentInfoFromComponentSetup(c, "key")
}

func (s *handlersComponentsSuite) TestComponentInfoFromComponentSetupInstance(c *C) {
	s.testComponentInfoFromComponentSetup(c, "")
}

func (s *handlersComponentsSuite) testComponentInfoFromComponentSetup(c *C, instanceKey string) {
	const snapName = "mysnap"
	const compName = "mycomp"
	compRev := snap.R(7)
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)

	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: snapName,
			Revision: snap.R(2),
		},
		InstanceKey: instanceKey,
		Components: map[string]*snap.Component{
			"mycomp": {
				Type: snap.TestComponent,
				Name: compName,
			},
		},
	}

	realCompInfo := snaptest.MockComponent(c, "component: mysnap+mycomp\ntype: test\nversion: 1", info, *csi)
	_ = realCompInfo

	compsup := snapstate.NewComponentSetup(csi, snap.KernelModulesComponent, "")

	compInfo, err := snapstate.ComponentInfoFromComponentSetup(compsup, info)
	c.Assert(err, IsNil)

	c.Check(compInfo.Component, Equals, cref)
	c.Check(compInfo.Type, Equals, snap.TestComponent)
	c.Check(compInfo.ComponentSideInfo.Revision, Equals, compRev)
}
