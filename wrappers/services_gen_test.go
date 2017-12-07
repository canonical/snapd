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

	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/timeout"
	"github.com/snapcore/snapd/wrappers"
)

type servicesWrapperGenSuite struct{}

var _ = Suite(&servicesWrapperGenSuite{})

const expectedServiceFmt = `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application snap.app
Requires=%s-snap-44.mount
Wants=network-online.target
After=%s-snap-44.mount network-online.target
X-Snappy=yes

[Service]
ExecStart=/usr/bin/snap run snap.app
SyslogIdentifier=snap.app
Restart=%s
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
	mountUnitPrefix = strings.Replace(dirs.SnapMountDir[1:], "/", "-", -1)
)

var (
	expectedAppService     = fmt.Sprintf(expectedServiceFmt, mountUnitPrefix, mountUnitPrefix, "on-failure", "simple\n\n")
	expectedDbusService    = fmt.Sprintf(expectedServiceFmt, mountUnitPrefix, mountUnitPrefix, "on-failure", "dbus\n\nBusName=foo.bar.baz")
	expectedOneshotService = fmt.Sprintf(expectedServiceFmt, mountUnitPrefix, mountUnitPrefix, "no", "oneshot\nRemainAfterExit=yes\n")
)

var (
	expectedServiceWrapperFmt = `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application xkcd-webserver.xkcd-webserver
Requires=%s-xkcd\x2dwebserver-44.mount
Wants=network-online.target
After=%s-xkcd\x2dwebserver-44.mount network-online.target
X-Snappy=yes

[Service]
ExecStart=/usr/bin/snap run xkcd-webserver
SyslogIdentifier=xkcd-webserver.xkcd-webserver
Restart=on-failure
WorkingDirectory=/var/snap/xkcd-webserver/44
ExecStop=/usr/bin/snap run --command=stop xkcd-webserver
ExecReload=/usr/bin/snap run --command=reload xkcd-webserver
ExecStopPost=/usr/bin/snap run --command=post-stop xkcd-webserver
TimeoutStopSec=30
Type=%s
%s
`
	expectedTypeForkingWrapper = fmt.Sprintf(expectedServiceWrapperFmt, mountUnitPrefix, mountUnitPrefix, "forking", "\n\n\n\n[Install]\nWantedBy=multi-user.target\n")
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
	c.Check(string(generatedWrapper), Equals, expectedAppService)
}

func (s *servicesWrapperGenSuite) TestGenerateSnapServiceFileRestart(c *C) {
	yamlTextTemplate := `
name: snap
apps:
    app:
        restart-condition: %s
`
	for name, cond := range snap.RestartMap {
		yamlText := fmt.Sprintf(yamlTextTemplate, cond)

		info, err := snap.InfoFromSnapYaml([]byte(yamlText))
		c.Assert(err, IsNil)
		info.Revision = snap.R(44)
		app := info.Apps["app"]

		generatedWrapper, err := wrappers.GenerateSnapServiceFile(app)
		c.Assert(err, IsNil)
		wrapperText := string(generatedWrapper)
		if cond == snap.RestartNever {
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
	c.Assert(string(generatedWrapper), Equals, expectedTypeForkingWrapper)
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

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(app)
	c.Assert(err, IsNil)

	c.Assert(string(generatedWrapper), Equals, expectedDbusService)
}

func (s *servicesWrapperGenSuite) TestGenOneshotServiceFile(c *C) {

	info := snaptest.MockInfo(c, `
name: snap
version: 1.0
apps:
    app:
        command: bin/start
        stop-command: bin/stop
        reload-command: bin/reload
        post-stop-command: bin/stop --post
        stop-timeout: 10s
        daemon: oneshot
`, &snap.SideInfo{Revision: snap.R(44)})

	app := info.Apps["app"]

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(app)
	c.Assert(err, IsNil)

	c.Assert(string(generatedWrapper), Equals, expectedOneshotService)
}

func (s *servicesWrapperGenSuite) TestGenerateSnapServiceWithSockets(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "xkcd-webserver",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: snap.R(44)},
		},
		Name:    "xkcd-webserver",
		Command: "bin/foo start",
		Daemon:  "simple",
		Plugs:   map[string]*snap.PlugInfo{"network-bind": {}},
		Sockets: map[string]*snap.SocketInfo{
			"sock1": {
				Name:         "sock1",
				ListenStream: "$SNAP_DATA/sock1.socket",
				SocketMode:   0666,
			},
		},
	}

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(service)
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(generatedWrapper), "[Install]"), Equals, false)
	c.Assert(strings.Contains(string(generatedWrapper), "WantedBy=multi-user.target"), Equals, false)
}

func (s *servicesWrapperGenSuite) TestServiceAfterBefore(c *C) {
	const expectedServiceFmt = `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application snap.app
Requires=%s-snap-44.mount
Wants=network-online.target
After=%s-snap-44.mount network-online.target snap.snap.bar.service snap.snap.zed.service
Before=snap.snap.foo.service
X-Snappy=yes

[Service]
ExecStart=/usr/bin/snap run snap.app
SyslogIdentifier=snap.app
Restart=%s
WorkingDirectory=/var/snap/snap/44



TimeoutStopSec=30
Type=%s


[Install]
WantedBy=multi-user.target

`

	expectedService := fmt.Sprintf(expectedServiceFmt, mountUnitPrefix, mountUnitPrefix, "on-failure", "simple\n\n")
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "snap",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: snap.R(44)},
			Apps: map[string]*snap.AppInfo{
				"foo": &snap.AppInfo{
					Name:   "foo",
					Snap:   &snap.Info{SuggestedName: "snap"},
					Daemon: "forking",
				},
				"bar": &snap.AppInfo{
					Name:   "bar",
					Snap:   &snap.Info{SuggestedName: "snap"},
					Daemon: "forking",
				},
				"zed": &snap.AppInfo{
					Name:   "zed",
					Snap:   &snap.Info{SuggestedName: "snap"},
					Daemon: "forking",
				},
			},
		},
		Name:        "app",
		Command:     "bin/foo start",
		Daemon:      "simple",
		Before:      []string{"foo"},
		After:       []string{"bar", "zed"},
		StopTimeout: timeout.DefaultTimeout,
	}

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(service)
	c.Assert(err, IsNil)

	c.Logf("service: \n%v\n", string(generatedWrapper))
	c.Assert(string(generatedWrapper), Equals, expectedService)
}
