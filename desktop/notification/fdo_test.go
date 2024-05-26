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

package notification_test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/desktop/notification"
	"github.com/snapcore/snapd/desktop/notification/notificationtest"
	"github.com/snapcore/snapd/testutil"
)

type fdoSuite struct {
	testutil.BaseTest
	testutil.DBusTest

	backend *notificationtest.FdoServer
}

var _ = Suite(&fdoSuite{})

func (s *fdoSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.DBusTest.SetUpTest(c)

	backend := mylog.Check2(notificationtest.NewFdoServer())

	s.AddCleanup(func() { c.Check(backend.Stop(), IsNil) })
	s.backend = backend
}

func (s *fdoSuite) TearDownTest(c *C) {
	s.DBusTest.TearDownTest(c)
	s.BaseTest.TearDownTest(c)
}

func (s *fdoSuite) TestServerInformationSuccess(c *C) {
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id").(*notification.FdoBackend)
	name, vendor, version, specVersion := mylog.Check5(srv.ServerInformation())

	c.Check(name, Equals, "name")
	c.Check(vendor, Equals, "vendor")
	c.Check(version, Equals, "version")
	c.Check(specVersion, Equals, "specVersion")
}

func (s *fdoSuite) TestServerInformationError(c *C) {
	s.backend.SetError(&dbus.Error{Name: "org.freedesktop.DBus.Error.Failed"})
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id").(*notification.FdoBackend)
	_, _, _, _ := mylog.Check5(srv.ServerInformation())
	c.Assert(err, ErrorMatches, "org.freedesktop.DBus.Error.Failed")
}

func (s *fdoSuite) TestServerCapabilitiesSuccess(c *C) {
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id").(*notification.FdoBackend)
	caps := mylog.Check2(srv.ServerCapabilities())

	c.Check(caps, DeepEquals, []notification.ServerCapability{"cap-foo", "cap-bar"})
}

func (s *fdoSuite) TestServerCapabilitiesError(c *C) {
	s.backend.SetError(&dbus.Error{Name: "org.freedesktop.DBus.Error.Failed"})
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id").(*notification.FdoBackend)
	_ := mylog.Check2(srv.ServerCapabilities())
	c.Assert(err, ErrorMatches, "org.freedesktop.DBus.Error.Failed")
}

func (s *fdoSuite) TestSendNotificationSuccess(c *C) {
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id").(*notification.FdoBackend)
	mylog.Check(srv.SendNotification("some-id", &notification.Message{
		AppName:       "app-name",
		Icon:          "icon",
		Title:         "summary",
		Body:          "body",
		Priority:      notification.PriorityUrgent,
		ExpireTimeout: time.Second * 1,
		Actions: []notification.Action{
			{ActionKey: "key-1", LocalizedText: "text-1"},
			{ActionKey: "key-2", LocalizedText: "text-2"},
		},
		Hints: []notification.Hint{
			{Name: "hint-str", Value: "str"},
			{Name: "hint-bool", Value: true},
		},
	}))


	c.Check(s.backend.Get(1), DeepEquals, &notificationtest.FdoNotification{
		ID:      uint32(1),
		AppName: "app-name",
		Icon:    "icon",
		Summary: "summary",
		Body:    "body",
		Actions: []string{"key-1", "text-1", "key-2", "text-2"},
		Hints: map[string]dbus.Variant{
			"hint-str":      dbus.MakeVariant("str"),
			"hint-bool":     dbus.MakeVariant(true),
			"urgency":       dbus.MakeVariant(uint8(2)),
			"desktop-entry": dbus.MakeVariant("desktop-id"),
		},
		Expires: 1000,
	})
}

func (s *fdoSuite) TestSendNotificationWithServerDecidedExpireTimeout(c *C) {
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id").(*notification.FdoBackend)
	mylog.Check(srv.SendNotification("some-id", &notification.Message{
		ExpireTimeout: notification.ServerSelectedExpireTimeout,
	}))


	c.Check(s.backend.Get(uint32(1)), DeepEquals, &notificationtest.FdoNotification{
		ID:      uint32(1),
		Actions: []string{},
		Hints: map[string]dbus.Variant{
			"urgency":       dbus.MakeVariant(uint8(1)),
			"desktop-entry": dbus.MakeVariant("desktop-id"),
		},
		Expires: -1,
	})
}

func (s *fdoSuite) TestSendNotificationError(c *C) {
	s.backend.SetError(&dbus.Error{Name: "org.freedesktop.DBus.Error.Failed"})
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id")
	mylog.Check(srv.SendNotification("some-id", &notification.Message{}))
	c.Assert(err, ErrorMatches, "org.freedesktop.DBus.Error.Failed")
}

func (s *fdoSuite) TestCloseNotificationSuccess(c *C) {
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id").(*notification.FdoBackend)
	mylog.Check(srv.SendNotification("some-id", &notification.Message{}))

	mylog.Check(srv.CloseNotification("some-id"))

	c.Check(s.backend.GetAll(), HasLen, 0)
}

func (s *fdoSuite) TestCloseNotificationError(c *C) {
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id").(*notification.FdoBackend)
	mylog.Check(srv.SendNotification("some-id", &notification.Message{}))

	s.backend.SetError(&dbus.Error{Name: "org.freedesktop.DBus.Error.Failed"})
	mylog.Check(srv.CloseNotification("some-id"))
	c.Assert(err, ErrorMatches, "org.freedesktop.DBus.Error.Failed")
}

func (s *fdoSuite) TestCloseNotificationUnknownNotification(c *C) {
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id")
	mylog.Check(srv.CloseNotification("some-id"))
	c.Assert(err, ErrorMatches, `unknown notification with id "some-id"`)
}

type testObserver struct {
	notificationClosed func(notification.ID, notification.CloseReason) error
	actionInvoked      func(uint32, string) error
}

func (o *testObserver) NotificationClosed(id notification.ID, reason notification.CloseReason) error {
	if o.notificationClosed != nil {
		return o.notificationClosed(id, reason)
	}
	return nil
}

func (o *testObserver) ActionInvoked(id uint32, actionKey string) error {
	if o.actionInvoked != nil {
		return o.actionInvoked(id, actionKey)
	}
	return nil
}

func (s *fdoSuite) TestObserveNotificationsContextAndSignalWatch(c *C) {
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id").(*notification.FdoBackend)

	ctx, cancel := context.WithCancel(context.TODO())
	signalDelivered := make(chan struct{}, 1)
	defer close(signalDelivered)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		mylog.Check(srv.ObserveNotifications(ctx, &testObserver{
			actionInvoked: func(id uint32, actionKey string) error {
				select {
				case signalDelivered <- struct{}{}:
				default:
				}
				return nil
			},
		}))
		c.Assert(err, ErrorMatches, "context canceled")
		wg.Done()
	}()
	// Send signals until we've got confirmation that the observer
	// is firing
	for sendSignal := true; sendSignal; {
		c.Check(s.backend.InvokeAction(42, "action-key"), IsNil)
		select {
		case <-signalDelivered:
			sendSignal = false
		default:
		}
	}
	cancel()
	// Wait for ObserveNotifications to return
	wg.Wait()
}

func (s *fdoSuite) TestObserveNotificationsProcessingError(c *C) {
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id").(*notification.FdoBackend)
	signalDelivered := make(chan struct{}, 1)
	defer close(signalDelivered)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		mylog.Check(srv.ObserveNotifications(context.TODO(), &testObserver{
			actionInvoked: func(id uint32, actionKey string) error {
				signalDelivered <- struct{}{}
				c.Check(id, Equals, uint32(42))
				c.Check(actionKey, Equals, "action-key")
				return fmt.Errorf("boom")
			},
		}))
		c.Log("End of goroutine")
		c.Check(err, ErrorMatches, "cannot process ActionInvoked signal: boom")
		wg.Done()
	}()
	// We don't know if the other goroutine has set up the signal
	// match yet, so send signals until we get confirmation.
	for sendSignal := true; sendSignal; {
		c.Check(s.backend.InvokeAction(42, "action-key"), IsNil)
		select {
		case <-signalDelivered:
			sendSignal = false
		default:
		}
	}
	// Wait for ObserveNotifications to return
	wg.Wait()
}

func (s *fdoSuite) TestProcessNotificationClosedUnknownNotificationNoError(c *C) {
	var called bool
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id").(*notification.FdoBackend)
	mylog.Check(srv.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.NotificationClosed",
		Body: []interface{}{uint32(1), uint32(2)},
	}, &testObserver{
		notificationClosed: func(id notification.ID, reason notification.CloseReason) error {
			called = true
			return fmt.Errorf("this shouldn't get called")
		},
	}))

	c.Assert(called, Equals, false)
}

func (s *fdoSuite) TestProcessActionInvokedSignalSuccess(c *C) {
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id").(*notification.FdoBackend)
	called := false
	mylog.Check(srv.ProcessSignal(&dbus.Signal{
		// Sender and Path are not used
		Name: "org.freedesktop.Notifications.ActionInvoked",
		Body: []interface{}{uint32(42), "action-key"},
	}, &testObserver{
		actionInvoked: func(id uint32, actionKey string) error {
			called = true
			c.Check(id, Equals, uint32(42))
			c.Check(actionKey, Equals, "action-key")
			return nil
		},
	}))

	c.Assert(called, Equals, true)
}

func (s *fdoSuite) TestProcessActionInvokedSignalError(c *C) {
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id").(*notification.FdoBackend)
	mylog.Check(srv.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.ActionInvoked",
		Body: []interface{}{uint32(42), "action-key"},
	}, &testObserver{
		actionInvoked: func(id uint32, actionKey string) error {
			return fmt.Errorf("boom")
		},
	}))
	c.Assert(err, ErrorMatches, "cannot process ActionInvoked signal: boom")
}

func (s *fdoSuite) TestProcessActionInvokedSignalBodyParseErrors(c *C) {
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id").(*notification.FdoBackend)
	mylog.Check(srv.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.ActionInvoked",
		Body: []interface{}{uint32(42), "action-key", "unexpected"},
	}, &testObserver{}))
	c.Assert(err, ErrorMatches, "cannot process ActionInvoked signal: unexpected number of body elements: 3")
	mylog.Check(srv.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.ActionInvoked",
		Body: []interface{}{uint32(42)},
	}, &testObserver{}))
	c.Assert(err, ErrorMatches, "cannot process ActionInvoked signal: unexpected number of body elements: 1")
	mylog.Check(srv.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.ActionInvoked",
		Body: []interface{}{uint32(42), true},
	}, &testObserver{}))
	c.Assert(err, ErrorMatches, "cannot process ActionInvoked signal: expected second body element to be string, got bool")
	mylog.Check(srv.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.ActionInvoked",
		Body: []interface{}{true, "action-key"},
	}, &testObserver{}))
	c.Assert(err, ErrorMatches, "cannot process ActionInvoked signal: expected first body element to be uint32, got bool")
}

func (s *fdoSuite) TestProcessNotificationClosedSignalSuccess(c *C) {
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id").(*notification.FdoBackend)
	mylog.
		// send a notification first
		Check(srv.SendNotification("some-id", &notification.Message{}))

	called := false
	mylog.Check(srv.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.NotificationClosed",
		Body: []interface{}{uint32(1), uint32(2)},
	}, &testObserver{
		notificationClosed: func(id notification.ID, reason notification.CloseReason) error {
			called = true
			c.Check(id, Equals, notification.ID("some-id"))
			c.Check(reason, Equals, notification.CloseReason(2))
			return nil
		},
	}))

	c.Assert(called, Equals, true)
}

func (s *fdoSuite) TestProcessNotificationClosedSignalError(c *C) {
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id").(*notification.FdoBackend)
	mylog.
		// send a notification first
		Check(srv.SendNotification("some-id", &notification.Message{}))

	mylog.Check(srv.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.NotificationClosed",
		Body: []interface{}{uint32(1), uint32(2)},
	}, &testObserver{
		notificationClosed: func(id notification.ID, reason notification.CloseReason) error {
			return fmt.Errorf("boom")
		},
	}))
	c.Assert(err, ErrorMatches, "cannot process NotificationClosed signal: boom")
}

func (s *fdoSuite) TestProcessNotificationClosedSignalBodyParseErrors(c *C) {
	srv := notification.NewFdoBackend(s.SessionBus, "desktop-id").(*notification.FdoBackend)
	mylog.Check(srv.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.NotificationClosed",
		Body: []interface{}{uint32(42), uint32(2), "unexpected"},
	}, &testObserver{}))
	c.Assert(err, ErrorMatches, "cannot process NotificationClosed signal: unexpected number of body elements: 3")
	mylog.Check(srv.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.NotificationClosed",
		Body: []interface{}{uint32(42)},
	}, &testObserver{}))
	c.Assert(err, ErrorMatches, "cannot process NotificationClosed signal: unexpected number of body elements: 1")
	mylog.Check(srv.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.NotificationClosed",
		Body: []interface{}{uint32(42), true},
	}, &testObserver{}))
	c.Assert(err, ErrorMatches, "cannot process NotificationClosed signal: expected second body element to be uint32, got bool")
	mylog.Check(srv.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.NotificationClosed",
		Body: []interface{}{true, uint32(2)},
	}, &testObserver{}))
	c.Assert(err, ErrorMatches, "cannot process NotificationClosed signal: expected first body element to be uint32, got bool")
}
