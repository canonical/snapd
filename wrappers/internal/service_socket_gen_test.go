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

	generatedWrapper, err := internal.GenerateSnapServiceUnitFile(service, nil)
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(generatedWrapper), "[Install]"), Equals, false)
	c.Assert(strings.Contains(string(generatedWrapper), "WantedBy=multi-user.target"), Equals, false)

	generatedSockets, err := internal.GenerateSnapSocketUnitFiles(service)
	c.Assert(err, IsNil)
	c.Assert(generatedSockets, HasLen, 2)
	c.Assert(generatedSockets, DeepEquals, map[string][]byte{
		"sock1": []byte(sock1Expected),
		"sock2": []byte(sock2Expected),
	})
}

func makeTestSnapInfo(instanceKey string) *snap.Info {
	return &snap.Info{
		SuggestedName: "some-snap",
		InstanceKey:   instanceKey,
		Version:       "1.0",
		SideInfo:      snap.SideInfo{Revision: snap.R(44)},
	}
}

func makeTestServiceWithSingleSocket(si *snap.Info, listenStream string) *snap.AppInfo {
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
				ListenStream: listenStream,
			},
		},
	}
	service.Sockets["sock1"].App = service
	return service
}

func (s *serviceSocketUnitGenSuite) TestGenerateSnapServiceWithAbstractSocketParallelInstance(c *C) {
	instanceKey := "inst1"
	listenStream := "@snap.some-snap.my.socket"
	si := makeTestSnapInfo(instanceKey)
	service := makeTestServiceWithSingleSocket(si, listenStream)

	generatedSockets, err := internal.GenerateSnapSocketUnitFiles(service)
	c.Assert(err, IsNil)
	c.Assert(generatedSockets, HasLen, 1)
	c.Assert(string(generatedSockets["sock1"]), testutil.Contains, "ListenStream=@snap.some-snap_inst1.my.socket\n")
}

func (s *serviceSocketUnitGenSuite) TestGenerateSnapServiceWithAbstractSocketNoInstanceKey(c *C) {
	instanceKey := ""
	listenStream := "@snap.some-snap.my.socket"
	si := makeTestSnapInfo(instanceKey)
	service := makeTestServiceWithSingleSocket(si, listenStream)

	generatedSockets, err := internal.GenerateSnapSocketUnitFiles(service)
	c.Assert(err, IsNil)
	c.Assert(generatedSockets, HasLen, 1)
	c.Assert(string(generatedSockets["sock1"]), testutil.Contains, "ListenStream="+listenStream+"\n")
}

func (s *serviceSocketUnitGenSuite) TestGenerateSnapServiceWithAbstractSocketDoesNotExpandSnapVars(c *C) {
	instanceKey := "inst1"
	listenStream := "@snap.some-snap.$SNAP_DATA.$SNAP_COMMON.$XDG_RUNTIME_DIR"
	si := makeTestSnapInfo(instanceKey)
	service := makeTestServiceWithSingleSocket(si, listenStream)

	generatedSockets, err := internal.GenerateSnapSocketUnitFiles(service)
	c.Assert(err, IsNil)
	c.Assert(generatedSockets, HasLen, 1)
	expectedListenStream := strings.Replace(listenStream, "@snap.some-snap.", "@snap.some-snap_inst1.", 1)
	c.Assert(string(generatedSockets["sock1"]), testutil.Contains, "ListenStream="+expectedListenStream+"\n")
}

func (s *serviceSocketUnitGenSuite) TestGenerateSnapServiceWithAbstractSocketTooLongBeforeRemap(c *C) {
	instanceKey := ""
	prefix := "@snap.some-snap."
	listenStream := prefix + strings.Repeat("a", internal.MaxLenUnixAbstractSocketAddress-len(prefix)+1)
	si := makeTestSnapInfo(instanceKey)
	service := makeTestServiceWithSingleSocket(si, listenStream)

	generatedSockets, err := internal.GenerateSnapSocketUnitFiles(service)
	expectedErr := fmt.Sprintf(`cannot generate socket unit for socket %q: abstract socket address %q is too long \(%d bytes\), maximum is %d`, "sock1",
		listenStream, len(listenStream), internal.MaxLenUnixAbstractSocketAddress)
	c.Assert(err, ErrorMatches, expectedErr)
	c.Assert(generatedSockets, IsNil)
}

func (s *serviceSocketUnitGenSuite) TestGenerateSnapServiceWithAbstractSocketTooLongAfterRemap(c *C) {
	instanceKey := "inst1"
	prefix := "@snap.some-snap."
	listenStream := prefix + strings.Repeat("a", internal.MaxLenUnixAbstractSocketAddress-len(prefix))
	si := makeTestSnapInfo(instanceKey)
	service := makeTestServiceWithSingleSocket(si, listenStream)

	generatedSockets, err := internal.GenerateSnapSocketUnitFiles(service)
	renderedListenStream := strings.Replace(listenStream, prefix, "@snap.some-snap_inst1.", 1)
	expectedErr := fmt.Sprintf(`cannot generate socket unit for socket %q: abstract socket address %q is too long \(%d bytes\), maximum is %d`, "sock1",
		renderedListenStream, len(renderedListenStream), internal.MaxLenUnixAbstractSocketAddress)
	c.Assert(err, ErrorMatches, expectedErr)
	c.Assert(generatedSockets, IsNil)
}

func (s *serviceSocketUnitGenSuite) TestGenerateSnapServiceWithAbstractSocketMaxLenAfterRemap(c *C) {
	instanceKey := "inst1"
	prefixSnapName := "@snap.some-snap."
	prefixInstanceName := "@snap.some-snap_inst1."
	listenStream := prefixSnapName + strings.Repeat("a", internal.MaxLenUnixAbstractSocketAddress-len(prefixInstanceName))
	si := makeTestSnapInfo(instanceKey)
	service := makeTestServiceWithSingleSocket(si, listenStream)

	generatedSockets, err := internal.GenerateSnapSocketUnitFiles(service)
	c.Assert(err, IsNil)
	c.Assert(generatedSockets, HasLen, 1)

	renderedListenStream := strings.Replace(listenStream, prefixSnapName, prefixInstanceName, 1)
	c.Assert(len(renderedListenStream), Equals, internal.MaxLenUnixAbstractSocketAddress)
	c.Assert(string(generatedSockets["sock1"]), testutil.Contains, "ListenStream="+renderedListenStream+"\n")
}

func (s *serviceSocketUnitGenSuite) TestGenerateSnapServiceWithPathSocketTooLongBeforeExpansion(c *C) {
	instanceKey := ""
	prefix := "$SNAP_DATA/"
	listenStream := prefix + strings.Repeat("a", internal.MaxLenUnixPathSocketAddress-len(prefix)+1)
	si := makeTestSnapInfo(instanceKey)
	service := makeTestServiceWithSingleSocket(si, listenStream)

	generatedSockets, err := internal.GenerateSnapSocketUnitFiles(service)
	renderedListenStream := strings.Replace(listenStream, "$SNAP_DATA", si.DataDir(), 1)
	expectedErr := fmt.Sprintf(`cannot generate socket unit for socket %q: socket path %q is too long \(%d bytes\), maximum is %d`, "sock1",
		renderedListenStream, len(renderedListenStream), internal.MaxLenUnixPathSocketAddress)
	c.Assert(err, ErrorMatches, expectedErr)
	c.Assert(generatedSockets, IsNil)
}

func (s *serviceSocketUnitGenSuite) TestGenerateSnapServiceWithPathSocketTooLongAfterExpansion(c *C) {
	instanceKey := ""
	prefix := "$SNAP_DATA/"
	si := makeTestSnapInfo(instanceKey)
	listenStream := prefix + strings.Repeat("a", internal.MaxLenUnixPathSocketAddress-len(si.DataDir()))
	service := makeTestServiceWithSingleSocket(si, listenStream)

	generatedSockets, err := internal.GenerateSnapSocketUnitFiles(service)
	renderedListenStream := strings.Replace(listenStream, "$SNAP_DATA", si.DataDir(), 1)
	expectedErr := fmt.Sprintf(`cannot generate socket unit for socket %q: socket path %q is too long \(%d bytes\), maximum is %d`, "sock1",
		renderedListenStream, len(renderedListenStream), internal.MaxLenUnixPathSocketAddress)
	c.Assert(err, ErrorMatches, expectedErr)
	c.Assert(generatedSockets, IsNil)
}

func (s *serviceSocketUnitGenSuite) TestGenerateSnapServiceWithPathSocketMaxLenAfterExpansion(c *C) {
	instanceKey := ""
	prefix := "$SNAP_DATA/"
	si := makeTestSnapInfo(instanceKey)
	listenStream := prefix + strings.Repeat("a", internal.MaxLenUnixPathSocketAddress-len(si.DataDir())-1)
	service := makeTestServiceWithSingleSocket(si, listenStream)

	generatedSockets, err := internal.GenerateSnapSocketUnitFiles(service)
	c.Assert(err, IsNil)
	c.Assert(generatedSockets, HasLen, 1)

	renderedListenStream := strings.Replace(listenStream, "$SNAP_DATA", si.DataDir(), 1)
	c.Assert(len(renderedListenStream), Equals, internal.MaxLenUnixPathSocketAddress)
	c.Assert(string(generatedSockets["sock1"]), testutil.Contains, "ListenStream="+renderedListenStream+"\n")
}
