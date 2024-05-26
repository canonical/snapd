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
	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/desktop/notification"
	"github.com/snapcore/snapd/desktop/notification/notificationtest"
	"github.com/snapcore/snapd/testutil"
)

type gtkSuite struct {
	testutil.BaseTest
	testutil.DBusTest

	backend *notificationtest.GtkServer
}

var _ = Suite(&gtkSuite{})

func (s *gtkSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.DBusTest.SetUpTest(c)

	backend := mylog.Check2(notificationtest.NewGtkServer())

	s.AddCleanup(func() { c.Check(backend.Stop(), IsNil) })
	s.backend = backend
}

func (s *gtkSuite) TearDownTest(c *C) {
	s.DBusTest.TearDownTest(c)
	s.BaseTest.TearDownTest(c)
}

func (s *gtkSuite) TestGtkNotAvailabe(c *C) {
	s.backend.ReleaseName()
	_ := mylog.Check2(notification.NewGtkBackend(s.SessionBus, "desktop-id"))
	c.Assert(err, ErrorMatches, `Could not get owner of name 'org.gtk.Notifications': no such name`)
}

func (s *gtkSuite) TestSendNotificationSuccess(c *C) {
	srv := mylog.Check2(notification.NewGtkBackend(s.SessionBus, "desktop-id"))

	mylog.Check(srv.SendNotification("some-id", &notification.Message{
		Title:    "some title",
		Body:     "a body",
		Icon:     "an icon",
		Priority: notification.PriorityUrgent,
	}))


	c.Check(s.backend.Get("some-id"), DeepEquals, &notificationtest.GtkNotification{
		DesktopID: "desktop-id",
		ID:        "some-id",
		Info: map[string]dbus.Variant{
			"title":    dbus.MakeVariant("some title"),
			"body":     dbus.MakeVariant("a body"),
			"icon":     dbus.MakeVariant([]interface{}{"file", dbus.MakeVariant("an icon")}),
			"priority": dbus.MakeVariant("urgent"),
		},
	})
}

func (s *gtkSuite) TestSendNotificationError(c *C) {
	s.backend.SetError(&dbus.Error{Name: "org.gtk.Notifications.Error.Failed"})
	srv := mylog.Check2(notification.NewGtkBackend(s.SessionBus, "desktop-id"))

	mylog.Check(srv.SendNotification("some-id", &notification.Message{}))
	c.Assert(err, ErrorMatches, "org.gtk.Notifications.Error.Failed")
}

func (s *gtkSuite) TestCloseNotificationSuccess(c *C) {
	srv := mylog.Check2(notification.NewGtkBackend(s.SessionBus, "desktop-id"))

	mylog.Check(srv.SendNotification("some-id", &notification.Message{}))

	mylog.Check(srv.CloseNotification("some-id"))

	c.Check(s.backend.GetAll(), HasLen, 0)
}

func (s *gtkSuite) TestCloseNotificationError(c *C) {
	srv := mylog.Check2(notification.NewGtkBackend(s.SessionBus, "desktop-id"))

	mylog.Check(srv.SendNotification("some-id", &notification.Message{}))

	s.backend.SetError(&dbus.Error{Name: "org.gtk.Notifications.Error.Failed"})
	mylog.Check(srv.CloseNotification("some-id"))
	c.Assert(err, ErrorMatches, "org.gtk.Notifications.Error.Failed")
}
