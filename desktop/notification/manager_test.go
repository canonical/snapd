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

package notification_test

import (
	"fmt"

	"github.com/godbus/dbus"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/desktop/notification"
	"github.com/snapcore/snapd/testutil"
)

type managerSuite struct {
	testutil.BaseTest
	testutil.DBusTest
}

var _ = Suite(&managerSuite{})

func (s *managerSuite) SetUpTest(c *C) {
}

func (s *managerSuite) TestUseGtkBackendIfAvailable(c *C) {
	gtkBackend := &notification.GtkBackend{}
	restoreGtk := notification.MockNewGtkBackend(func(conn *dbus.Conn, desktopID string) (notification.NotificationManager, error) {
		c.Check(conn, Equals, s.SessionBus)
		c.Check(desktopID, Equals, "desktop-id")
		return gtkBackend, nil
	})
	defer restoreGtk()

	restoreFdo := notification.MockNewFdoBackend(func(conn *dbus.Conn, desktopID string) notification.NotificationManager {
		c.Fatalf("fdo backend shouldn't be created")
		return nil
	})
	defer restoreFdo()

	mgr := notification.NewNotificationManager(s.SessionBus, "desktop-id")
	c.Check(mgr, NotNil)
	c.Check(mgr, Equals, gtkBackend)
}

func (s *managerSuite) TestFdoFallback(c *C) {
	restoreGtk := notification.MockNewGtkBackend(func(conn *dbus.Conn, desktopID string) (notification.NotificationManager, error) {
		c.Check(conn, Equals, s.SessionBus)
		c.Check(desktopID, Equals, "desktop-id")
		return nil, fmt.Errorf("boom")
	})
	defer restoreGtk()

	fdoBackend := &notification.FdoBackend{}
	restoreFdo := notification.MockNewFdoBackend(func(conn *dbus.Conn, desktopID string) notification.NotificationManager {
		c.Check(conn, Equals, s.SessionBus)
		c.Check(desktopID, Equals, "desktop-id")
		return fdoBackend
	})
	defer restoreFdo()

	mgr := notification.NewNotificationManager(s.SessionBus, "desktop-id")
	c.Check(mgr, NotNil)
	c.Check(mgr, Equals, fdoBackend)
}
