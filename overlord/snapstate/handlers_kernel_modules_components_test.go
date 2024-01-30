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
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
)

type kernelModulesCompSnapSuite struct {
	baseHandlerSuite
}

var _ = Suite(&kernelModulesCompSnapSuite{})

func (s *kernelModulesCompSnapSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)
	s.AddCleanup(snapstatetest.MockDeviceModel(DefaultModel()))

	var err error
	taskRunTime, err := time.Parse(time.RFC3339, "2024-01-01T00:00:00Z")
	c.Assert(err, IsNil)
	s.AddCleanup(snapstate.MockTimeNow(func() time.Time {
		return taskRunTime
	}))
}

func (s *kernelModulesCompSnapSuite) TestDoSetupKernelModulesComponent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)
	ci, compPath := createTestComponent(c, snapName, compName)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string) (*snap.ComponentInfo, error) {
		return ci, nil
	}))
	// Create file so kernel version can be found
	c.Assert(os.MkdirAll(si.MountDir(), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(
		si.MountDir(), "System.map-5.15.0-78-generic"), []byte{}, 0644), IsNil)

	s.state.Lock()

	t := s.state.NewTask("setup-kernel-modules-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup",
		snapstate.NewComponentSetup(csi, snap.TestComponent, compPath))
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	c.Check(chg.Err(), IsNil)
	// State of task is as expected
	c.Assert(t.Status(), Equals, state.DoneStatus)
	s.state.Unlock()

	// Ensure backend calls have happened with the expected data
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:            "setup-kernel-modules-component",
			compMountDir:  filepath.Join(dirs.SnapMountDir, "mysnap/components/1/mycomp"),
			compMountFile: filepath.Join(dirs.GlobalRootDir, "var/lib/snapd/snaps/mysnap+mycomp_7.comp"),
			cref:          cref,
			kernelVersion: "5.15.0-78-generic",
		},
	})
}

func (s *kernelModulesCompSnapSuite) TestDoSetupKernelModulesComponentNoKernVersion(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)
	ci, compPath := createTestComponent(c, snapName, compName)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string) (*snap.ComponentInfo, error) {
		return ci, nil
	}))

	s.state.Lock()

	t := s.state.NewTask("setup-kernel-modules-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup",
		snapstate.NewComponentSetup(csi, snap.TestComponent, compPath))
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	c.Check(chg.Err(), ErrorMatches, "cannot perform the following tasks:\n"+
		`\- task desc \(unexpected number of matches \(0\) for glob .*System.map-\*\)`)
	c.Assert(t.Status(), Equals, state.ErrorStatus)
	s.state.Unlock()

	c.Check(len(s.fakeBackend.ops), Equals, 0)
}

func (s *kernelModulesCompSnapSuite) TestDoThenUndoSetupKernelModulesComponent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)
	ci, compPath := createTestComponent(c, snapName, compName)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string) (*snap.ComponentInfo, error) {
		return ci, nil
	}))
	// Create file so kernel version can be found
	c.Assert(os.MkdirAll(si.MountDir(), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(
		si.MountDir(), "System.map-5.15.0-78-generic"), []byte{}, 0644), IsNil)

	s.state.Lock()

	t := s.state.NewTask("setup-kernel-modules-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup",
		snapstate.NewComponentSetup(csi, snap.TestComponent, compPath))
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking undo setup")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(chg.Err().Error(), Equals, "cannot perform the following tasks:\n"+
		"- provoking undo setup (error out)")
	// State of task is as expected
	c.Check(t.Status(), Equals, state.UndoneStatus)
	s.state.Unlock()

	// Ensure backend calls have happened with the expected data
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:            "setup-kernel-modules-component",
			compMountDir:  filepath.Join(dirs.SnapMountDir, "mysnap/components/1/mycomp"),
			compMountFile: filepath.Join(dirs.GlobalRootDir, "var/lib/snapd/snaps/mysnap+mycomp_7.comp"),
			cref:          cref,
			kernelVersion: "5.15.0-78-generic",
		},
		{
			op:            "undo-setup-kernel-modules-component",
			compMountDir:  filepath.Join(dirs.SnapMountDir, "mysnap/components/1/mycomp"),
			compMountFile: filepath.Join(dirs.GlobalRootDir, "var/lib/snapd/snaps/mysnap+mycomp_7.comp"),
			cref:          cref,
			kernelVersion: "5.15.0-78-generic",
		},
	})
}

func (s *kernelModulesCompSnapSuite) TestDoThenUndoSetupKernelModulesComponentInstalled(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(2)
	compRev := snap.R(7)
	ci, compPath := createTestComponent(c, snapName, compName)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string) (*snap.ComponentInfo, error) {
		return ci, nil
	}))
	// Create file so kernel version can be found
	c.Assert(os.MkdirAll(si.MountDir(), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(
		si.MountDir(), "System.map-5.15.0-78-generic"), []byte{}, 0644), IsNil)

	s.state.Lock()

	// state must contain the component
	setStateWithOneComponent(s.state, snapName, snap.R(1), compName, compRev)

	t := s.state.NewTask("setup-kernel-modules-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup",
		snapstate.NewComponentSetup(csi, snap.TestComponent, compPath))
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking undo setup")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(chg.Err().Error(), Equals, "cannot perform the following tasks:\n"+
		"- provoking undo setup (error out)")
	// State of task is as expected
	c.Check(t.Status(), Equals, state.UndoneStatus)
	s.state.Unlock()

	// Ensure backend calls have happened with the expected data
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:            "setup-kernel-modules-component",
			compMountDir:  filepath.Join(dirs.SnapMountDir, "mysnap/components/2/mycomp"),
			compMountFile: filepath.Join(dirs.GlobalRootDir, "var/lib/snapd/snaps/mysnap+mycomp_7.comp"),
			cref:          cref,
			kernelVersion: "5.15.0-78-generic",
		},
		{
			op:            "setup-kernel-modules-component",
			compMountDir:  filepath.Join(dirs.SnapMountDir, "mysnap/components/2/mycomp"),
			compMountFile: filepath.Join(dirs.GlobalRootDir, "var/lib/snapd/snaps/mysnap+mycomp_7.comp"),
			cref:          cref,
			kernelVersion: "5.15.0-78-generic",
		},
	})
}

func (s *kernelModulesCompSnapSuite) TestDoCleanupKernelModulesComponent(c *C) {
	const snapName = "kernelsnap"
	const compName = "kmodcomp"
	snapRev := snap.R(1)
	currentCompRev := snap.R(5)
	compRev := snap.R(7)

	ci, _ := createTestComponent(c, snapName, compName)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string) (*snap.ComponentInfo, error) {
		return ci, nil
	}))
	// Create file so kernel version can be found
	c.Assert(os.MkdirAll(si.MountDir(), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(
		si.MountDir(), "System.map-5.15.0-78-generic"), []byte{}, 0644), IsNil)

	s.state.Lock()

	// state must contain the component
	setStateWithOneComponent(s.state, snapName, snapRev, compName, currentCompRev)

	t := s.state.NewTask("cleanup-kernel-modules-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, snap.TestComponent, ""))
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	c.Check(chg.Err(), IsNil)
	// State of task is as expected
	c.Assert(t.Status(), Equals, state.DoneStatus)
	s.state.Unlock()

	// Ensure backend calls have happened with the expected data
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:            "undo-setup-kernel-modules-component",
			compMountDir:  filepath.Join(dirs.SnapMountDir, "kernelsnap/components/1/kmodcomp"),
			compMountFile: filepath.Join(dirs.GlobalRootDir, "var/lib/snapd/snaps/kernelsnap+kmodcomp_5.comp"),
			cref:          cref,
			kernelVersion: "5.15.0-78-generic",
		},
	})
}

func (s *kernelModulesCompSnapSuite) TestDoThenUndoCleanupKernelModulesComponent(c *C) {
	const snapName = "kernelsnap"
	const compName = "kmodcomp"
	snapRev := snap.R(1)
	currentCompRev := snap.R(5)
	compRev := snap.R(7)

	ci, _ := createTestComponent(c, snapName, compName)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string) (*snap.ComponentInfo, error) {
		return ci, nil
	}))
	// Create file so kernel version can be found
	c.Assert(os.MkdirAll(si.MountDir(), 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(
		si.MountDir(), "System.map-5.15.0-78-generic"), []byte{}, 0644), IsNil)

	s.state.Lock()

	// state must contain the component
	setStateWithOneComponent(s.state, snapName, snapRev, compName, currentCompRev)

	t := s.state.NewTask("cleanup-kernel-modules-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, snap.TestComponent, ""))
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking undo setup")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	c.Check(chg.Err().Error(), Equals, "cannot perform the following tasks:\n"+
		"- provoking undo setup (error out)")
	// State of task is as expected
	c.Check(t.Status(), Equals, state.UndoneStatus)
	s.state.Unlock()

	// Ensure backend calls have happened with the expected data
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:            "undo-setup-kernel-modules-component",
			compMountDir:  filepath.Join(dirs.SnapMountDir, "kernelsnap/components/1/kmodcomp"),
			compMountFile: filepath.Join(dirs.GlobalRootDir, "var/lib/snapd/snaps/kernelsnap+kmodcomp_5.comp"),
			cref:          cref,
			kernelVersion: "5.15.0-78-generic",
		},
		{
			op:            "setup-kernel-modules-component",
			compMountDir:  filepath.Join(dirs.SnapMountDir, "kernelsnap/components/1/kmodcomp"),
			compMountFile: filepath.Join(dirs.GlobalRootDir, "var/lib/snapd/snaps/kernelsnap+kmodcomp_5.comp"),
			cref:          cref,
			kernelVersion: "5.15.0-78-generic",
		},
	})
}
