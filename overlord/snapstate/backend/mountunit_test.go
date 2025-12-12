// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/integrity"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type ParamsForMountUnitConfiguration struct {
	what, fstype       string
	startBeforeDrivers bool
}

type ResultForMountUnitConfiguration struct {
	fsType        string
	options       []string
	mountUnitType systemd.MountUnitType
}

type ParamsForEnsureMountUnitFileWithOptions struct {
	description string
	where       string
	options     []string
}

type ResultForEnsureMountUnitFileWithOptions struct {
	path string
	err  error
}

type FakeSystemd struct {
	systemd.Systemd

	MountUnitConfigurationCalls   []ParamsForMountUnitConfiguration
	MountUnitConfigurationResults ResultForMountUnitConfiguration

	EnsureMountUnitFileWithOptionsCalls  []ParamsForEnsureMountUnitFileWithOptions
	EnsureMountUnitFileWithOptionsResult ResultForEnsureMountUnitFileWithOptions

	RemoveMountUnitFileCalls  []string
	RemoveMountUnitFileResult error

	ListMountUnitsCalls  []ParamsForListMountUnits
	ListMountUnitsResult ResultForListMountUnits
}

func (s *FakeSystemd) MountUnitConfiguration(what string, fstype string, startBeforeDrivers bool) (string, []string, systemd.MountUnitType) {
	s.MountUnitConfigurationCalls = append(s.MountUnitConfigurationCalls, ParamsForMountUnitConfiguration{what, fstype, startBeforeDrivers})
	return s.MountUnitConfigurationResults.fsType, s.MountUnitConfigurationResults.options, s.MountUnitConfigurationResults.mountUnitType
}

func (s *FakeSystemd) EnsureMountUnitFileWithOptions(mountOptions *systemd.MountUnitOptions) (string, error) {
	s.EnsureMountUnitFileWithOptionsCalls = append(s.EnsureMountUnitFileWithOptionsCalls, ParamsForEnsureMountUnitFileWithOptions{
		mountOptions.Description,
		mountOptions.Where,
		mountOptions.Options,
	})
	return s.EnsureMountUnitFileWithOptionsResult.path, s.EnsureMountUnitFileWithOptionsResult.err
}

func (s *FakeSystemd) RemoveMountUnitFile(mountDir string) error {
	s.RemoveMountUnitFileCalls = append(s.RemoveMountUnitFileCalls, mountDir)
	return s.RemoveMountUnitFileResult
}

type ParamsForListMountUnits struct {
	snapName, origin string
}

type ResultForListMountUnits struct {
	mountPoints []string
	err         error
}

func (s *FakeSystemd) ListMountUnits(snapName, origin string) ([]string, error) {
	s.ListMountUnitsCalls = append(s.ListMountUnitsCalls,
		ParamsForListMountUnits{snapName, origin})
	return s.ListMountUnitsResult.mountPoints, s.ListMountUnitsResult.err
}

type mountunitSuite struct {
	testutil.BaseTest
}

var _ = Suite(&mountunitSuite{})

func (s *mountunitSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *mountunitSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *mountunitSuite) TestAddMountUnit(c *C) {
	s.testAddMountUnit(c, backend.MountUnitFlags{})
}

func (s *mountunitSuite) TestAddBeforeDriversMountUnit(c *C) {
	s.testAddMountUnit(c, backend.MountUnitFlags{StartBeforeDriversLoad: true})
}

func (s *mountunitSuite) testAddMountUnit(c *C, flags backend.MountUnitFlags) {
	expectedErr := errors.New("creation error")

	var sysd *FakeSystemd
	restore := systemd.MockNewSystemd(func(be systemd.Backend, roodDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &FakeSystemd{}
		sysd.EnsureMountUnitFileWithOptionsResult = ResultForEnsureMountUnitFileWithOptions{"", expectedErr}
		return sysd
	})
	defer restore()

	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(13),
		},
		Version:       "1.1",
		Architectures: []string{"all"},
	}
	err := backend.AddMountUnit(info, systemd.New(systemd.SystemMode, progress.Null), flags)
	c.Check(err, Equals, expectedErr)

	// ensure correct parameters
	expectedMountUnitParameters := ParamsForMountUnitConfiguration{
		what:               "/var/lib/snapd/snaps/foo_13.snap",
		fstype:             "squashfs",
		startBeforeDrivers: flags.StartBeforeDriversLoad,
	}
	c.Check(sysd.MountUnitConfigurationCalls, DeepEquals, []ParamsForMountUnitConfiguration{
		expectedMountUnitParameters,
	})

	expectedParameters := ParamsForEnsureMountUnitFileWithOptions{
		description: "Mount unit for foo, revision 13",
		where:       fmt.Sprintf("%s/foo/13", dirs.StripRootDir(dirs.SnapMountDir)),
	}

	c.Check(sysd.EnsureMountUnitFileWithOptionsCalls, DeepEquals, []ParamsForEnsureMountUnitFileWithOptions{
		expectedParameters,
	})
}

func (s *mountunitSuite) TestRemoveMountUnit(c *C) {
	expectedErr := errors.New("removal error")

	var sysd *FakeSystemd
	restore := systemd.MockNewSystemd(func(be systemd.Backend, roodDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &FakeSystemd{}
		sysd.RemoveMountUnitFileResult = expectedErr
		return sysd
	})
	defer restore()

	err := backend.RemoveMountUnit("/some/where", progress.Null)
	c.Check(err, Equals, expectedErr)
	c.Check(sysd.RemoveMountUnitFileCalls, HasLen, 1)
	c.Check(sysd.RemoveMountUnitFileCalls[0], Equals, "/some/where")
}

func (s *mountunitSuite) TestRemoveSnapMountUnitsFailOnList(c *C) {
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(13),
		},
		Version:       "1.1",
		Architectures: []string{"all"},
	}

	expectedErr := errors.New("listing error")

	var sysd *FakeSystemd
	restore := systemd.MockNewSystemd(func(be systemd.Backend, roodDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &FakeSystemd{}
		sysd.ListMountUnitsResult = ResultForListMountUnits{nil, expectedErr}
		return sysd
	})
	defer restore()

	b := backend.Backend{}
	err := b.RemoveContainerMountUnits(info, progress.Null)
	c.Check(err, Equals, expectedErr)
	c.Check(sysd.ListMountUnitsCalls, HasLen, 1)
	c.Check(sysd.ListMountUnitsCalls, DeepEquals, []ParamsForListMountUnits{
		{snapName: "foo", origin: ""},
	})
	c.Check(sysd.RemoveMountUnitFileCalls, HasLen, 0)
}

func (s *mountunitSuite) TestRemoveSnapMountUnitsFailOnRemoval(c *C) {
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(13),
		},
		Version:       "1.1",
		Architectures: []string{"all"},
	}

	expectedErr := errors.New("removal error")
	returnedMountPoints := []string{"/here", "/and/there"}

	var sysd *FakeSystemd
	restore := systemd.MockNewSystemd(func(be systemd.Backend, roodDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &FakeSystemd{}
		sysd.ListMountUnitsResult = ResultForListMountUnits{returnedMountPoints, nil}
		sysd.RemoveMountUnitFileResult = expectedErr
		return sysd
	})
	defer restore()

	b := backend.Backend{}
	err := b.RemoveContainerMountUnits(info, progress.Null)
	c.Check(err, Equals, expectedErr)
	c.Check(sysd.ListMountUnitsCalls, HasLen, 1)
	c.Check(sysd.ListMountUnitsCalls, DeepEquals, []ParamsForListMountUnits{
		{snapName: "foo", origin: ""},
	})

	c.Check(sysd.RemoveMountUnitFileCalls, HasLen, 1)
	c.Check(sysd.RemoveMountUnitFileCalls, DeepEquals, []string{"/here"})
}

func (s *mountunitSuite) TestRemoveSnapMountUnitsHappy(c *C) {
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(13),
		},
		Version:       "1.1",
		Architectures: []string{"all"},
	}

	returnedMountPoints := []string{"/here", "/and/there", "/here/too"}

	var sysd *FakeSystemd
	restore := systemd.MockNewSystemd(func(be systemd.Backend, roodDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &FakeSystemd{}
		sysd.ListMountUnitsResult = ResultForListMountUnits{returnedMountPoints, nil}
		sysd.RemoveMountUnitFileResult = nil
		return sysd
	})
	defer restore()

	b := backend.Backend{}
	err := b.RemoveContainerMountUnits(info, progress.Null)
	c.Check(err, IsNil)
	c.Check(sysd.ListMountUnitsCalls, HasLen, 1)
	c.Check(sysd.ListMountUnitsCalls, DeepEquals, []ParamsForListMountUnits{
		{snapName: "foo", origin: ""},
	})

	c.Check(sysd.RemoveMountUnitFileCalls, HasLen, 3)
	c.Check(sysd.RemoveMountUnitFileCalls, DeepEquals, returnedMountPoints)
}

func (s *mountunitSuite) TestAddMountUnitWithIntegrity(c *C) {
	expectedErr := errors.New("creation error")
	flags := backend.MountUnitFlags{}

	var sysd *FakeSystemd
	restore := systemd.MockNewSystemd(func(be systemd.Backend, roodDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &FakeSystemd{}
		sysd.EnsureMountUnitFileWithOptionsResult = ResultForEnsureMountUnitFileWithOptions{"", expectedErr}
		return sysd
	})
	defer restore()

	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(13),
		},
		Version:       "1.1",
		Architectures: []string{"all"},
		IntegrityData: &snap.IntegrityDataInfo{
			IntegrityDataParams: integrity.IntegrityDataParams{
				Type:   "dm-verity",
				Digest: "aaa",
			},
		},
	}

	err := backend.AddMountUnit(info, systemd.New(systemd.SystemMode, progress.Null), flags)
	c.Check(err, Equals, expectedErr)

	// ensure correct parameters
	expectedMountUnitParameters := ParamsForMountUnitConfiguration{
		what:               "/var/lib/snapd/snaps/foo_13.snap",
		fstype:             "squashfs",
		startBeforeDrivers: flags.StartBeforeDriversLoad,
	}
	c.Check(sysd.MountUnitConfigurationCalls, DeepEquals, []ParamsForMountUnitConfiguration{
		expectedMountUnitParameters,
	})

	expectedParameters := ParamsForEnsureMountUnitFileWithOptions{
		description: "Mount unit for foo, revision 13",
		where:       fmt.Sprintf("%s/foo/13", dirs.StripRootDir(dirs.SnapMountDir)),
		options: []string{
			"verity.roothash=aaa",
			"verity.hashdevice=/var/lib/snapd/snaps/foo_13.snap.dmverity_aaa",
		},
	}

	c.Check(sysd.EnsureMountUnitFileWithOptionsCalls, DeepEquals, []ParamsForEnsureMountUnitFileWithOptions{
		expectedParameters,
	})
}
