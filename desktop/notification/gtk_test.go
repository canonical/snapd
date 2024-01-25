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
	"context"

	"github.com/godbus/dbus"
	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/desktop/notification"
	"github.com/snapcore/snapd/desktop/notification/notificationtest"
	"github.com/snapcore/snapd/testutil"
)

const sessionAgentBusName = "io.snapcraft.SessionAgent"

type gtkSuite struct {
	testutil.BaseTest
	testutil.DBusTest

	backend *notificationtest.GtkServer
}

var _ = Suite(&gtkSuite{})

func (s *gtkSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.DBusTest.SetUpTest(c)

	backend, err := notificationtest.NewGtkServer()
	c.Assert(err, IsNil)
	s.AddCleanup(func() { c.Check(backend.Stop(), IsNil) })
	s.backend = backend
}

func (s *gtkSuite) TearDownTest(c *C) {
	s.DBusTest.TearDownTest(c)
	s.BaseTest.TearDownTest(c)
}

func (s *gtkSuite) TestGtkNotAvailabe(c *C) {
	s.backend.ReleaseName()
	_, err := notification.NewGtkBackend(s.SessionBus, sessionAgentBusName)
	c.Assert(err, ErrorMatches, `Could not get owner of name 'org.gtk.Notifications': no such name`)
}

func (s *gtkSuite) TestSendNotificationSuccess(c *C) {
	srv, err := notification.NewGtkBackend(s.SessionBus, sessionAgentBusName)
	c.Assert(err, IsNil)
	err = srv.SendNotification("some-id", &notification.Message{
		Title:    "some title",
		Body:     "a body",
		Icon:     "an icon",
		Priority: notification.PriorityUrgent,
	})
	c.Assert(err, IsNil)

	c.Check(s.backend.Get("some-id"), DeepEquals, &notificationtest.GtkNotification{
		DesktopID: sessionAgentBusName,
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
	srv, err := notification.NewGtkBackend(s.SessionBus, sessionAgentBusName)
	c.Assert(err, IsNil)
	err = srv.SendNotification("some-id", &notification.Message{})
	c.Assert(err, ErrorMatches, "org.gtk.Notifications.Error.Failed")
}

func (s *gtkSuite) TestCloseNotificationSuccess(c *C) {
	srv, err := notification.NewGtkBackend(s.SessionBus, sessionAgentBusName)
	c.Assert(err, IsNil)
	err = srv.SendNotification("some-id", &notification.Message{})
	c.Assert(err, IsNil)

	err = srv.CloseNotification("some-id")
	c.Assert(err, IsNil)
	c.Check(s.backend.GetAll(), HasLen, 0)
}

func (s *gtkSuite) TestCloseNotificationError(c *C) {
	srv, err := notification.NewGtkBackend(s.SessionBus, sessionAgentBusName)
	c.Assert(err, IsNil)
	err = srv.SendNotification("some-id", &notification.Message{})
	c.Assert(err, IsNil)
	s.backend.SetError(&dbus.Error{Name: "org.gtk.Notifications.Error.Failed"})
	err = srv.CloseNotification("some-id")
	c.Assert(err, ErrorMatches, "org.gtk.Notifications.Error.Failed")
}

func (s *gtkSuite) TestNotificationGtkWithActions(c *C) {
	srv, err := notification.NewGtkBackend(s.SessionBus, sessionAgentBusName)
	c.Assert(err, IsNil)
	srv.GetConn().RequestName(sessionAgentBusName, dbus.NameFlagDoNotQueue)

	tombS := tomb.Tomb{}
	ch := make(chan notificationData)
	observer := testObserver{
		actionInvoked: func(id notification.ID, actionKey string, parameters []string) error {
			ch <- notificationData{
				id:         id,
				actionKey:  actionKey,
				parameters: parameters,
			}
			return nil
		},
	}
	tombS.Go(func() error {
		ctx := tombS.Context(context.Background())
		return srv.HandleNotifications(ctx, &observer)
	})
	defer tombS.Kill(nil)

	err = srv.SendNotification("some-id", &notification.Message{
		Actions: []notification.Action{
			{
				ActionKey:     "key-1",
				LocalizedText: "text-1",
				Parameters:    []string{"param1", "param2"},
			},
			{
				ActionKey:     "key-2",
				LocalizedText: "text-2",
			},
		},
	})
	c.Assert(err, IsNil)
	s.backend.WaitCounter(1)
	notifications := s.backend.GetAll()
	c.Assert(len(notifications), Equals, 1)
	c.Assert(notifications[0].DesktopID, Equals, sessionAgentBusName)
	c.Assert(notifications[0].ID, Equals, "some-id")
	notificationInfo := notifications[0].Info
	c.Assert(notificationInfo, FitsTypeOf, map[string]dbus.Variant{})
	data := map[string]dbus.Variant(notificationInfo)
	buttonsVariant := data["buttons"].Value().([]map[string]dbus.Variant)
	c.Assert(len(buttonsVariant), Equals, 2)
	var notification1 map[string]dbus.Variant
	var notification2 map[string]dbus.Variant
	if buttonsVariant[0]["label"].String() == "\"text-1\"" {
		notification1 = buttonsVariant[0]
		notification2 = buttonsVariant[1]
	} else {
		notification1 = buttonsVariant[1]
		notification2 = buttonsVariant[0]
	}
	c.Assert(notification1["action"].String(), Equals, "\"app.Notification\"")
	c.Assert(notification2["action"].String(), Equals, "\"app.Notification\"")
	c.Assert(notification1["target"].Signature().String(), Equals, "as")
	c.Assert(notification2["target"].Signature().String(), Equals, "as")
	params1 := notification1["target"].Value().([]string)
	params2 := notification2["target"].Value().([]string)
	c.Assert(len(params1), Equals, 4)
	c.Assert(params1[0], Equals, "some-id")
	c.Assert(params1[1], Equals, "key-1")
	c.Assert(params1[2], Equals, "param1")
	c.Assert(params1[3], Equals, "param2")
	c.Assert(len(params2), Equals, 2)
	c.Assert(params2[0], Equals, "some-id")
	c.Assert(params2[1], Equals, "key-2")

	err = s.backend.InvokeAction(sessionAgentBusName, "some-id", "key-1", []string{"param1", "param2"})
	c.Assert(err, IsNil)
	recvNotification := <-ch
	c.Assert(recvNotification.id, Equals, notification.ID("some-id"))
	c.Assert(recvNotification.actionKey, Equals, "key-1")
	c.Assert(recvNotification.parameters, DeepEquals, []string{"param1", "param2"})

	s.backend.InvokeAction(sessionAgentBusName, "some-id", "key-2", nil)
	recvNotification = <-ch
	c.Assert(recvNotification.id, Equals, notification.ID("some-id"))
	c.Assert(recvNotification.actionKey, Equals, "key-2")
	c.Assert(recvNotification.parameters, IsNil)
}
