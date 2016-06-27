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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timeout"
	"github.com/snapcore/snapd/wrappers"
)

type servicesWrapperGenSuite struct{}

var _ = Suite(&servicesWrapperGenSuite{})

const expectedServiceFmt = `[Unit]
# Auto-generated, DO NO EDIT
Description=Service for snap application snap.app
%s
X-Snappy=yes

[Service]
ExecStart=/usr/bin/snap run snap.app
Restart=on-failure
WorkingDirectory=/var/snap/snap/44
ExecStop=/usr/bin/snap run --command=stop snap.app
ExecStopPost=/usr/bin/snap run --command=post-stop snap.app
TimeoutStopSec=10
%[2]s

[Install]
WantedBy=multi-user.target
`

var (
	expectedAppService  = fmt.Sprintf(expectedServiceFmt, "After=snapd.frameworks.target\nRequires=snapd.frameworks.target", "Type=simple\n", arch.UbuntuArchitecture())
	expectedDbusService = fmt.Sprintf(expectedServiceFmt, "After=snapd.frameworks.target\nRequires=snapd.frameworks.target", "Type=dbus\nBusName=foo.bar.baz", arch.UbuntuArchitecture())
)

var (
	expectedServiceWrapperFmt = `[Unit]
# Auto-generated, DO NO EDIT
Description=Service for snap application xkcd-webserver.xkcd-webserver
%s
X-Snappy=yes

[Service]
ExecStart=/usr/bin/snap run xkcd-webserver
Restart=on-failure
WorkingDirectory=/var/snap/xkcd-webserver/44
ExecStop=/usr/bin/snap run --command=stop xkcd-webserver
ExecStopPost=/usr/bin/snap run --command=post-stop xkcd-webserver
TimeoutStopSec=30
%[2]s

[Install]
WantedBy=multi-user.target
`
	expectedSocketUsingWrapper = fmt.Sprintf(expectedServiceWrapperFmt, "After=snapd.frameworks.target snap.xkcd-webserver.xkcd-webserver.socket\nRequires=snapd.frameworks.target snap.xkcd-webserver.xkcd-webserver.socket", "Type=simple\n", arch.UbuntuArchitecture())
	expectedTypeForkingWrapper = fmt.Sprintf(expectedServiceWrapperFmt, "After=snapd.frameworks.target\nRequires=snapd.frameworks.target", "Type=forking\n", arch.UbuntuArchitecture())
)

func (s *servicesWrapperGenSuite) TestGenerateSnapServiceFile(c *C) {
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
	info, err := snap.InfoFromSnapYaml([]byte(yamlText))
	c.Assert(err, IsNil)
	info.Revision = snap.R(44)
	app := info.Apps["app"]

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(app)
	c.Assert(err, IsNil)
	c.Check(generatedWrapper, Equals, expectedAppService)
}

func (s *servicesWrapperGenSuite) TestGenerateSnapServiceFileRestart(c *C) {
	yamlTextTemplate := `
name: snap
apps:
    app:
        restart-condition: %s
`
	for name, cond := range systemd.RestartMap {
		yamlText := fmt.Sprintf(yamlTextTemplate, cond)

		info, err := snap.InfoFromSnapYaml([]byte(yamlText))
		c.Assert(err, IsNil)
		info.Revision = snap.R(44)
		app := info.Apps["app"]

		wrapperText, err := wrappers.GenerateSnapServiceFile(app)
		c.Assert(err, IsNil)
		c.Check(wrapperText, Matches,
			`(?ms).*^Restart=`+name+`$.*`, Commentf(name))
	}
}

func (s *servicesWrapperGenSuite) TestGenerateSnapServiceFileTypeForking(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "xkcd-webserver",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: snap.R(44)},
		},
		Name:            "xkcd-webserver",
		Command:         "bin/foo start",
		StopCommand:     "bin/foo stop",
		PostStopCommand: "bin/foo post-stop",
		StopTimeout:     timeout.DefaultTimeout,
		Daemon:          "forking",
	}

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(service)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expectedTypeForkingWrapper)
}

func (s *servicesWrapperGenSuite) TestGenerateSnapServiceFileIllegalChars(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "xkcd-webserver",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: snap.R(44)},
		},
		Name:            "xkcd-webserver",
		Command:         "bin/foo start\n",
		StopCommand:     "bin/foo stop",
		PostStopCommand: "bin/foo post-stop",
		StopTimeout:     timeout.DefaultTimeout,
		Daemon:          "simple",
	}

	_, err := wrappers.GenerateSnapServiceFile(service)
	c.Assert(err, NotNil)
}

func (s *servicesWrapperGenSuite) TestGenServiceFileWithBusName(c *C) {

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

	info, err := snap.InfoFromSnapYaml([]byte(yamlText))
	c.Assert(err, IsNil)
	info.Revision = snap.R(44)
	app := info.Apps["app"]

	wrapperText, err := wrappers.GenerateSnapServiceFile(app)
	c.Assert(err, IsNil)

	c.Assert(wrapperText, Equals, expectedDbusService)
}

func (s *servicesWrapperGenSuite) TestGenerateSnapSocketFile(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SideInfo: snap.SideInfo{
				OfficialName: "xkcd-webserver",
				Revision:     snap.R(44),
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

	content, err := wrappers.GenerateSnapSocketFile(service)
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

func (s *servicesWrapperGenSuite) TestGenerateSnapSocketFileIllegalChars(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SideInfo: snap.SideInfo{
				OfficialName: "xkcd-webserver",
				Revision:     snap.R(44),
			},
			Version: "0.3.4",
		},
		Name:         "xkcd-webserver",
		Command:      "bin/foo start",
		Socket:       true,
		ListenStream: "/var/run/docker!sock",
		SocketMode:   "0660",
		Daemon:       "simple",
	}

	_, err := wrappers.GenerateSnapSocketFile(service)
	c.Assert(err, NotNil)
}

func (s *servicesWrapperGenSuite) TestGenerateSnapServiceFileWithSocket(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SideInfo: snap.SideInfo{
				OfficialName: "xkcd-webserver",
				Revision:     snap.R(44),
			},
			Version: "0.3.4",
		},
		Name:            "xkcd-webserver",
		Command:         "bin/foo start",
		StopCommand:     "bin/foo stop",
		PostStopCommand: "bin/foo post-stop",
		StopTimeout:     timeout.DefaultTimeout,
		Socket:          true,
		Daemon:          "simple",
	}

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(service)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expectedSocketUsingWrapper)
}

func (s *servicesWrapperGenSuite) TestGenerateSnapSocketFileMode(c *C) {
	srv := &snap.AppInfo{
		Name: "foo",
		Snap: &snap.Info{},
	}

	// no socket mode means 0660
	content, err := wrappers.GenerateSnapSocketFile(srv)
	c.Assert(err, IsNil)
	c.Assert(content, Matches, "(?ms).*SocketMode=0660")

	// SocketMode itself is honored
	srv.SocketMode = "0600"
	content, err = wrappers.GenerateSnapSocketFile(srv)
	c.Assert(err, IsNil)
	c.Assert(content, Matches, "(?ms).*SocketMode=0600")

}
