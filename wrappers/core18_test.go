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

package wrappers_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/wrappers"
)

func makeMockSnapdSnap(c *C) *snap.Info {
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)

	info := snaptest.MockSnap(c, snapdYaml, &snap.SideInfo{Revision: snap.R(1)})
	snapdSrv := filepath.Join(info.MountDir(), "/lib/systemd/system/snapd.service")
	err = os.MkdirAll(filepath.Dir(snapdSrv), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(snapdSrv, []byte("[Unit]\nExecStart=/usr/lib/snapd/snapd"), 0644)
	c.Assert(err, IsNil)
	return info
}

func (s *servicesTestSuite) TestAddSnapServicesForSnapdOnCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	info := makeMockSnapdSnap(c)
	// add the snapd service
	err := wrappers.AddSnapServices(info, nil)
	c.Assert(err, IsNil)

	// check that snapd.service is created
	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapServicesDir, "snapd.service"))
	c.Assert(err, IsNil)
	// and paths get re-writen
	c.Check(string(content), Equals, fmt.Sprintf("[Unit]\nExecStart=%s/snapd/1/usr/lib/snapd/snapd", dirs.SnapMountDir))

	// check that usr-lib-snapd.mount is created
	content, err = ioutil.ReadFile(filepath.Join(dirs.SnapServicesDir, "usr-lib-snapd.mount"))
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, `[Unit]
Description=Make the snapd snap tooling available for the system
Before=snapd.service

[Mount]
What=$PREFIX/usr/lib/snapd
Where=/usr/lib/snapd
Type=none
Options=bind

[Install]
RequiredBy=snapd.service
`)

	// check that systemd got started
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "enable", "usr-lib-snapd.mount"},
		{"start", "usr-lib-snapd.mount"},
		{"--root", dirs.GlobalRootDir, "enable", "snapd.service"},
		{"start", "snapd.service"},
		{"daemon-reload"},
	})
}

func (s *servicesTestSuite) TestAddSnapServicesForSnapdOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	info := makeMockSnapdSnap(c)
	// add the snapd service
	err := wrappers.AddSnapServices(info, nil)
	c.Assert(err, IsNil)

	// check that snapd.service is created
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, "snapd.service")), Equals, false)

	c.Check(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, "usr-lib-snapd.mount")), Equals, false)

	// check that systemd got started
	c.Check(s.sysdLog, IsNil)
}
