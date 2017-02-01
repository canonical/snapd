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
Requires=snap-snap-44.mount
Wants=network-online.target
After=snap-snap-44.mount network-online.target
X-Snappy=yes

[Service]
ExecStart=/usr/bin/snap run snap.app
Restart=on-failure
WorkingDirectory=/var/snap/snap/44
ExecStop=/usr/bin/snap run --command=stop snap.app
ExecReload=/usr/bin/snap run --command=reload snap.app
ExecStopPost=/usr/bin/snap run --command=post-stop snap.app
TimeoutStopSec=10
Type=%s

[Install]
WantedBy=multi-user.target
`

var (
	expectedAppService  = fmt.Sprintf(expectedServiceFmt, "simple\n")
	expectedDbusService = fmt.Sprintf(expectedServiceFmt, "dbus\nBusName=foo.bar.baz")
)

var (
	expectedServiceWrapperFmt = `[Unit]
# Auto-generated, DO NO EDIT
Description=Service for snap application xkcd-webserver.xkcd-webserver
Requires=snap-xkcd\x2dwebserver-44.mount
Wants=network-online.target
After=snap-xkcd\x2dwebserver-44.mount network-online.target
X-Snappy=yes

[Service]
ExecStart=/usr/bin/snap run xkcd-webserver
Restart=on-failure
WorkingDirectory=/var/snap/xkcd-webserver/44
ExecStop=/usr/bin/snap run --command=stop xkcd-webserver
ExecReload=/usr/bin/snap run --command=reload xkcd-webserver
ExecStopPost=/usr/bin/snap run --command=post-stop xkcd-webserver
TimeoutStopSec=30
Type=%s
%s
`
	expectedTypeForkingWrapper = fmt.Sprintf(expectedServiceWrapperFmt, "forking\n", "\n[Install]\nWantedBy=multi-user.target")
)

func (s *servicesWrapperGenSuite) TestGenerateSnapServiceFile(c *C) {
	yamlText := `
name: snap
version: 1.0
apps:
    app:
        command: bin/start
        stop-command: bin/stop
        reload-command: bin/reload
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
		if cond == systemd.RestartNever {
			c.Check(wrapperText, Matches,
				`(?ms).*^Restart=no$.*`, Commentf(name))
		} else {
			c.Check(wrapperText, Matches,
				`(?ms).*^Restart=`+name+`$.*`, Commentf(name))
		}
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
		ReloadCommand:   "bin/foo reload",
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
		ReloadCommand:   "bin/foo reload",
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
        reload-command: bin/reload
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
