// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/snapshotstate/backend"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/systemd/systemdtest"
	"github.com/snapcore/snapd/testutil"
)

type mountunitSuite struct {
	testutil.BaseTest
}

var _ = Suite(&mountunitSuite{})

func (s *mountunitSuite) TestListMountControlMountPointsHappy(c *C) {
	returnedMounts := []string{"/var/snap/a-snap/x1/data/mymount", "/var/snap/a-snap/common/media"}

	var sysd *systemdtest.FakeSystemd
	restore := systemd.MockNewSystemd(func(_ systemd.Backend, _ string, _ systemd.InstanceMode, _ systemd.Reporter) systemd.Systemd {
		sysd = &systemdtest.FakeSystemd{}
		sysd.ListMountUnitsResult.MountPoints = returnedMounts
		return sysd
	})
	defer restore()

	mountPts, err := backend.ListMountControlMountPoints("a-snap")
	c.Assert(err, IsNil)
	c.Check(mountPts, DeepEquals, returnedMounts)

	c.Check(sysd.ListMountUnitsCalls, DeepEquals, []systemdtest.ParamsForListMountUnits{{SnapName: "a-snap", Origin: "mount-control"}})
}

func (s *mountunitSuite) TestListMountControlMountPointsError(c *C) {
	expectedErr := errors.New("systemd failure")

	var sysd *systemdtest.FakeSystemd
	restore := systemd.MockNewSystemd(func(_ systemd.Backend, _ string, _ systemd.InstanceMode, _ systemd.Reporter) systemd.Systemd {
		sysd = &systemdtest.FakeSystemd{}
		sysd.ListMountUnitsResult.Err = expectedErr
		return sysd
	})
	defer restore()

	mountPts, err := backend.ListMountControlMountPoints("a-snap")
	c.Check(err, Equals, expectedErr)
	c.Check(mountPts, IsNil)

	c.Check(sysd.ListMountUnitsCalls, DeepEquals, []systemdtest.ParamsForListMountUnits{{SnapName: "a-snap", Origin: "mount-control"}})
}

func (s *mountunitSuite) TestListMountControlMountPointsEmpty(c *C) {
	var sysd *systemdtest.FakeSystemd
	restore := systemd.MockNewSystemd(func(_ systemd.Backend, _ string, _ systemd.InstanceMode, _ systemd.Reporter) systemd.Systemd {
		sysd = &systemdtest.FakeSystemd{}
		sysd.ListMountUnitsResult.MountPoints = []string{}
		return sysd
	})
	defer restore()

	mountPts, err := backend.ListMountControlMountPoints("a-snap")
	c.Assert(err, IsNil)
	c.Check(mountPts, DeepEquals, []string{})

	c.Check(sysd.ListMountUnitsCalls, DeepEquals, []systemdtest.ParamsForListMountUnits{{SnapName: "a-snap", Origin: "mount-control"}})
}
