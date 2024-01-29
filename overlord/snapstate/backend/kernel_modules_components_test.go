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

package backend_test

import (
	"errors"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type kernelModulesSuite struct {
	testutil.BaseTest

	be         backend.Backend
	sysctlArgs [][]string
}

var _ = Suite(&kernelModulesSuite{})

func (s *kernelModulesSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	restore := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysctlArgs = append(s.sysctlArgs, cmd)
		return []byte{}, nil
	})
	s.AddCleanup(restore)
	s.AddCleanup(func() { s.sysctlArgs = nil })

	s.AddCleanup(backend.MockRunDepmod(func(baseDir, kernelVersion string) error {
		return nil
	}))

	s.AddCleanup(osutil.MockMountInfo(""))
}

func (s *kernelModulesSuite) TestSetupKernelModulesComponentNoModules(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	const kernelVersion = "5.15.0-78-generic"

	cpi := snap.MinimalComponentContainerPlaceInfo(compName, snap.R(33), snapName, snap.R(1))
	cref := naming.NewComponentRef(snapName, compName)

	err := s.be.SetupKernelModulesComponent(cpi, cref, kernelVersion, progress.Null)
	c.Assert(err.Error(), Equals,
		"mysnap+mycomp does not contain firmware or components for 5.15.0-78-generic")
	_, ok := err.(*backend.NoKernelDriversError)
	c.Assert(ok, Equals, true)
}

func (s *kernelModulesSuite) TestSetupKernelModulesComponentWithModules(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	const kernelVersion = "5.15.0-78-generic"

	cpi := snap.MinimalComponentContainerPlaceInfo(compName, snap.R(33), snapName, snap.R(1))
	cref := naming.NewComponentRef(snapName, compName)
	// Make directories so the method thinks there are modules
	compDir := filepath.Join(dirs.SnapMountDir, snapName, "components/1", compName)
	modsDir := filepath.Join(compDir, "modules", kernelVersion)
	fwDir := filepath.Join(compDir, "firmware")
	c.Assert(os.MkdirAll(modsDir, os.ModePerm), IsNil)
	c.Assert(os.MkdirAll(fwDir, os.ModePerm), IsNil)

	err := s.be.SetupKernelModulesComponent(cpi, cref, kernelVersion, progress.Null)
	c.Assert(err, IsNil)
	kmodMountUnit := "run-mnt-kernel\\x2dmodules-5.15.0\\x2d78\\x2dgeneric-mycomp.mount"
	ktreeMountUnit := "usr-lib-modules-5.15.0\\x2d78\\x2dgeneric-updates-mycomp.mount"
	c.Assert(s.sysctlArgs, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--no-reload", "enable", kmodMountUnit},
		{"reload-or-restart", kmodMountUnit},
		{"daemon-reload"},
		{"--no-reload", "enable", ktreeMountUnit},
		{"reload-or-restart", ktreeMountUnit},
	})
	// Check mount files exist
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, kmodMountUnit)), Equals, true)
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, ktreeMountUnit)), Equals, true)

	// Now do a clean-up
	s.sysctlArgs = nil
	err = s.be.UndoSetupKernelModulesComponent(cpi, cref, kernelVersion, progress.Null)
	c.Assert(err, IsNil)
	c.Assert(s.sysctlArgs, DeepEquals, [][]string{
		{"--no-reload", "disable", ktreeMountUnit},
		{"daemon-reload"},
		{"--no-reload", "disable", kmodMountUnit},
		{"daemon-reload"},
	})
	// Check mount files do not exist
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, kmodMountUnit)), Equals, false)
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, ktreeMountUnit)), Equals, false)
}

func (s *kernelModulesSuite) TestSetupKernelModulesComponentJustFirmware(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	const kernelVersion = "5.15.0-78-generic"

	cpi := snap.MinimalComponentContainerPlaceInfo(compName, snap.R(33), snapName, snap.R(1))
	cref := naming.NewComponentRef(snapName, compName)
	// Make directories so the method thinks there are modules
	compDir := filepath.Join(dirs.SnapMountDir, snapName, "components/1", compName)
	fwDir := filepath.Join(compDir, "firmware")
	c.Assert(os.MkdirAll(fwDir, os.ModePerm), IsNil)

	err := s.be.SetupKernelModulesComponent(cpi, cref, kernelVersion, progress.Null)
	c.Assert(err, IsNil)
	kmodMountUnit := "run-mnt-kernel\\x2dmodules-5.15.0\\x2d78\\x2dgeneric-mycomp.mount"
	c.Assert(s.sysctlArgs, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--no-reload", "enable", kmodMountUnit},
		{"reload-or-restart", kmodMountUnit},
	})
	// Check mount files exist
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, kmodMountUnit)), Equals, true)

	// Now do a clean-up
	s.sysctlArgs = nil
	err = s.be.UndoSetupKernelModulesComponent(cpi, cref, kernelVersion, progress.Null)
	c.Assert(err, IsNil)
	c.Assert(s.sysctlArgs, DeepEquals, [][]string{
		{"--no-reload", "disable", kmodMountUnit},
		{"daemon-reload"},
	})
	// Check mount files do not exist
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, kmodMountUnit)), Equals, false)
}

func (s *kernelModulesSuite) TestSetupKernelModulesComponentDepmodFailed(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	const kernelVersion = "5.15.0-78-generic"

	cpi := snap.MinimalComponentContainerPlaceInfo(compName, snap.R(33), snapName, snap.R(1))
	cref := naming.NewComponentRef(snapName, compName)
	// Make directories so the method thinks there are modules
	compDir := filepath.Join(dirs.SnapMountDir, snapName, "components/1", compName)
	modsDir := filepath.Join(compDir, "modules", kernelVersion)
	fwDir := filepath.Join(compDir, "firmware")
	c.Assert(os.MkdirAll(modsDir, os.ModePerm), IsNil)
	c.Assert(os.MkdirAll(fwDir, os.ModePerm), IsNil)
	// make depmod fail
	s.AddCleanup(backend.MockRunDepmod(func(baseDir, kernelVersion string) error {
		return errors.New("depmod failure")
	}))

	err := s.be.SetupKernelModulesComponent(cpi, cref, kernelVersion, progress.Null)
	c.Assert(err.Error(), Equals, "depmod failure")
	kmodMountUnit := "run-mnt-kernel\\x2dmodules-5.15.0\\x2d78\\x2dgeneric-mycomp.mount"
	ktreeMountUnit := "usr-lib-modules-5.15.0\\x2d78\\x2dgeneric-updates-mycomp.mount"
	c.Assert(s.sysctlArgs, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--no-reload", "enable", kmodMountUnit},
		{"reload-or-restart", kmodMountUnit},
		{"daemon-reload"},
		{"--no-reload", "enable", ktreeMountUnit},
		{"reload-or-restart", ktreeMountUnit},
		{"--no-reload", "disable", ktreeMountUnit},
		{"daemon-reload"},
		{"--no-reload", "disable", kmodMountUnit},
		{"daemon-reload"},
	})
	// Check mount files do not exist
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, kmodMountUnit)), Equals, false)
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, ktreeMountUnit)), Equals, false)
}
