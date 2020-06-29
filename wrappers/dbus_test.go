// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/wrappers"
)

type dbusTestSuite struct {
	tempdir string
}

var _ = Suite(&dbusTestSuite{})

func (s *dbusTestSuite) SetUpTest(c *C) {
	s.tempdir = c.MkDir()
	dirs.SetRootDir(s.tempdir)
}

func (s *dbusTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

const dbusSnapYaml = `
name: snapname
version: 1.0
slots:
  system1:
    interface: dbus
    bus: system
    name: org.example.Foo
  system2:
    interface: dbus
    bus: system
    name: org.example.Bar
  session1:
    interface: dbus
    bus: session
    name: org.example.Foo
  session2:
    interface: dbus
    bus: session
    name: org.example.Bar
apps:
  system-svc:
    command: bin/start-system
    daemon: simple
    activates-on:
      - system1
      - system2
  session-svc:
    command: bin/start-session
    daemon: simple
    daemon-scope: user
    activates-on:
      - session1
      - session2
`

func (s *dbusTestSuite) TestGenerateDBusActivationFile(c *C) {
	info := snaptest.MockSnap(c, dbusSnapYaml, &snap.SideInfo{Revision: snap.R(12)})

	app := info.Apps["system-svc"]
	svcWrapper, err := wrappers.GenerateDBusActivationFile(app, "org.example.Foo")
	c.Assert(err, IsNil)
	c.Check(string(svcWrapper), Equals, `[D-BUS Service]
Name=org.example.Foo
Comment=Bus name for snap application snapname.system-svc
SystemdService=snap.snapname.system-svc.service
Exec=/usr/bin/snap run snapname.system-svc
AssumedAppArmorLabel=snap.snapname.system-svc
User=root
X-Snap=snapname
`)

	app = info.Apps["session-svc"]
	svcWrapper, err = wrappers.GenerateDBusActivationFile(app, "org.example.Foo")
	c.Assert(err, IsNil)
	c.Check(string(svcWrapper), Equals, `[D-BUS Service]
Name=org.example.Foo
Comment=Bus name for snap application snapname.session-svc
SystemdService=snap.snapname.session-svc.service
Exec=/usr/bin/snap run snapname.session-svc
AssumedAppArmorLabel=snap.snapname.session-svc
X-Snap=snapname
`)
}

func (s *dbusTestSuite) TestAddSnapDBusActivationFiles(c *C) {
	info := snaptest.MockSnap(c, dbusSnapYaml, &snap.SideInfo{Revision: snap.R(12)})

	err := wrappers.AddSnapDBusActivationFiles(info)
	c.Assert(err, IsNil)

	c.Check(filepath.Join(dirs.SnapDBusSessionServicesDir, "org.example.Foo.service"), testutil.FileContains, "SystemdService=snap.snapname.session-svc.service\n")
	c.Check(filepath.Join(dirs.SnapDBusSessionServicesDir, "org.example.Bar.service"), testutil.FileContains, "SystemdService=snap.snapname.session-svc.service\n")

	c.Check(filepath.Join(dirs.SnapDBusSystemServicesDir, "org.example.Foo.service"), testutil.FileContains, "SystemdService=snap.snapname.system-svc.service\n")
	c.Check(filepath.Join(dirs.SnapDBusSystemServicesDir, "org.example.Bar.service"), testutil.FileContains, "SystemdService=snap.snapname.system-svc.service\n")
}

func (s *dbusTestSuite) TestAddSnapDBusActivationFilesRemovesLeftovers(c *C) {
	c.Assert(os.MkdirAll(dirs.SnapDBusSessionServicesDir, 0755), IsNil)
	c.Assert(os.MkdirAll(dirs.SnapDBusSystemServicesDir, 0755), IsNil)

	sessionSvc := filepath.Join(dirs.SnapDBusSessionServicesDir, "org.example.Baz.service")
	c.Assert(ioutil.WriteFile(sessionSvc, []byte("[D-BUS Service]\nX-Snap=snapname\n"), 0644), IsNil)
	systemSvc := filepath.Join(dirs.SnapDBusSessionServicesDir, "org.example.Baz.service")
	c.Assert(ioutil.WriteFile(systemSvc, []byte("[D-BUS Service]\nX-Snap=snapname\n"), 0644), IsNil)

	otherSessionSvc := filepath.Join(dirs.SnapDBusSessionServicesDir, "org.example.OtherSnap.service")
	c.Assert(ioutil.WriteFile(otherSessionSvc, []byte("[D-BUS Service]\nX-Snap=other-snap\n"), 0644), IsNil)
	otherSystemSvc := filepath.Join(dirs.SnapDBusSystemServicesDir, "org.example.OtherSnap.service")
	c.Assert(ioutil.WriteFile(otherSystemSvc, []byte("[D-BUS Service]\nX-Snap=other-snap\n"), 0644), IsNil)

	info := snaptest.MockSnap(c, dbusSnapYaml, &snap.SideInfo{Revision: snap.R(12)})
	err := wrappers.AddSnapDBusActivationFiles(info)
	c.Assert(err, IsNil)

	c.Check(sessionSvc, testutil.FileAbsent)
	c.Check(systemSvc, testutil.FileAbsent)

	// Files belonging to other snap are left as is
	c.Check(otherSessionSvc, testutil.FilePresent)
	c.Check(otherSystemSvc, testutil.FilePresent)
}

func (s *dbusTestSuite) TestRemoveSnapDBusActivationFiles(c *C) {
	c.Assert(os.MkdirAll(dirs.SnapDBusSessionServicesDir, 0755), IsNil)
	c.Assert(os.MkdirAll(dirs.SnapDBusSystemServicesDir, 0755), IsNil)

	sessionSvc := filepath.Join(dirs.SnapDBusSessionServicesDir, "org.example.Baz.service")
	c.Assert(ioutil.WriteFile(sessionSvc, []byte("[D-BUS Service]\nX-Snap=snapname\n"), 0644), IsNil)
	systemSvc := filepath.Join(dirs.SnapDBusSessionServicesDir, "org.example.Baz.service")
	c.Assert(ioutil.WriteFile(systemSvc, []byte("[D-BUS Service]\nX-Snap=snapname\n"), 0644), IsNil)

	otherSessionSvc := filepath.Join(dirs.SnapDBusSessionServicesDir, "org.example.OtherSnap.service")
	c.Assert(ioutil.WriteFile(otherSessionSvc, []byte("[D-BUS Service]\nX-Snap=other-snap\n"), 0644), IsNil)
	otherSystemSvc := filepath.Join(dirs.SnapDBusSystemServicesDir, "org.example.OtherSnap.service")
	c.Assert(ioutil.WriteFile(otherSystemSvc, []byte("[D-BUS Service]\nX-Snap=other-snap\n"), 0644), IsNil)

	info := snaptest.MockSnap(c, dbusSnapYaml, &snap.SideInfo{Revision: snap.R(12)})
	err := wrappers.RemoveSnapDBusActivationFiles(info)
	c.Assert(err, IsNil)

	c.Check(sessionSvc, testutil.FileAbsent)
	c.Check(systemSvc, testutil.FileAbsent)

	// Files belonging to other snap are left as is
	c.Check(otherSessionSvc, testutil.FilePresent)
	c.Check(otherSystemSvc, testutil.FilePresent)
}
