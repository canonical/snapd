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
	"github.com/snapcore/snapd/systemd/systemdtest"
	"github.com/snapcore/snapd/testutil"
)

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

	type mountUnitOptions struct {
		name, revision, what, where, fstype string
	}
	var receivedParameters *mountUnitOptions
	restore := backend.MockSystemdNew(func(mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd := &systemdtest.FakeSystemd{Mode: mode, Reporter: meter}
		sysd.MockedAddMountUnitFile = func(name, revision, what, where, fstype string) (string, error) {
			receivedParameters = &mountUnitOptions{
				name: name,
				revision: revision,
				what: what,
				where: where,
				fstype: fstype,
			}
			return "/unused", expectedErr
		}
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
	expectedParameters := &mountUnitOptions{
		name: "foo",
		revision: "13",
		what: "/var/lib/snapd/snaps/foo_13.snap",
		where: fmt.Sprintf("%s/foo/13", dirs.StripRootDir(dirs.SnapMountDir)),
		fstype: "squashfs",
	}
	c.Check(receivedParameters, DeepEquals, expectedParameters)
}

func (s *mountunitSuite) TestRemoveMountUnit(c *C) {
	expectedErr := errors.New("removal error")

	var removedMountDir string
	restore := backend.MockSystemdNew(func(mode systemd.InstanceMode, meter systemd.Reporter) systemd.Systemd {
		sysd := &systemdtest.FakeSystemd{Mode: mode, Reporter: meter}
		sysd.MockedRemoveMountUnitFile = func(mountDir string) error {
			removedMountDir = mountDir
			return expectedErr
		}
		return sysd
	})
	defer restore()

	err := backend.RemoveMountUnit("/some/where", progress.Null)
	c.Check(err, Equals, expectedErr)
	c.Check(removedMountDir, Equals, "/some/where")
}
