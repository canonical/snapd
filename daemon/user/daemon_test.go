// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package user_test

import (
	"time"

	"github.com/godbus/dbus"
	"github.com/snapcore/snapd/daemon/user"

	"fmt"

	. "gopkg.in/check.v1"
)

type daemonSuite struct {
	i      int
	args   [][]string
	errors []error
	outs   [][]byte
}

var _ = Suite(&daemonSuite{})

type exportData struct {
	iface interface{}
	name  string
	path  dbus.ObjectPath
}

type mockDBusConn struct {
	exportError      error
	exportData       []*exportData
	requestNameReply dbus.RequestNameReply
	requestNameError error
	requestNameData  struct {
		name  string
		flags dbus.RequestNameFlags
	}
	signalData struct {
		signals []chan<- *dbus.Signal
	}
}

func (c *mockDBusConn) Export(v interface{}, path dbus.ObjectPath, iface string) error {
	c.exportData = append(c.exportData, &exportData{iface: v, path: path, name: iface})
	return c.exportError
}

func (c *mockDBusConn) RequestName(name string, flags dbus.RequestNameFlags) (dbus.RequestNameReply, error) {
	c.requestNameData.name = name
	c.requestNameData.flags = flags
	return c.requestNameReply, c.requestNameError
}

func (c *mockDBusConn) Signal(ch chan<- *dbus.Signal) {
	c.signalData.signals = append(c.signalData.signals, ch)
}

func (c *mockDBusConn) sendTerminateSignal() {
	for _, ch := range c.signalData.signals {
		close(ch)
	}
}

func (c *mockDBusConn) Close() error {
	return nil
}

func (s *daemonSuite) TestStartsAndRequestsName(c *C) {
	conn := &mockDBusConn{
		requestNameReply: dbus.RequestNameReplyPrimaryOwner,
	}

	user.MockSessionBus(conn, nil)

	d, err := user.NewDaemon()
	c.Assert(err, IsNil)
	c.Assert(d, NotNil)

	d.Start()
	time.Sleep(time.Second * 1)
	conn.sendTerminateSignal()
	d.Stop()

	c.Assert(conn.requestNameData.name, Equals, "com.canonical.SafeLauncher")
	c.Assert(conn.requestNameData.flags, Equals, dbus.NameFlagDoNotQueue)

	c.Assert(len(conn.exportData), Equals, 2)
	c.Assert(conn.exportData[0].name, Equals, "com.canonical.SafeLauncher")
	c.Assert(conn.exportData[0].path, Equals, dbus.ObjectPath("/"))
	c.Assert(conn.exportData[0].iface, NotNil)
	c.Assert(conn.exportData[1].name, Equals, "org.freedesktop.DBus.Introspectable")
	c.Assert(conn.exportData[1].path, Equals, dbus.ObjectPath("/"))
	c.Assert(conn.exportData[1].iface, NotNil)
}

func (s *daemonSuite) TestStartupFailsWhenSessionBusIsNotAvailable(c *C) {
	user.MockSessionBus(nil, fmt.Errorf("Session bus is not available"))

	d, err := user.NewDaemon()
	c.Assert(err, IsNil)
	c.Assert(d, NotNil)

	d.Start()
	time.Sleep(time.Second * 1)
	err = d.Stop()
	c.Assert(err, ErrorMatches, "Session bus is not available")
}

func (s *daemonSuite) TestStartupFailsWhenNameRequestFails(c *C) {
	conn := &mockDBusConn{
		requestNameError: fmt.Errorf("Failed to request name"),
	}

	user.MockSessionBus(conn, nil)

	d, err := user.NewDaemon()
	c.Assert(err, IsNil)
	c.Assert(d, NotNil)

	d.Start()
	time.Sleep(time.Second * 1)
	d.Stop()

	c.Assert(conn.requestNameData.name, Equals, "com.canonical.SafeLauncher")
	c.Assert(conn.requestNameData.flags, Equals, dbus.NameFlagDoNotQueue)
	c.Assert(len(conn.exportData), Equals, 0)
}

func (s *daemonSuite) TestStartupFailsWhenNameIsAlreadyOwned(c *C) {
	conn := &mockDBusConn{
		requestNameReply: dbus.RequestNameReplyExists,
	}

	user.MockSessionBus(conn, nil)

	d, err := user.NewDaemon()
	c.Assert(err, IsNil)
	c.Assert(d, NotNil)

	d.Start()
	time.Sleep(time.Second * 1)
	d.Stop()

	c.Assert(conn.requestNameData.name, Equals, "com.canonical.SafeLauncher")
	c.Assert(conn.requestNameData.flags, Equals, dbus.NameFlagDoNotQueue)
	c.Assert(len(conn.exportData), Equals, 0)
}
