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

package netplantest

import (
	"fmt"

	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/dbusutil"
)

const (
	netplanBusName    = "io.netplan.Netplan"
	netplanObjectPath = "/io/netplan/Netplan"
	netplanInterface  = "io.netplan.Netplan"

	netplanConfigInterface = "io.netplan.Netplan.Config"

	introspectInterface = "org.freedesktop.DBus.Introspectable"
)

type NetplanServer struct {
	conn *dbus.Conn
	err  *dbus.Error

	mockNetplanConfigYaml string
}

func NewNetplanServer(mockNetplanConfigYaml string) (*NetplanServer, error) {
	// we use a private bus for testing
	conn, err := dbusutil.SessionBusPrivate()
	if err != nil {
		return nil, err
	}

	server := &NetplanServer{
		conn:                  conn,
		mockNetplanConfigYaml: mockNetplanConfigYaml,
	}

	reply, err := conn.RequestName(netplanBusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		conn.Close()
		return nil, err
	}

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
	if _, err := server.conn.ReleaseName(netplanBusName); err != nil {
		return err
	}
	return server.conn.Close()
}

// SetError sets an error to be returned by the D-Bus interface.
//
// If not nil, all the netplanApi methods will return the provided error
// in place of performing their usual task.
func (server *NetplanServer) SetError(err *dbus.Error) {
	server.err = err
}

// netplanApiV1 implements the original netplan DBus API that is found
// in netplan 0.98. It can only do a global "Apply".
type netplanApiV1 struct {
	server *NetplanServer
}

func (a netplanApiV1) Apply() (bool, *dbus.Error) {
	if a.server.err != nil {
		return false, a.server.err
	}

	return true, nil
}

// netplanApiV2 implements the "Config/Get/Set/Try" API that is found
// in netplan 0.101-0ubuntu3.
type netplanApiV2 struct {
	netplanApiV1
}

func (a netplanApiV2) Config() (dbus.ObjectPath, *dbus.Error) {
	if a.server.err != nil {
		return dbus.ObjectPath(""), a.server.err
	}
	path := dbus.ObjectPath("/io/netplan/Netplan/config/WFIU80")
	a.server.conn.Export(netplanConfigApi{a.server, path}, path, netplanConfigInterface)

	return path, nil
}

type netplanConfigApi struct {
	server *NetplanServer

	path dbus.ObjectPath
}

func (c netplanConfigApi) Get() (string, *dbus.Error) {
	return c.server.mockNetplanConfigYaml, nil
}

// TODO: implement Set/Try once we have "write" support
