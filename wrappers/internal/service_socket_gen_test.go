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

package internal_test

import (
	"fmt"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	_ "github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/wrappers/internal"
)

type serviceSocketUnitGenSuite struct {
	testutil.BaseTest
}

var _ = Suite(&serviceSocketUnitGenSuite{})

func (s *serviceSocketUnitGenSuite) TestGenerateSnapServiceWithSockets(c *C) {
	const sock1ExpectedFmt = `[Unit]
# Auto-generated, DO NOT EDIT
Description=Socket sock1 for snap application some-snap.app
Requires=%s-some\x2dsnap-44.mount
After=%s-some\x2dsnap-44.mount
X-Snappy=yes

[Socket]
Service=snap.some-snap.app.service
FileDescriptorName=sock1
ListenStream=%s/sock1.socket
SocketMode=0666

[Install]
WantedBy=sockets.target
`
	const sock2ExpectedFmt = `[Unit]
# Auto-generated, DO NOT EDIT
Description=Socket sock2 for snap application some-snap.app
Requires=%s-some\x2dsnap-44.mount
After=%s-some\x2dsnap-44.mount
X-Snappy=yes

[Socket]
Service=snap.some-snap.app.service
FileDescriptorName=sock2
ListenStream=%s/sock2.socket

[Install]
WantedBy=sockets.target
`

	si := &snap.Info{
		SuggestedName: "some-snap",
		Version:       "1.0",
		SideInfo:      snap.SideInfo{Revision: snap.R(44)},
	}
	service := &snap.AppInfo{
		Snap:        si,
		Name:        "app",
		Command:     "bin/foo start",
		Daemon:      "simple",
		DaemonScope: snap.SystemDaemon,
		Plugs:       map[string]*snap.PlugInfo{"network-bind": {Interface: "network-bind"}},
		Sockets: map[string]*snap.SocketInfo{
			"sock1": {
				Name:         "sock1",
				ListenStream: "$SNAP_DATA/sock1.socket",
				SocketMode:   0666,
			},
			"sock2": {
				Name:         "sock2",
				ListenStream: "$SNAP_DATA/sock2.socket",
			},
		},
	}
	service.Sockets["sock1"].App = service
	service.Sockets["sock2"].App = service

	sock1Expected := fmt.Sprintf(sock1ExpectedFmt, mountUnitPrefix, mountUnitPrefix, si.DataDir())
	sock2Expected := fmt.Sprintf(sock2ExpectedFmt, mountUnitPrefix, mountUnitPrefix, si.DataDir())

	generatedWrapper := mylog.Check2(internal.GenerateSnapServiceUnitFile(service, nil))

	c.Assert(strings.Contains(string(generatedWrapper), "[Install]"), Equals, false)
	c.Assert(strings.Contains(string(generatedWrapper), "WantedBy=multi-user.target"), Equals, false)

	generatedSockets := mylog.Check2(internal.GenerateSnapSocketUnitFiles(service))

	c.Assert(generatedSockets, HasLen, 2)
	c.Assert(generatedSockets, DeepEquals, map[string][]byte{
		"sock1": []byte(sock1Expected),
		"sock2": []byte(sock2Expected),
	})
}
