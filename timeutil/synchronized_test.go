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

package timeutil_test

import (
	"errors"
	"fmt"

	"github.com/godbus/dbus"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timeutil"
)

const (
	timedate1BusName    = "org.freedesktop.timedate1"
	timedate1ObjectPath = "/org/freedesktop/timedate1"
)

type mockTimedate1 struct {
	conn *dbus.Conn

	NTPSynchronized bool

	getPropertyCalled []string
}

func newMockTimedate1() (*mockTimedate1, error) {
	conn, err := dbusutil.SessionBusPrivate()
	if err != nil {
		return nil, err
	}

	server := &mockTimedate1{
		conn: conn,
	}

	reply, err := conn.RequestName(timedate1BusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		conn.Close()
		return nil, err
	}

	if reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return nil, fmt.Errorf("cannot obtain bus name %q", timedate1BusName)
	}

	return server, nil
}

func (server *mockTimedate1) Export() {
	server.conn.Export(timedate1Api{server}, timedate1ObjectPath, "org.freedesktop.DBus.Properties")
}

func (server *mockTimedate1) Stop() error {
	if _, err := server.conn.ReleaseName(timedate1BusName); err != nil {
		return err
	}
	return server.conn.Close()
}

type timedate1Api struct {
	server *mockTimedate1
}

func (a timedate1Api) Get(iff, prop string) (dbus.Variant, *dbus.Error) {
	a.server.getPropertyCalled = append(a.server.getPropertyCalled, fmt.Sprintf("if=%s;prop=%s", iff, prop))
	return dbus.MakeVariant(a.server.NTPSynchronized), nil
}

type syncedSuite struct {
	testutil.BaseTest
	testutil.DBusTest
}

var _ = Suite(&syncedSuite{})

func (s *syncedSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.DBusTest.SetUpTest(c)

	restore := dbusutil.MockOnlySystemBusAvailable(s.SessionBus)
	s.AddCleanup(restore)
}

func (s *syncedSuite) TearDownTest(c *C) {
	s.DBusTest.TearDownTest(c)
	s.BaseTest.TearDownTest(c)
}

func (s *syncedSuite) TestIsNTPSynchronized(c *C) {
	backend, err := newMockTimedate1()
	c.Assert(err, IsNil)
	defer backend.Stop()
	backend.Export()

	for _, v := range []bool{true, false} {
		backend.getPropertyCalled = nil
		backend.NTPSynchronized = v

		synced, err := timeutil.IsNTPSynchronized()
		c.Assert(err, IsNil)
		c.Check(synced, Equals, v)

		c.Check(backend.getPropertyCalled, DeepEquals, []string{
			"if=org.freedesktop.timedate1;prop=NTPSynchronized",
		})
	}
}

func (s *syncedSuite) TestIsNTPSynchronizedStrangeEr(c *C) {
	backend, err := newMockTimedate1()
	c.Assert(err, IsNil)
	defer backend.Stop()
	// Note that we did not export anything here - this error is a bit
	// artificial

	_, err = timeutil.IsNTPSynchronized()
	c.Check(err, ErrorMatches, `cannot check for ntp sync: Object does not implement the interface`)
}

func (s *syncedSuite) TestIsNTPSynchronizedNoTimedatectlNoErr(c *C) {
	// note that there is no mock timedate1 created so we are on an empty bus
	synced, err := timeutil.IsNTPSynchronized()
	c.Check(err, ErrorMatches, `cannot find org.freedesktop.timedate1 dbus service: .*`)
	c.Check(errors.As(err, &timeutil.NoTimedate1Error{}), Equals, true)
	c.Check(synced, Equals, false)
}
