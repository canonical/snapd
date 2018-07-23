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
	_, err := snap.Parser().ParseArgs([]string{"userd", "extra-arg"})
	c.Assert(err, ErrorMatches, "too many arguments for command")
}

func (s *userdSuite) TestUserd(c *C) {
	go func() {
		defer func() {
			me, err := os.FindProcess(os.Getpid())
			c.Assert(err, IsNil)
			me.Signal(syscall.SIGUSR1)
		}()

		needle := "io.snapcraft.Launcher"
		for i := 0; i < 10; i++ {
			for _, objName := range s.SessionBus.Names() {
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
