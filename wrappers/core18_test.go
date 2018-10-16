// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	snapdDir := filepath.Join(info.MountDir(), "lib", "systemd", "system")
	err = os.MkdirAll(snapdDir, 0755)
	c.Assert(err, IsNil)
	snapdSrv := filepath.Join(snapdDir, "snapd.service")
	err = ioutil.WriteFile(snapdSrv, []byte("[Unit]\nExecStart=/usr/lib/snapd/snapd\n# X-Snapd-Snap: do-not-start"), 0644)
	c.Assert(err, IsNil)
	snapdShutdown := filepath.Join(snapdDir, "snapd.system-shutdown.service")
	err = ioutil.WriteFile(snapdShutdown, []byte("[Unit]\nExecStart=/bin/umount --everything\n# X-Snapd-Snap: do-not-start"), 0644)
	c.Assert(err, IsNil)
	snapdAutoimport := filepath.Join(snapdDir, "snapd.autoimport.service")
	err = ioutil.WriteFile(snapdAutoimport, []byte("[Unit]\nExecStart=/usr/bin/snap auto-import"), 0644)
	c.Assert(err, IsNil)

	return info
}

func (s *servicesTestSuite) TestAddSnapServicesForSnapdOnCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	defer restore()

	// reset root dir
	dirs.SetRootDir(s.tempdir)

	info := makeMockSnapdSnap(c)
	// add the snapd service
	err := wrappers.AddSnapServices(info, nil)
	c.Assert(err, IsNil)

	// check that snapd.service is created
	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapServicesDir, "snapd.service"))
	c.Assert(err, IsNil)
	// and paths get re-written
	c.Check(string(content), Equals, fmt.Sprintf("[Unit]\nExecStart=%s/snapd/1/usr/lib/snapd/snapd\n# X-Snapd-Snap: do-not-start", dirs.SnapMountDir))

	// check that snapd.autoimport.service is created
	content, err = ioutil.ReadFile(filepath.Join(dirs.SnapServicesDir, "snapd.autoimport.service"))
	c.Assert(err, IsNil)
	// and paths get re-written
	c.Check(string(content), Equals, fmt.Sprintf("[Unit]\nExecStart=%s/snapd/1/usr/bin/snap auto-import", dirs.SnapMountDir))

	// check that snapd.system-shutdown.service is created
	content, err = ioutil.ReadFile(filepath.Join(dirs.SnapServicesDir, "snapd.system-shutdown.service"))
	c.Assert(err, IsNil)
	// and paths *do not* get re-written
	c.Check(string(content), Equals, "[Unit]\nExecStart=/bin/umount --everything\n# X-Snapd-Snap: do-not-start")

	// check that usr-lib-snapd.mount is created
	content, err = ioutil.ReadFile(filepath.Join(dirs.SnapServicesDir, "usr-lib-snapd.mount"))
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, fmt.Sprintf(`[Unit]
Description=Make the snapd snap tooling available for the system
Before=snapd.service

[Mount]
What=%s/snap/snapd/1/usr/lib/snapd
Where=/usr/lib/snapd
Type=none
Options=bind

[Install]
WantedBy=snapd.service
`, dirs.GlobalRootDir))

	// check the systemctl calls
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--root", dirs.GlobalRootDir, "enable", "usr-lib-snapd.mount"},
		{"stop", "usr-lib-snapd.mount"},
		{"show", "--property=ActiveState", "usr-lib-snapd.mount"},
		{"start", "usr-lib-snapd.mount"},
		{"daemon-reload"},
		{"--root", dirs.GlobalRootDir, "enable", "snapd.autoimport.service"},
		{"--root", dirs.GlobalRootDir, "enable", "snapd.service"},
		{"--root", dirs.GlobalRootDir, "enable", "snapd.system-shutdown.service"},
		{"stop", "snapd.autoimport.service"},
		{"show", "--property=ActiveState", "snapd.autoimport.service"},
		{"start", "snapd.autoimport.service"},
		{"start", "snapd.service"},
		{"start", "--no-block", "snapd.seeded.service"},
	})
}

func (s *servicesTestSuite) TestAddSnapServicesForSnapdOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	info := makeMockSnapdSnap(c)
	// add the snapd service
	err := wrappers.AddSnapServices(info, nil)
	c.Assert(err, IsNil)

	// check that snapd services were *not* created
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, "snapd.service")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, "snapd.autoimport.service")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, "snapd.system-shutdown.service")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, "usr-lib-snapd.mount")), Equals, false)

	// check that no systemctl calls happened
	c.Check(s.sysdLog, IsNil)
}
