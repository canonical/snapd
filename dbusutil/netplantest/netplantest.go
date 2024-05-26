// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

// netplantest provides a fake implementation of the netplan dbus API for
// testing. Unlike the real netplan-dbus it uses the session bus but that
// is good enough for the testing. See configcore/netplan_test.go for
// example usage.
package netplantest

import (
	"fmt"
	"sync"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/dbusutil"
)

const (
	netplanBusName    = "io.netplan.Netplan"
	netplanObjectPath = "/io/netplan/Netplan"
	netplanInterface  = "io.netplan.Netplan"

	netplanConfigInterface = "io.netplan.Netplan.Config"
)

type NetplanServer struct {
	conn *dbus.Conn
	sync.Mutex

	MockNetplanConfigYaml string

	ConfigErr *dbus.Error

	ConfigApiGetCalls int
	ConfigApiGetErr   *dbus.Error

	ConfigApiSetCalls []string
	ConfigApiSetRet   bool
	ConfigApiSetErr   *dbus.Error

	ConfigApiApplyCalls int
	ConfigApiApplyRet   bool
	ConfigApiApplyErr   *dbus.Error

	ConfigApiTryCalls int
	ConfigApiTryRet   bool
	ConfigApiTryErr   *dbus.Error

	ConfigApiCancelCalls int
	ConfigApiCancelRet   bool
	ConfigApiCancelErr   *dbus.Error
}

func NewNetplanServer(mockNetplanConfigYaml string) (*NetplanServer, error) {
	// we use a private bus for testing
	conn := mylog.Check2(dbusutil.SessionBusPrivate())

	server := &NetplanServer{
		conn:                  conn,
		MockNetplanConfigYaml: mockNetplanConfigYaml,
	}

	reply := mylog.Check2(conn.RequestName(netplanBusName, dbus.NameFlagDoNotQueue))

	if reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return nil, fmt.Errorf("cannot obtain bus name %q", netplanBusName)
	}
	return server, nil
}

func (server *NetplanServer) ExportApiV1() {
	// netplanApiV1 implements the original netplan DBus API that is found
	// in netplan 0.98. It can only do a global "Apply".
	server.conn.Export(netplanApiV1{server}, netplanObjectPath, netplanInterface)
}

func (server *NetplanServer) ExportApiV2() {
	// netplanApiV2 implements the "Config/Get/Set/Try" API that is found
	// in netplan 0.101-0ubuntu3.
	server.conn.Export(netplanApiV2{netplanApiV1{server}}, netplanObjectPath, netplanInterface)
}

func (server *NetplanServer) Stop() error {
	mylog.Check2(server.conn.ReleaseName(netplanBusName))

	return server.conn.Close()
}

func (server *NetplanServer) WithLocked(f func()) {
	server.Lock()
	defer server.Unlock()

	f()
}

// netplanApiV1 implements the original netplan DBus API that is found
// in netplan 0.98. It can only do a global "Apply".
type netplanApiV1 struct {
	server *NetplanServer
}

func (a netplanApiV1) Apply() (bool, *dbus.Error) {
	a.server.Lock()
	defer a.server.Unlock()

	return true, a.server.ConfigApiApplyErr
}

// netplanApiV2 implements the "Config/Get/Set/Try" API that is found
// in netplan 0.101-0ubuntu3.
type netplanApiV2 struct {
	netplanApiV1
}

func (a netplanApiV2) Config() (dbus.ObjectPath, *dbus.Error) {
	path := dbus.ObjectPath("/io/netplan/Netplan/config/WFIU80")
	a.server.conn.Export(netplanConfigApi{a.server, path}, path, netplanConfigInterface)

	return path, a.server.ConfigErr
}

type netplanConfigApi struct {
	server *NetplanServer

	path dbus.ObjectPath
}

func (c netplanConfigApi) Get() (string, *dbus.Error) {
	c.server.Lock()
	defer c.server.Unlock()

	c.server.ConfigApiGetCalls++
	return c.server.MockNetplanConfigYaml, c.server.ConfigApiGetErr
}

func (c netplanConfigApi) Set(value, originHint string) (bool, *dbus.Error) {
	c.server.Lock()
	defer c.server.Unlock()

	c.server.ConfigApiSetCalls = append(c.server.ConfigApiSetCalls, fmt.Sprintf("%s/%s", value, originHint))
	return c.server.ConfigApiSetRet, c.server.ConfigApiSetErr
}

func (c netplanConfigApi) Apply() (bool, *dbus.Error) {
	c.server.Lock()
	defer c.server.Unlock()

	c.server.ConfigApiApplyCalls++
	return c.server.ConfigApiApplyRet, c.server.ConfigApiApplyErr
}

func (c netplanConfigApi) Cancel() (bool, *dbus.Error) {
	c.server.Lock()
	defer c.server.Unlock()

	c.server.ConfigApiCancelCalls++
	return c.server.ConfigApiCancelRet, c.server.ConfigApiCancelErr
}

func (c netplanConfigApi) Try(timeout int) (bool, *dbus.Error) {
	c.server.Lock()
	defer c.server.Unlock()

	c.server.ConfigApiTryCalls++
	return c.server.ConfigApiTryRet, c.server.ConfigApiTryErr
}
