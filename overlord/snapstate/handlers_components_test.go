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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
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
