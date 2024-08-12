// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	. "gopkg.in/check.v1"
)

type mountCompSnapSuite struct {
	baseHandlerSuite
}

var _ = Suite(&mountCompSnapSuite{})

func (s *mountCompSnapSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)
	s.AddCleanup(snapstatetest.MockDeviceModel(DefaultModel()))
}

func (s *mountCompSnapSuite) TestDoMountComponent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ci, compPath := createTestComponent(c, snapName, compName, si)
	ssu := createTestSnapSetup(si, snapstate.Flags{})
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string, snapInfo *snap.Info, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error) {
		return ci, nil
	}))

	s.state.Lock()

	t := s.state.NewTask("mount-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, snap.TestComponent, compPath))
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	c.Check(chg.Err(), IsNil)
	s.state.Unlock()

	// Ensure backend calls have happened with the expected data
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:                "setup-component",
			containerName:     "mysnap+mycomp",
			containerFileName: "mysnap+mycomp_7.comp",
		},
	})
	// File not removed
	c.Assert(osutil.FileExists(compPath), Equals, true)
}

func (s *mountCompSnapSuite) TestDoMountComponentFailsUnassertedComponentAssertedSnap(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.Revision{}
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ci, compPath := createTestComponent(c, snapName, compName, si)
	ssu := createTestSnapSetup(si, snapstate.Flags{})
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string, snapInfo *snap.Info, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error) {
		return ci, nil
	}))

	s.state.Lock()
	defer s.state.Unlock()

	t := s.state.NewTask("mount-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, snap.TestComponent, compPath))
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Check(chg.Err().Error(), Equals, "cannot perform the following tasks:\n"+
		"- task desc (cannot mix asserted snap and unasserted components)")
}

func (s *mountCompSnapSuite) TestDoMountComponentFailsAssertedComponentUnassertedSnap(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(-1)
	compRev := snap.R(7)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ci, compPath := createTestComponent(c, snapName, compName, si)
	ssu := createTestSnapSetup(si, snapstate.Flags{})
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string, snapInfo *snap.Info, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error) {
		return ci, nil
	}))

	s.state.Lock()
	defer s.state.Unlock()

	t := s.state.NewTask("mount-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, snap.TestComponent, compPath))
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Check(chg.Err().Error(), Equals, "cannot perform the following tasks:\n"+
		"- task desc (cannot mix unasserted snap and asserted components)")
}

func (s *mountCompSnapSuite) TestDoUndoMountComponent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ci, compPath := createTestComponent(c, snapName, compName, si)
	ssu := createTestSnapSetup(si, snapstate.Flags{})
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string, snapInfo *snap.Info, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error) {
		return ci, nil
	}))

	s.state.Lock()
	defer s.state.Unlock()

	t := s.state.NewTask("mount-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, snap.TestComponent, compPath))
	t.Set("snap-setup", ssu)

	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(chg.Err().Error(), Equals, "cannot perform the following tasks:\n"+
		"- provoking total undo (error out)")

	// ensure undo was called the right way
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:                "setup-component",
			containerName:     "mysnap+mycomp",
			containerFileName: "mysnap+mycomp_7.comp",
		},
		{
			op:                "undo-setup-component",
			containerName:     "mysnap+mycomp",
			containerFileName: "mysnap+mycomp_7.comp",
		},
		{
			op:                "remove-component-dir",
			containerName:     "mysnap+mycomp",
			containerFileName: "mysnap+mycomp_7.comp",
		},
	})
}

func (s *mountCompSnapSuite) TestDoMountComponentSetupFails(c *C) {
	const snapName = "mysnap"
	// fakeSnappyBackend.SetupComponent will fail for this component name
	const compName = "broken"
	snapRev := snap.R(1)
	compRev := snap.R(7)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ci, compPath := createTestComponent(c, snapName, compName, si)
	ssu := createTestSnapSetup(si, snapstate.Flags{})
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string, snapInfo *snap.Info, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error) {
		return ci, nil
	}))

	s.state.Lock()
	defer s.state.Unlock()

	t := s.state.NewTask("mount-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, snap.TestComponent, compPath))
	t.Set("snap-setup", ssu)

	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(chg.Err().Error(), Equals, "cannot perform the following tasks:\n"+
		"- task desc (cannot set-up component \"mysnap+broken\")")

	// ensure undo was called the right way
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:                "setup-component",
			containerName:     "mysnap+broken",
			containerFileName: "mysnap+broken_7.comp",
		},
		{
			op:                "remove-component-dir",
			containerName:     "mysnap+broken",
			containerFileName: "mysnap+broken_7.comp",
		},
	})
}

func (s *mountCompSnapSuite) TestDoUndoMountComponentFails(c *C) {
	const snapName = "mysnap"
	// fakeSnappyBackend.UndoSetupComponent will fail for this component name
	const compName = "brokenundo"
	snapRev := snap.R(1)
	compRev := snap.R(7)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ci, compPath := createTestComponent(c, snapName, compName, si)
	ssu := createTestSnapSetup(si, snapstate.Flags{})
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string, snapInfo *snap.Info, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error) {
		return ci, nil
	}))

	s.state.Lock()
	defer s.state.Unlock()

	t := s.state.NewTask("mount-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, snap.TestComponent, compPath))
	t.Set("snap-setup", ssu)

	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(chg.Err().Error(), Equals, "cannot perform the following tasks:\n"+
		"- task desc (cannot undo set-up of component \"mysnap+brokenundo\")\n"+
		"- provoking total undo (error out)")

	// ensure undo was called the right way
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:                "setup-component",
			containerName:     "mysnap+brokenundo",
			containerFileName: "mysnap+brokenundo_7.comp",
		},
		{
			op:                "undo-setup-component",
			containerName:     "mysnap+brokenundo",
			containerFileName: "mysnap+brokenundo_7.comp",
		},
	})
}

func (s *mountCompSnapSuite) TestDoMountComponentMountFails(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ci, compPath := createTestComponent(c, snapName, compName, si)
	ssu := createTestSnapSetup(si, snapstate.Flags{})
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string, snapInfo *snap.Info, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error) {
		return ci, fmt.Errorf("cannot read component")
	}))

	s.state.Lock()
	defer s.state.Unlock()

	t := s.state.NewTask("mount-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, snap.TestComponent, compPath))
	t.Set("snap-setup", ssu)

	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(chg.Err().Error(), Equals, "cannot perform the following tasks:\n"+
		"- task desc (expected component \"mysnap+mycomp\" revision 7 "+
		"to be mounted but is not: cannot read component)")

	// ensure undo was called the right way
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:                "setup-component",
			containerName:     "mysnap+mycomp",
			containerFileName: "mysnap+mycomp_7.comp",
		},
		{
			op:                "undo-setup-component",
			containerName:     "mysnap+mycomp",
			containerFileName: "mysnap+mycomp_7.comp",
		},
		{
			op:                "remove-component-dir",
			containerName:     "mysnap+mycomp",
			containerFileName: "mysnap+mycomp_7.comp",
		},
	})
}
