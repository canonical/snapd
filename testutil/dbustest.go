// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package testutil

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/godbus/dbus"

	. "gopkg.in/check.v1"
)

// DBusTest provides a separate dbus session bus for running tests
type DBusTest struct {
	tmpdir           string
	dbusDaemon       *exec.Cmd
	oldSessionBusEnv string

	// the dbus.Conn to the session bus that tests can use
	SessionBus *dbus.Conn
}

func (s *DBusTest) SetUpSuite(c *C) {
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		c.Skip(fmt.Sprintf("cannot run test without dbus-daemon: %s", err))
		return
	}
	if _, err := exec.LookPath("dbus-launch"); err != nil {
		c.Skip(fmt.Sprintf("cannot run test without dbus-launch: %s", err))
		return
	}

	s.tmpdir = c.MkDir()
	s.dbusDaemon = exec.Command("dbus-daemon", "--session", fmt.Sprintf("--address=unix:%s/user_bus_socket", s.tmpdir))
	err := s.dbusDaemon.Start()
	c.Assert(err, IsNil)
	s.oldSessionBusEnv = os.Getenv("DBUS_SESSION_BUS_ADDRESS")

	s.SessionBus, err = dbus.SessionBus()
	c.Assert(err, IsNil)
}

func (s *DBusTest) TearDownSuite(c *C) {
	os.Setenv("DBUS_SESSION_BUS_ADDRESS", s.oldSessionBusEnv)
	if s.dbusDaemon != nil && s.dbusDaemon.Process != nil {
		err := s.dbusDaemon.Process.Kill()
		c.Assert(err, IsNil)
	}

}

func (s *DBusTest) SetUpTest(c *C)    {}
func (s *DBusTest) TearDownTest(c *C) {}
