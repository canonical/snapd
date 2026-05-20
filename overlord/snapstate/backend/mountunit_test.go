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
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type ParamsForConfigureMountUnitOptions struct {
	what, fstype       string
	startBeforeDrivers bool
}

type ResultForConfigureMountUnitOptions struct {
	fsType        string
	options       []string
	mountUnitType systemd.MountUnitType
}

type ParamsForEnsureMountUnitFile struct {
	description string
	where       string
	options     []string
}

type ResultForEnsureMountUnitFile struct {
	path string
	err  error
}

type FakeSystemd struct {
	systemd.Systemd

	ConfigureMountUnitOptionsCalls   []ParamsForConfigureMountUnitOptions
	ConfigureMountUnitOptionsResults ResultForConfigureMountUnitOptions

	EnsureMountUnitFileCalls  []ParamsForEnsureMountUnitFile
	EnsureMountUnitFileResult ResultForEnsureMountUnitFile

	RemoveMountUnitFileCalls  []string
	RemoveMountUnitFileResult error

	ListMountUnitsCalls  []ParamsForListMountUnits
	ListMountUnitsResult ResultForListMountUnits
}

func (s *FakeSystemd) ConfigureMountUnitOptions(o *systemd.MountUnitOptions, fstype string, startBeforeDrivers bool) error {
	s.ConfigureMountUnitOptionsCalls = append(s.ConfigureMountUnitOptionsCalls, ParamsForConfigureMountUnitOptions{o.What, fstype, startBeforeDrivers})

	o.Fstype = s.ConfigureMountUnitOptionsResults.fsType
	o.MountUnitType = s.ConfigureMountUnitOptionsResults.mountUnitType
	o.Options = s.ConfigureMountUnitOptionsResults.options

	return nil
}

func (s *FakeSystemd) EnsureMountUnitFile(mountOptions *systemd.MountUnitOptions) (string, error) {
	s.EnsureMountUnitFileCalls = append(s.EnsureMountUnitFileCalls, ParamsForEnsureMountUnitFile{
		mountOptions.Description,
		mountOptions.Where,
		mountOptions.Options,
	})
	return s.EnsureMountUnitFileResult.path, s.EnsureMountUnitFileResult.err
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
		sysd.EnsureMountUnitFileResult = ResultForEnsureMountUnitFile{"", expectedErr}
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
	expectedMountUnitParameters := ParamsForConfigureMountUnitOptions{
		what:               "/var/lib/snapd/snaps/foo_13.snap",
		fstype:             "squashfs",
		startBeforeDrivers: flags.StartBeforeDriversLoad,
	}
	c.Check(sysd.ConfigureMountUnitOptionsCalls, DeepEquals, []ParamsForConfigureMountUnitOptions{
		expectedMountUnitParameters,
	})

	expectedParameters := ParamsForEnsureMountUnitFile{
		description: "Mount unit for foo, revision 13",
		where:       fmt.Sprintf("%s/foo/13", dirs.StripRootDir(dirs.SnapMountDir)),
	}

	c.Check(sysd.EnsureMountUnitFileCalls, DeepEquals, []ParamsForEnsureMountUnitFile{
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
	err := b.RemoveContainerMountUnits(info, progress.Null, "", nil)
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
	err := b.RemoveContainerMountUnits(info, progress.Null, "", nil)
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
	err := b.RemoveContainerMountUnits(info, progress.Null, "", nil)
	c.Check(err, IsNil)
	c.Check(sysd.ListMountUnitsCalls, HasLen, 1)
	c.Check(sysd.ListMountUnitsCalls, DeepEquals, []ParamsForListMountUnits{
		{snapName: "foo", origin: ""},
	})

	c.Check(sysd.RemoveMountUnitFileCalls, HasLen, 3)
	c.Check(sysd.RemoveMountUnitFileCalls, DeepEquals, returnedMountPoints)
}

func (s *mountunitSuite) TestRemoveSnapMountUnitsFiltersBaseDirs(c *C) {
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "some-snap",
			Revision: snap.R(1),
		},
	}

	// Only mount points under the specified base dirs should be removed.
	baseDirs := []string{"/var/snap/some-snap/1", "/var/snap/some-snap/common"}
	mountPoints := []string{
		"/var/snap/some-snap/1/target",   // under revision base dir: matched
		"/var/snap/some-snap/common/dir", // under common base dir: matched
		"/var/snap/other-snap/1/target",  // unrelated snap: not matched
	}

	var sysd *FakeSystemd
	restore := systemd.MockNewSystemd(func(be systemd.Backend, rootDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &FakeSystemd{}
		sysd.ListMountUnitsResult = ResultForListMountUnits{mountPoints, nil}
		return sysd
	})
	defer restore()

	b := backend.Backend{}
	err := b.RemoveContainerMountUnits(info, progress.Null, "mount-control", baseDirs)
	c.Assert(err, IsNil)

	c.Check(sysd.ListMountUnitsCalls, DeepEquals, []ParamsForListMountUnits{
		{snapName: "some-snap", origin: "mount-control"},
	})
	// Only the two matching mount points should have been removed.
	c.Assert(sysd.RemoveMountUnitFileCalls, HasLen, 2)
	c.Assert(sysd.RemoveMountUnitFileCalls, DeepEquals, []string{
		"/var/snap/some-snap/1/target",
		"/var/snap/some-snap/common/dir",
	})
}

func (s *mountunitSuite) TestIsUnderAnyDir(c *C) {
	// Subdirectory matches.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1/bar", []string{"/var/snap/foo/1"}), Equals, true)
	// Trailing slash on path must not affect matching.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1/bar/", []string{"/var/snap/foo/1"}), Equals, true)
	// Trailing slash on candidate must not affect matching.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1/bar", []string{"/var/snap/foo/1/"}), Equals, true)
	// Trailing slash on both must not affect matching.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1/bar/", []string{"/var/snap/foo/1/"}), Equals, true)
	// Path equal to candidate must not match (strict subdirectory check).
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1", []string{"/var/snap/foo/1"}), Equals, false)
	// Path equal to candidate with trailing slash on path must not match.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1/", []string{"/var/snap/foo/1"}), Equals, false)
	// Path equal to candidate with trailing slash on candidate must not match.
	c.Check(backend.IsUnderAnyDir("/var/snap/foo/1", []string{"/var/snap/foo/1/"}), Equals, false)
	// Unrelated path must not match.
	c.Check(backend.IsUnderAnyDir("/var/snap/other/1/bar", []string{"/var/snap/foo/1"}), Equals, false)
}
