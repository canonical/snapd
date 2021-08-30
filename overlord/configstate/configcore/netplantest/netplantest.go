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
	conn.Export(netplanApi{server}, netplanObjectPath, netplanInterface)
	conn.Export(introspectApi{server}, netplanObjectPath, introspectInterface)

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

type introspectApi struct {
	server *NetplanServer
}

func (a introspectApi) Introspect() (out string, err *dbus.Error) {
	if a.server.err != nil {
		return "", a.server.err
	}

	// XXX: generate this
	var netplanDBusIntrospectXML = `
<!DOCTYPE node PUBLIC "-//freedesktop//DTD D-BUS Object Introspection 1.0//EN"
"http://www.freedesktop.org/standards/dbus/1.0/introspect.dtd">
<node>
 <interface name="org.freedesktop.DBus.Introspectable">
  <method name="Introspect">
   <arg name="data" type="s" direction="out"/>
  </method>
 </interface>
 <interface name="io.netplan.Netplan">
  <method name="Config">
   <arg type="o" direction="out"/>
   <annotation name="org.freedesktop.systemd1.Privileged" value="true"/>
  </method>
 </interface>
 <node name="config"/>
</node>`

	return netplanDBusIntrospectXML, nil
}

type netplanApi struct {
	server *NetplanServer
}

func (a netplanApi) Config() (dbus.ObjectPath, *dbus.Error) {
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
