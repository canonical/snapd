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

type ParamsForAddMountUnitFile struct {
	name, revision, what, where, fstype string
}

type ResultForAddMountUnitFile struct {
	path string
	err  error
}

type FakeSystemd struct {
	systemd.Systemd

	AddMountUnitFileCalls  []ParamsForAddMountUnitFile
	AddMountUnitFileResult ResultForAddMountUnitFile

	RemoveMountUnitFileCalls  []string
	RemoveMountUnitFileResult error

	ListMountUnitsCalls  []ParamsForListMountUnits
	ListMountUnitsResult ResultForListMountUnits
}

func (s *FakeSystemd) AddMountUnitFile(name, revision, what, where, fstype string) (string, error) {
	s.AddMountUnitFileCalls = append(s.AddMountUnitFileCalls,
		ParamsForAddMountUnitFile{name, revision, what, where, fstype})
	return s.AddMountUnitFileResult.path, s.AddMountUnitFileResult.err
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
	expectedErr := errors.New("creation error")

	var sysd *FakeSystemd
	restore := systemd.MockNewSystemd(func(be systemd.Backend, roodDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &FakeSystemd{}
		sysd.AddMountUnitFileResult = ResultForAddMountUnitFile{"", expectedErr}
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
	err := backend.AddMountUnit(info, false, progress.Null)
	c.Check(err, Equals, expectedErr)

	// ensure correct parameters
	expectedParameters := ParamsForAddMountUnitFile{
		name:     "foo",
		revision: "13",
		what:     "/var/lib/snapd/snaps/foo_13.snap",
		where:    fmt.Sprintf("%s/foo/13", dirs.StripRootDir(dirs.SnapMountDir)),
		fstype:   "squashfs",
	}
	c.Check(sysd.AddMountUnitFileCalls, DeepEquals, []ParamsForAddMountUnitFile{
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
	err := b.RemoveSnapMountUnits(info, progress.Null)
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
	err := b.RemoveSnapMountUnits(info, progress.Null)
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
	err := b.RemoveSnapMountUnits(info, progress.Null)
	c.Check(err, IsNil)
	c.Check(sysd.ListMountUnitsCalls, HasLen, 1)
	c.Check(sysd.ListMountUnitsCalls, DeepEquals, []ParamsForListMountUnits{
		{snapName: "foo", origin: ""},
	})

	c.Check(sysd.RemoveMountUnitFileCalls, HasLen, 3)
	c.Check(sysd.RemoveMountUnitFileCalls, DeepEquals, returnedMountPoints)
}
