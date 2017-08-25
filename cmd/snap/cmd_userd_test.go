// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package main_test

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/godbus/dbus"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/osutil"
)

type UserdSuite struct {
	BaseSnapSuite

	tmpdir           string
	dbusDaemon       *exec.Cmd
	oldSessionBusEnv string
}

var _ = Suite(&UserdSuite{})

func (s *UserdSuite) SetUpSuite(c *C) {
	if !osutil.ExecutableExists("dbus-daemon") {
		c.Skip("cannot run test without dbus-launch")
		return
	}

	s.tmpdir = c.MkDir()

	s.dbusDaemon = exec.Command("dbus-daemon", "--session", fmt.Sprintf("--address=unix:%s/user_bus_socket", s.tmpdir))
	err := s.dbusDaemon.Start()
	c.Assert(err, IsNil)
	s.oldSessionBusEnv = os.Getenv("DBUS_SESSION_BUS_ADDRESS")
}

func (s *UserdSuite) TearDownSuite(c *C) {
	os.Setenv("DBUS_SESSION_BUS_ADDRESS", s.oldSessionBusEnv)
	err := s.dbusDaemon.Process.Kill()
	c.Assert(err, IsNil)
}

func (s *UserdSuite) TestUserdBadCommandline(c *C) {
	_, err := snap.Parser().ParseArgs([]string{"userd", "extra-arg"})
	c.Assert(err, ErrorMatches, "too many arguments for command")
}

func (s *UserdSuite) TestUserd(c *C) {
	go func() {
		defer func() {
			me, err := os.FindProcess(os.Getpid())
			c.Assert(err, IsNil)
			me.Signal(syscall.SIGUSR1)
		}()

		session, err := dbus.SessionBus()
		c.Assert(err, IsNil)
		needle := "io.snapcraft.Launcher"
		for i := 0; i < 10; i++ {
			for _, objName := range session.Names() {
				if objName == needle {
					return
				}
				time.Sleep(1 * time.Second)
			}

		}
		c.Fatalf("%s does not appeared on the bus", needle)
	}()

	rest, err := snap.Parser().ParseArgs([]string{"userd"})
	c.Assert(err, IsNil)
	c.Check(rest, DeepEquals, []string{})
	c.Check(s.Stdout(), Equals, "Exiting on user defined signal 1.\n")
}
