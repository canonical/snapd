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

	Kind  systemd.Kind
	Mode  systemd.InstanceMode
	Meter systemd.Reporter

	AddMountUnitFileCalls  []ParamsForAddMountUnitFile
	AddMountUnitFileResult ResultForAddMountUnitFile

	RemoveMountUnitFileCalls  []string
	RemoveMountUnitFileResult error
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
	restore := systemd.MockNewSystemd(func(kind systemd.Kind, roodDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &FakeSystemd{Kind: kind, Mode: mode, Meter: meter}
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
	c.Check(sysd.AddMountUnitFileCalls, HasLen, 1)
	c.Check(sysd.AddMountUnitFileCalls[0], DeepEquals, expectedParameters)
}

func (s *mountunitSuite) TestRemoveMountUnit(c *C) {
	expectedErr := errors.New("removal error")

	var sysd *FakeSystemd
	restore := systemd.MockNewSystemd(func(kind systemd.Kind, roodDir string, mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd = &FakeSystemd{Kind: kind, Mode: mode, Meter: meter}
		sysd.RemoveMountUnitFileResult = expectedErr
		return sysd
	})
	defer restore()

	err := backend.RemoveMountUnit("/some/where", progress.Null)
	c.Check(err, Equals, expectedErr)
	c.Check(sysd.RemoveMountUnitFileCalls, HasLen, 1)
	c.Check(sysd.RemoveMountUnitFileCalls[0], Equals, "/some/where")
}
