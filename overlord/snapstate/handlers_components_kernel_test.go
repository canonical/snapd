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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
)

type setupKernelComponentsSuite struct {
	baseHandlerSuite

	restartRequested []restart.RestartType
	restartReasons   []restart.RestartReason
}

var _ = Suite(&setupKernelComponentsSuite{})

func (s *setupKernelComponentsSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)
	s.AddCleanup(snapstatetest.MockDeviceModel(DefaultModel()))

	s.restartRequested = nil
	s.restartReasons = nil
	s.state.Lock()
	_, err := restart.Manager(s.state, "boot-id-0", snapstatetest.MockRestartHandler(func(t restart.RestartType, reason restart.RestartReason) {
		s.restartRequested = append(s.restartRequested, t)
		s.restartReasons = append(s.restartReasons, reason)
	}))
	s.state.Unlock()
	c.Assert(err, IsNil)
}

func (s *setupKernelComponentsSuite) TestSetupKernelModules(c *C) {
	s.testSetupKernelModules(c, "mykernel", "")
}

func (s *setupKernelComponentsSuite) TestSetupKernelModulesFails(c *C) {
	s.testSetupKernelModules(c, "mykernel",
		"cannot perform the following tasks:\n- test kernel modules (cannot set-up kernel-modules for mykernel)")
}

func (s *setupKernelComponentsSuite) testSetupKernelModules(c *C, snapName, errStr string) {
	snapRev := snap.R(77)
	const compName = "kcomp"

	if errStr != "" {
		s.fakeBackend.maybeInjectErr = func(op *fakeOp) error {
			if op.op == "prepare-kernel-modules-components" {
				return fmt.Errorf("cannot set-up kernel-modules for %s", snapName)
			}
			return nil
		}
	}

	s.state.Lock()

	// add some components to the state
	csi1 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), snap.R(1))
	csi2 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, "other-comp"), snap.R(33))
	cs1 := sequence.NewComponentState(csi1, snap.KernelModulesComponent)
	cs2 := sequence.NewComponentState(csi2, snap.KernelModulesComponent)

	t := s.state.NewTask("prepare-kernel-modules-components", "test kernel modules")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapName,
			Revision: snapRev,
		},
		PreUpdateKernelModuleComponents: []*snap.ComponentSideInfo{cs1.SideInfo, cs2.SideInfo},
	})
	compRev := snap.R(7)
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup",
		snapstate.NewComponentSetup(csi, snap.KernelModulesComponent, ""))

	// set the state to include the new component, since this task will run
	// after the component has been linked
	cs := sequence.NewComponentState(csi, snap.KernelModulesComponent)
	setStateWithComponents(s.state, snapName, snapRev, []*sequence.ComponentState{cs1, cs2, cs})

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
			op:           "prepare-kernel-modules-components",
			finalComps:   []*snap.ComponentSideInfo{cs1.SideInfo, cs2.SideInfo, csi},
			currentComps: []*snap.ComponentSideInfo{csi1, csi2},
		},
	})
}

func (s *setupKernelComponentsSuite) TestSetupKernelModulesRebootSameKernelRevision(c *C) {
	// the kernel revision does not change, only the kernel-modules
	// components do, so the reboot is attributed to them
	s.testSetupKernelModulesReboot(c, snap.R(77), restart.RestartKernelModulesUpdate)
}

func (s *setupKernelComponentsSuite) TestSetupKernelModulesRebootNewKernelRevision(c *C) {
	// the kernel revision changed, so this is a regular kernel update
	s.testSetupKernelModulesReboot(c, snap.R(76), restart.RestartKernelUpdate)
}

func (s *setupKernelComponentsSuite) TestSetupKernelModulesRebootUnknownPreviousKernelRevision(c *C) {
	// without a recorded previous revision we fall back to the kernel
	// update reason
	s.testSetupKernelModulesReboot(c, snap.R(0), restart.RestartKernelUpdate)
}

func (s *setupKernelComponentsSuite) testSetupKernelModulesReboot(c *C, prevKernelRev snap.Revision, expectedReason restart.RestartReason) {
	restore := release.MockOnClassic(false)
	defer restore()

	// the fake backend reads this snap as a kernel and requests a reboot
	// from MaybeSetNextBoot for kernels
	const snapName = "kernel-snap-with-components"
	snapRev := snap.R(77)
	s.fakeBackend.linkSnapMaybeReboot = true

	s.state.Lock()

	csi := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, "kcomp"), snap.R(7))
	cs := sequence.NewComponentState(csi, snap.KernelModulesComponent)

	t := s.state.NewTask("prepare-kernel-modules-components", "test kernel modules")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapName,
			Revision: snapRev,
		},
		Type: snap.TypeKernel,
	})
	t.Set("component-setup", snapstate.NewComponentSetup(csi, snap.KernelModulesComponent, ""))
	t.Set("set-next-boot", true)
	if !prevKernelRev.Unset() {
		// as recorded by prepare-kernel-snap on the snap-setup task
		t.Set("previous-kernel-rev", prevKernelRev)
	}

	setStateWithComponents(s.state, snapName, snapRev, []*sequence.ComponentState{cs})

	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(chg.Err(), IsNil)
	c.Check(t.Status(), Equals, state.WaitStatus)
	c.Check(s.restartRequested, DeepEquals, []restart.RestartType{restart.RestartSystem})
	c.Check(s.restartReasons, DeepEquals, []restart.RestartReason{expectedReason})
}

func (s *setupKernelComponentsSuite) TestRemoveKernelModulesSetup(c *C) {
	s.testRemoveKernelModulesSetup(c, "mykernel", "")
}

func (s *setupKernelComponentsSuite) TestRemoveKernelModulesSetupFails(c *C) {
	s.testRemoveKernelModulesSetup(c, "mykernel",
		"(?s).*cannot set-up kernel-modules for mykernel.*")
}

func (s *setupKernelComponentsSuite) testRemoveKernelModulesSetup(c *C, snapName, errStr string) {
	snapRev := snap.R(77)
	const compName = "kcomp"

	if errStr != "" {
		var count int
		s.fakeBackend.maybeInjectErr = func(op *fakeOp) error {
			if op.op == "prepare-kernel-modules-components" {
				count++
				if count == 2 {
					return fmt.Errorf("cannot set-up kernel-modules for %s", snapName)
				}
			}
			return nil
		}
	}

	s.state.Lock()

	// add some components to the state
	csi1 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), snap.R(1))
	csi2 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, "other-comp"), snap.R(33))
	cs1 := sequence.NewComponentState(csi1, snap.KernelModulesComponent)
	cs2 := sequence.NewComponentState(csi2, snap.KernelModulesComponent)

	t := s.state.NewTask("prepare-kernel-modules-components", "test kernel modules")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapName,
			Revision: snapRev,
		},
		PreUpdateKernelModuleComponents: []*snap.ComponentSideInfo{cs1.SideInfo, cs2.SideInfo},
	})

	compRev := snap.R(7)
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup",
		snapstate.NewComponentSetup(csi, snap.KernelModulesComponent, ""))

	cs := sequence.NewComponentState(csi, snap.KernelModulesComponent)
	setStateWithComponents(s.state, snapName, snapRev, []*sequence.ComponentState{cs1, cs2, cs})

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
			op:           "prepare-kernel-modules-components",
			currentComps: []*snap.ComponentSideInfo{csi1, csi2},
			finalComps:   []*snap.ComponentSideInfo{csi1, csi2, csi},
		},
		{
			op:           "prepare-kernel-modules-components",
			finalComps:   []*snap.ComponentSideInfo{csi1, csi2},
			currentComps: []*snap.ComponentSideInfo{csi1, csi2, csi},
		},
	})
}
