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
	"os"
	"strings"
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/testutil"
)

type userdSuite struct {
	BaseSnapSuite
	testutil.DBusTest

	restoreLogger func()
}

var _ = Suite(&userdSuite{})

func (s *userdSuite) SetUpTest(c *C) {
	s.BaseSnapSuite.SetUpTest(c)
	s.DBusTest.SetUpTest(c)

	_, s.restoreLogger = logger.MockLogger()
}

func (s *userdSuite) TearDownTest(c *C) {
	s.BaseSnapSuite.TearDownTest(c)
	s.DBusTest.TearDownTest(c)

	s.restoreLogger()
}

func (s *userdSuite) TestUserdBadCommandline(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"userd", "extra-arg"})
	c.Assert(err, ErrorMatches, "too many arguments for command")
}

func (s *userdSuite) TestUserdDBus(c *C) {
	go func() {
		myPid := os.Getpid()
		defer func() {
			me, err := os.FindProcess(myPid)
			c.Assert(err, IsNil)
			me.Signal(syscall.SIGUSR1)
		}()

		names := map[string]bool{
			"io.snapcraft.Launcher": false,
			"io.snapcraft.Settings": false,
		}
		for i := 0; i < 1000; i++ {
			seenCount := 0
			for name, seen := range names {
				if seen {
					seenCount++
					continue
				}
				pid, err := testutil.DBusGetConnectionUnixProcessID(s.SessionBus, name)
				c.Logf("name: %v pid: %v err: %v", name, pid, err)
				if pid == myPid {
					names[name] = true
					seenCount++
				}
			}
			if seenCount == len(names) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		c.Fatalf("not all names have appeared on the bus: %v", names)
	}()

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"userd"})
	c.Assert(err, IsNil)
	c.Check(rest, DeepEquals, []string{})
	c.Check(strings.ToLower(s.Stdout()), Equals, "exiting on user defined signal 1.\n")
}
