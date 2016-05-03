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

package snappy

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/snaptest"
	"github.com/ubuntu-core/snappy/systemd"
	"github.com/ubuntu-core/snappy/timeout"
)

func (s *SnapTestSuite) TestAddPackageServicesStripsGlobalRootdir(c *C) {
	// ensure that even with a global rootdir the paths in the generated
	// .services file are setup correctly (i.e. that the global root
	// is stripped)
	c.Assert(dirs.GlobalRootDir, Not(Equals), "/")

	yamlFile, err := makeInstalledMockSnap("", 12)
	c.Assert(err, IsNil)
	snap, err := NewInstalledSnap(yamlFile)
	c.Assert(err, IsNil)

	err = addPackageServices(snap.Info(), nil)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(filepath.Join(s.tempdir, "/etc/systemd/system/snap.hello-snap.svc1.service"))
	c.Assert(err, IsNil)

	baseDirWithoutRootPrefix := "/snap/" + helloSnapComposedName + "/12"
	verbs := []string{"Start", "Stop", "StopPost"}
	bins := []string{"hello", "goodbye", "missya"}
	for i := range verbs {
		expected := fmt.Sprintf("Exec%s=/usr/bin/ubuntu-core-launcher snap.hello-snap.svc1 snap.hello-snap.svc1 %s/bin/%s",
			verbs[i], baseDirWithoutRootPrefix, bins[i])
		c.Check(string(content), Matches, "(?ms).*^"+regexp.QuoteMeta(expected)) // check.v1 adds ^ and $ around the regexp provided
	}
}

func (s *SnapTestSuite) TestAddPackageBinariesStripsGlobalRootdir(c *C) {
	// ensure that even with a global rootdir the paths in the generated
	// .services file are setup correctly (i.e. that the global root
	// is stripped)
	c.Assert(dirs.GlobalRootDir, Not(Equals), "/")

	yamlFile, err := makeInstalledMockSnap("", 11)
	c.Assert(err, IsNil)
	snap, err := NewInstalledSnap(yamlFile)
	c.Assert(err, IsNil)

	err = addPackageBinaries(snap.Info())
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(filepath.Join(s.tempdir, "/snap/bin/hello-snap.hello"))
	c.Assert(err, IsNil)

	needle := `
ubuntu-core-launcher snap.hello-snap.hello snap.hello-snap.hello /snap/hello-snap/11/bin/hello "$@"
`
	c.Assert(string(content), Matches, "(?ms).*"+regexp.QuoteMeta(needle)+".*")
}

var (
	expectedServiceWrapperFmt = `[Unit]
# Auto-generated, DO NO EDIT
Description=Service for snap application xkcd-webserver.xkcd-webserver
%s
X-Snappy=yes

[Service]
ExecStart=/usr/bin/ubuntu-core-launcher snap.xkcd-webserver.xkcd-webserver snap.xkcd-webserver.xkcd-webserver /snap/xkcd-webserver/44/bin/foo start
Restart=on-failure
WorkingDirectory=/var/snap/xkcd-webserver/44
Environment="SNAP=/snap/xkcd-webserver/44" "SNAP_DATA=/var/snap/xkcd-webserver/44" "SNAP_NAME=xkcd-webserver" "SNAP_VERSION=0.3.4" "SNAP_REVISION=44" "SNAP_ARCH=%[3]s" "SNAP_LIBRARY_PATH=/var/lib/snapd/lib/gl:" "SNAP_USER_DATA=/root/snap/xkcd-webserver/44"
ExecStop=/usr/bin/ubuntu-core-launcher snap.xkcd-webserver.xkcd-webserver snap.xkcd-webserver.xkcd-webserver /snap/xkcd-webserver/44/bin/foo stop
ExecStopPost=/usr/bin/ubuntu-core-launcher snap.xkcd-webserver.xkcd-webserver snap.xkcd-webserver.xkcd-webserver /snap/xkcd-webserver/44/bin/foo post-stop
TimeoutStopSec=30
%[2]s

[Install]
WantedBy=multi-user.target
`
	expectedServiceAppWrapper     = fmt.Sprintf(expectedServiceWrapperFmt, "After=snapd.frameworks.target\nRequires=snapd.frameworks.target", "Type=simple\n", arch.UbuntuArchitecture())
	expectedServiceFmkWrapper     = fmt.Sprintf(expectedServiceWrapperFmt, "Before=snapd.frameworks.target\nAfter=snapd.frameworks-pre.target\nRequires=snapd.frameworks-pre.target", "Type=dbus\nBusName=foo.bar.baz", arch.UbuntuArchitecture())
	expectedSocketUsingWrapper    = fmt.Sprintf(expectedServiceWrapperFmt, "After=snapd.frameworks.target snap.xkcd-webserver.xkcd-webserver.socket\nRequires=snapd.frameworks.target snap.xkcd-webserver.xkcd-webserver.socket", "Type=simple\n", arch.UbuntuArchitecture())
	expectedTypeForkingFmkWrapper = fmt.Sprintf(expectedServiceWrapperFmt, "After=snapd.frameworks.target\nRequires=snapd.frameworks.target", "Type=forking\n", arch.UbuntuArchitecture())
)

const expectedServiceFmt = `[Unit]
# Auto-generated, DO NO EDIT
Description=Service for snap application snap.app
%s
X-Snappy=yes

[Service]
ExecStart=/usr/bin/ubuntu-core-launcher snap.snap.app snap.snap.app /snap/snap/44/bin/start
Restart=on-failure
WorkingDirectory=/var/snap/snap/44
Environment="SNAP=/snap/snap/44" "SNAP_DATA=/var/snap/snap/44" "SNAP_NAME=snap" "SNAP_VERSION=1.0" "SNAP_REVISION=44" "SNAP_ARCH=%[3]s" "SNAP_LIBRARY_PATH=/var/lib/snapd/lib/gl:" "SNAP_USER_DATA=/root/snap/snap/44"
ExecStop=/usr/bin/ubuntu-core-launcher snap.snap.app snap.snap.app /snap/snap/44/bin/stop
ExecStopPost=/usr/bin/ubuntu-core-launcher snap.snap.app snap.snap.app /snap/snap/44/bin/stop --post
TimeoutStopSec=10
%[2]s

[Install]
WantedBy=multi-user.target
`

var (
	expectedAppService  = fmt.Sprintf(expectedServiceFmt, "After=snapd.frameworks.target\nRequires=snapd.frameworks.target", "Type=simple\n", arch.UbuntuArchitecture())
	expectedDbusService = fmt.Sprintf(expectedServiceFmt, "After=snapd.frameworks.target\nRequires=snapd.frameworks.target", "Type=dbus\nBusName=foo.bar.baz", arch.UbuntuArchitecture())
)

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceTypeForking(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "xkcd-webserver",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: 44},
		},
		Name:        "xkcd-webserver",
		Command:     "bin/foo start",
		Stop:        "bin/foo stop",
		PostStop:    "bin/foo post-stop",
		StopTimeout: timeout.DefaultTimeout,
		Daemon:      "forking",
	}

	generatedWrapper, err := generateSnapServicesFile(service)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expectedTypeForkingFmkWrapper)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceAppWrapper(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "xkcd-webserver",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: 44},
		},
		Name:        "xkcd-webserver",
		Command:     "bin/foo start",
		Stop:        "bin/foo stop",
		PostStop:    "bin/foo post-stop",
		StopTimeout: timeout.DefaultTimeout,
		Daemon:      "simple",
	}

	generatedWrapper, err := generateSnapServicesFile(service)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expectedServiceAppWrapper)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceRestart(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "xkcd-webserver",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: 44},
		},
		Name:        "xkcd-webserver",
		RestartCond: systemd.RestartOnAbort,
		Daemon:      "simple",
	}

	generatedWrapper, err := generateSnapServicesFile(service)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Matches, `(?ms).*^Restart=on-abort$.*`)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceWrapperWhitelist(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "xkcd-webserver",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: 44},
		},
		Name:        "xkcd-webserver",
		Command:     "bin/foo start\n",
		Stop:        "bin/foo stop",
		PostStop:    "bin/foo post-stop",
		StopTimeout: timeout.DefaultTimeout,
		Daemon:      "simple",
	}

	_, err := generateSnapServicesFile(service)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestRemovePackageServiceKills(c *C) {
	c.Skip("needs porting to new squashfs based snap activation!")

	// make Stop not work
	var sysdLog [][]string
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=active\n"), nil
	}
	yamlFile, err := makeInstalledMockSnap(`name: wat
version: 42
apps:
 wat:
   command: wat
   stop-timeout: 25
   daemon: forking
`, 11)

	c.Assert(err, IsNil)
	snap, err := NewInstalledSnap(yamlFile)
	c.Assert(err, IsNil)

	inter := &MockProgressMeter{}
	c.Check(removePackageServices(snap.Info(), inter), IsNil)
	c.Assert(len(inter.notified) > 0, Equals, true)
	c.Check(inter.notified[len(inter.notified)-1], Equals, "wat_wat_42.service refused to stop, killing.")
	c.Assert(len(sysdLog) >= 3, Equals, true)
	sd1 := sysdLog[len(sysdLog)-3]
	sd2 := sysdLog[len(sysdLog)-2]
	c.Check(sd1, DeepEquals, []string{"kill", "wat_wat_42.service", "-s", "TERM"})
	c.Check(sd2, DeepEquals, []string{"kill", "wat_wat_42.service", "-s", "KILL"})
}

func (s *SnapTestSuite) TestSnappyGenerateSnapSocket(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SideInfo: snap.SideInfo{
				OfficialName: "xkcd-webserver",
				Revision:     44,
			},
			Version: "0.3.4",
		},
		Name:         "xkcd-webserver",
		Command:      "bin/foo start",
		Socket:       true,
		ListenStream: "/var/run/docker.sock",
		SocketMode:   "0660",
		Daemon:       "simple",
	}

	content, err := generateSnapSocketFile(service)
	c.Assert(err, IsNil)
	c.Assert(content, Equals, `[Unit]
# Auto-generated, DO NO EDIT
Description=Socket for snap application xkcd-webserver.xkcd-webserver
PartOf=snap.xkcd-webserver.xkcd-webserver.service
X-Snappy=yes

[Socket]
ListenStream=/var/run/docker.sock
SocketMode=0660

[Install]
WantedBy=sockets.target
`)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceWithSocket(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SideInfo: snap.SideInfo{
				OfficialName: "xkcd-webserver",
				Revision:     44,
			},
			Version: "0.3.4",
		},
		Name:        "xkcd-webserver",
		Command:     "bin/foo start",
		Stop:        "bin/foo stop",
		PostStop:    "bin/foo post-stop",
		StopTimeout: timeout.DefaultTimeout,
		Socket:      true,
		Daemon:      "simple",
	}

	generatedWrapper, err := generateSnapServicesFile(service)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expectedSocketUsingWrapper)
}

func (s *SnapTestSuite) TestGenerateSnapSocketFile(c *C) {
	srv := &snap.AppInfo{
		Snap: &snap.Info{},
	}

	// no socket mode means 0660
	content, err := generateSnapSocketFile(srv)
	c.Assert(err, IsNil)
	c.Assert(content, Matches, "(?ms).*SocketMode=0660")

	// SocketMode itself is honored
	srv.SocketMode = "0600"
	content, err = generateSnapSocketFile(srv)
	c.Assert(err, IsNil)
	c.Assert(content, Matches, "(?ms).*SocketMode=0600")

}

func (s *SnapTestSuite) TestGenAppServiceFile(c *C) {
	yamlText := `
name: snap
version: 1.0
apps:
    app:
        command: bin/start
        stop-command: bin/stop
        post-stop-command: bin/stop --post
        stop-timeout: 10s
        daemon: simple
`
	snapInfo := snaptest.MockSnap(c, yamlText, &snap.SideInfo{Revision: 44})
	app := snapInfo.Apps["app"]

	c.Check(GenServiceFile(app), Equals, expectedAppService)
}

func (s *SnapTestSuite) TestGenAppServiceFileRestart(c *C) {
	yamlTextTemplate := `
name: snap
apps:
    app:
        restart-condition: %s
`
	for name, cond := range systemd.RestartMap {
		yamlText := fmt.Sprintf(yamlTextTemplate, cond)
		snapInfo := snaptest.MockSnap(c, yamlText, &snap.SideInfo{Revision: 44})
		app := snapInfo.Apps["app"]

		c.Check(GenServiceFile(app), Matches,
			`(?ms).*^Restart=`+name+`$.*`, Commentf(name))
	}
}

func (s *SnapTestSuite) TestGenServiceFileWithBusName(c *C) {
	yamlText := `
name: snap
version: 1.0
apps:
    app:
        command: bin/start
        stop-command: bin/stop
        post-stop-command: bin/stop --post
        stop-timeout: 10s
        bus-name: foo.bar.baz
        daemon: dbus
`
	snapInfo := snaptest.MockSnap(c, yamlText, &snap.SideInfo{Revision: 44})
	app := snapInfo.Apps["app"]

	c.Assert(GenServiceFile(app), Equals, expectedDbusService)
}
