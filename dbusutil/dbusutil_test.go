// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package dbusutil_test

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/dbusutil/dbustest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type dbusutilSuite struct {
	testutil.BaseTest
}

var _ = Suite(&dbusutilSuite{})

const envVar = "DBUS_SESSION_BUS_ADDRESS"

func (s *dbusutilSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	// Pretend we have an empty file system. This specifically makes
	// /run/user/*/ empty as well.
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	// Pretend that we don't have the environment variable with session bus
	// address.
	if value := os.Getenv(envVar); value != "" {
		os.Unsetenv(envVar)
		s.AddCleanup(func() { os.Setenv(envVar, value) })
	}
}

func (*dbusutilSuite) TestIsSessionBusLikelyPresentNothing(c *C) {
	c.Assert(dbusutil.IsSessionBusLikelyPresent(), Equals, false)
}

func (*dbusutilSuite) TestIsSessionBusLikelyPresentEnvVar(c *C) {
	os.Setenv(envVar, "address")

	c.Assert(dbusutil.IsSessionBusLikelyPresent(), Equals, true)
}

func (*dbusutilSuite) TestIsSessionBusLikelyPresentAddrFile(c *C) {
	f := fmt.Sprintf("%s/%d/dbus-session", dirs.XdgRuntimeDirBase, os.Getuid())
	c.Assert(os.MkdirAll(filepath.Dir(f), 0755), IsNil)
	c.Assert(os.WriteFile(f, []byte("address"), 0644), IsNil)

	c.Assert(dbusutil.IsSessionBusLikelyPresent(), Equals, true)
}

func (*dbusutilSuite) TestIsSessionBusLikelyPresentSocket(c *C) {
	f := fmt.Sprintf("%s/%d/bus", dirs.XdgRuntimeDirBase, os.Getuid())
	c.Assert(os.MkdirAll(filepath.Dir(f), 0755), IsNil)
	l := mylog.Check2(net.Listen("unix", f))

	defer l.Close()

	c.Assert(dbusutil.IsSessionBusLikelyPresent(), Equals, true)
}

func (*dbusutilSuite) TestSessionBusWithoutBus(c *C) {
	_ := mylog.Check2(dbusutil.SessionBus())
	c.Assert(err, ErrorMatches, "cannot find session bus")
}

func (*dbusutilSuite) TestMockOnlySessionBusAvailable(c *C) {
	stub := mylog.Check2(dbustest.StubConnection())

	defer stub.Close()
	restore := dbusutil.MockOnlySessionBusAvailable(stub)
	defer restore()

	conn := mylog.Check2(dbusutil.SessionBus())

	c.Check(conn, Equals, stub)

	c.Check(func() { dbusutil.SystemBus() }, PanicMatches, "DBus system bus should not have been used")
}

func (*dbusutilSuite) TestMockOnlySystemBusAvailable(c *C) {
	stub := mylog.Check2(dbustest.StubConnection())

	defer stub.Close()
	restore := dbusutil.MockOnlySystemBusAvailable(stub)
	defer restore()

	c.Check(func() { dbusutil.SessionBus() }, PanicMatches, "DBus session bus should not have been used")

	conn := mylog.Check2(dbusutil.SystemBus())

	c.Check(conn, Equals, stub)
}
