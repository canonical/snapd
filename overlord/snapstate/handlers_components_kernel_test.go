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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
)

type setupKernelComponentsSuite struct {
	baseHandlerSuite
}

var _ = Suite(&setupKernelComponentsSuite{})

func (s *setupKernelComponentsSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)
	s.AddCleanup(snapstatetest.MockDeviceModel(DefaultModel()))
}

func (s *setupKernelComponentsSuite) TestSetupKernelModules(c *C) {
	s.testSetupKernelModules(c, "mykernel", "")
}

func (s *setupKernelComponentsSuite) TestSetupKernelModulesFails(c *C) {
	s.testSetupKernelModules(c, "mykernel+broken",
		"cannot perform the following tasks:\n- test kernel modules (cannot set-up kernel-modules for mykernel+broken)")
}

func (s *setupKernelComponentsSuite) testSetupKernelModules(c *C, snapName, errStr string) {
	snapRev := snap.R(77)
	const compName = "kcomp"

	s.state.Lock()

	// add some components to the state
	csi1 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), snap.R(1))
	csi2 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, "other-comp"), snap.R(33))
	cs1 := sequence.NewComponentState(csi1, snap.KernelModulesComponent)
	cs2 := sequence.NewComponentState(csi2, snap.KernelModulesComponent)
	setStateWithComponents(s.state, snapName, snapRev, []*sequence.ComponentState{cs1, cs2})

	t := s.state.NewTask("prepare-kernel-modules-components", "test kernel modules")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapName,
			Revision: snapRev,
		},
	})
	compRev := snap.R(7)
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup",
		snapstate.NewComponentSetup(csi, snap.KernelModulesComponent, ""))
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	if errStr == "" {
		c.Check(chg.Err(), IsNil)
		c.Check(t.Status(), Equals, state.DoneStatus)
	} else {
		c.Check(chg.Err().Error(), Equals, errStr)
		c.Check(t.Status(), Equals, state.ErrorStatus)
	}
	s.state.Unlock()

	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:             "setup-kernel-modules-components",
			compsToInstall: []*snap.ComponentSideInfo{csi},
			currentComps:   []*snap.ComponentSideInfo{csi1, csi2},
		},
	})
}

func (s *setupKernelComponentsSuite) TestRemoveKernelModulesSetup(c *C) {
	s.testRemoveKernelModulesSetup(c, "mykernel", "")
}

func (s *setupKernelComponentsSuite) TestRemoveKernelModulesSetupFails(c *C) {
	s.testRemoveKernelModulesSetup(c, "mykernel+reverterr",
		"(?s).*cannot remove set-up of kernel-modules for mykernel\\+reverterr.*")
}

func (s *setupKernelComponentsSuite) testRemoveKernelModulesSetup(c *C, snapName, errStr string) {
	snapRev := snap.R(77)
	const compName = "kcomp"

	s.state.Lock()

	// add some components to the state
	csi1 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), snap.R(1))
	csi2 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, "other-comp"), snap.R(33))
	cs1 := sequence.NewComponentState(csi1, snap.KernelModulesComponent)
	cs2 := sequence.NewComponentState(csi2, snap.KernelModulesComponent)
	setStateWithComponents(s.state, snapName, snapRev, []*sequence.ComponentState{cs1, cs2})

	t := s.state.NewTask("prepare-kernel-modules-components", "test kernel modules")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapName,
			Revision: snapRev,
		},
	})
	compRev := snap.R(7)
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup",
		snapstate.NewComponentSetup(csi, snap.KernelModulesComponent, ""))
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	if errStr == "" {
		c.Check(chg.Err(), ErrorMatches, "(?s)cannot perform the following tasks:\n"+
			"- provoking undo \\(error out\\)")
		c.Check(t.Status(), Equals, state.UndoneStatus)
	} else {
		c.Check(chg.Err(), ErrorMatches, "(?s).* provoking undo \\(error out\\).*")
		c.Check(chg.Err(), ErrorMatches, errStr)
		c.Check(t.Status(), Equals, state.ErrorStatus)
	}

	s.state.Unlock()

	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:             "setup-kernel-modules-components",
			compsToInstall: []*snap.ComponentSideInfo{csi},
			currentComps:   []*snap.ComponentSideInfo{csi1, csi2},
		},
		{
			op:            "remove-kernel-modules-components-setup",
			compsToRemove: []*snap.ComponentSideInfo{csi},
			finalComps:    []*snap.ComponentSideInfo{csi1, csi2},
		},
	})
}
