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
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/godbus/dbus"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dbusutil"
)

// DBusTest provides a separate dbus session bus for running tests
type DBusTest struct {
	tmpdir           string
	dbusDaemon       *exec.Cmd
	oldSessionBusEnv string

	// the dbus.Conn to the session bus that tests can use
	SessionBus *dbus.Conn
}

// sessionBusConfigTemplate is a minimal session dbus daemon
// configuration template for use in the unit tests. In comparison to
// the typical disto session config, it contains no <servicedir>
// directives to avoid activating services installed on the test
// system.
const sessionBusConfigTemplate = `<busconfig>
  <type>session</type>
  <listen>unix:path=%s/user_bus_socket</listen>
  <auth>EXTERNAL</auth>
  <policy context="default">
    <!-- Allow everything to be sent -->
    <allow send_destination="*" eavesdrop="true"/>
    <!-- Allow everything to be received -->
    <allow eavesdrop="true"/>
    <!-- Allow anyone to own anything -->
    <allow own="*"/>
  </policy>
</busconfig>
`

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
	configFile := filepath.Join(s.tmpdir, "session.conf")
	err := ioutil.WriteFile(configFile, []byte(fmt.Sprintf(sessionBusConfigTemplate, s.tmpdir)), 0644)
	c.Assert(err, IsNil)
	s.dbusDaemon = exec.Command("dbus-daemon", "--print-address", fmt.Sprintf("--config-file=%s", configFile))
	s.dbusDaemon.Stderr = os.Stderr
	pout, err := s.dbusDaemon.StdoutPipe()
	c.Assert(err, IsNil)
	err = s.dbusDaemon.Start()
	c.Assert(err, IsNil)

	scanner := bufio.NewScanner(pout)
	scanner.Scan()
	c.Assert(scanner.Err(), IsNil)
	s.oldSessionBusEnv = os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	os.Setenv("DBUS_SESSION_BUS_ADDRESS", scanner.Text())

	s.SessionBus, err = dbusutil.SessionBusPrivate()
	c.Assert(err, IsNil)
}

func (s *DBusTest) TearDownSuite(c *C) {
	if s.SessionBus != nil {
		s.SessionBus.Close()
	}

	os.Setenv("DBUS_SESSION_BUS_ADDRESS", s.oldSessionBusEnv)
	if s.dbusDaemon != nil && s.dbusDaemon.Process != nil {
		err := s.dbusDaemon.Process.Kill()
		c.Assert(err, IsNil)
		err = s.dbusDaemon.Wait() // do cleanup
		c.Assert(err, ErrorMatches, `(?i)signal: killed`)
	}
}

func (s *DBusTest) SetUpTest(c *C)    {}
func (s *DBusTest) TearDownTest(c *C) {}

func DBusGetConnectionUnixProcessID(conn *dbus.Conn, name string) (pid int, err error) {
	obj := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")

	err = obj.Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, name).Store(&pid)
	return pid, err
}
