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

	"github.com/godbus/dbus"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/dbusutil/dbustest"
	"github.com/snapcore/snapd/desktop/notification"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/testutil"
)

type fdoSuite struct {
	testutil.BaseTest
}

var _ = Suite(&fdoSuite{})

func (s *fdoSuite) connectWithHandler(c *C, handler dbustest.DBusHandlerFunc) *notification.Server {
	conn, err := dbustest.Connection(handler)
	c.Assert(err, IsNil)
	restore := dbusutil.MockOnlySessionBusAvailable(conn)
	s.AddCleanup(restore)
	return notification.New(conn)
}

func (s *fdoSuite) checkGetServerInformationRequest(c *C, msg *dbus.Message) {
	c.Assert(msg.Type, Equals, dbus.TypeMethodCall)
	c.Check(msg.Flags, Equals, dbus.Flags(0))
	c.Check(msg.Headers, DeepEquals, map[dbus.HeaderField]dbus.Variant{
		dbus.FieldDestination: dbus.MakeVariant("org.freedesktop.Notifications"),
		dbus.FieldPath:        dbus.MakeVariant(dbus.ObjectPath("/org/freedesktop/Notifications")),
		dbus.FieldInterface:   dbus.MakeVariant("org.freedesktop.Notifications"),
		dbus.FieldMember:      dbus.MakeVariant("GetServerInformation"),
	})
	c.Check(msg.Body, HasLen, 0)
}

func (s *fdoSuite) checkGetCapabilitiesRequest(c *C, msg *dbus.Message) {
	c.Assert(msg.Type, Equals, dbus.TypeMethodCall)
	c.Check(msg.Flags, Equals, dbus.Flags(0))
	c.Check(msg.Headers, DeepEquals, map[dbus.HeaderField]dbus.Variant{
		dbus.FieldDestination: dbus.MakeVariant("org.freedesktop.Notifications"),
		dbus.FieldPath:        dbus.MakeVariant(dbus.ObjectPath("/org/freedesktop/Notifications")),
		dbus.FieldInterface:   dbus.MakeVariant("org.freedesktop.Notifications"),
		dbus.FieldMember:      dbus.MakeVariant("GetCapabilities"),
	})
	c.Check(msg.Body, HasLen, 0)
}

func (s *fdoSuite) checkNotifyRequest(c *C, msg *dbus.Message) {
	c.Assert(msg.Type, Equals, dbus.TypeMethodCall)
	c.Check(msg.Flags, Equals, dbus.Flags(0))
	c.Check(msg.Headers, DeepEquals, map[dbus.HeaderField]dbus.Variant{
		dbus.FieldDestination: dbus.MakeVariant("org.freedesktop.Notifications"),
		dbus.FieldPath:        dbus.MakeVariant(dbus.ObjectPath("/org/freedesktop/Notifications")),
		dbus.FieldInterface:   dbus.MakeVariant("org.freedesktop.Notifications"),
		dbus.FieldMember:      dbus.MakeVariant("Notify"),
		dbus.FieldSignature: dbus.MakeVariant(dbus.SignatureOf(
			"", uint32(0), "", "", "", []string{}, map[string]dbus.Variant{}, int32(0),
		)),
	})
	c.Check(msg.Body, HasLen, 8)
}

func (s *fdoSuite) checkCloseNotificationRequest(c *C, msg *dbus.Message) {
	c.Assert(msg.Type, Equals, dbus.TypeMethodCall)
	c.Check(msg.Flags, Equals, dbus.Flags(0))
	c.Check(msg.Headers, DeepEquals, map[dbus.HeaderField]dbus.Variant{
		dbus.FieldDestination: dbus.MakeVariant("org.freedesktop.Notifications"),
		dbus.FieldPath:        dbus.MakeVariant(dbus.ObjectPath("/org/freedesktop/Notifications")),
		dbus.FieldInterface:   dbus.MakeVariant("org.freedesktop.Notifications"),
		dbus.FieldMember:      dbus.MakeVariant("CloseNotification"),
		dbus.FieldSignature:   dbus.MakeVariant(dbus.SignatureOf(uint32(0))),
	})
	c.Check(msg.Body, HasLen, 1)
}

func (s *fdoSuite) nameHasNoOwnerResponse(c *C, msg *dbus.Message) *dbus.Message {
	return &dbus.Message{
		Type: dbus.TypeError,
		Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
			dbus.FieldSender:      dbus.MakeVariant(":1"), // This does not matter.
			// dbus.FieldDestination is provided automatically by DBus test helper.
			dbus.FieldErrorName: dbus.MakeVariant("org.freedesktop.DBus.Error.NameHasNoOwner"),
		},
	}
}

func (s *fdoSuite) TestServerInformationSuccess(c *C) {
	srv := s.connectWithHandler(c, func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		switch n {
		case 0:
			s.checkGetServerInformationRequest(c, msg)
			responseSig := dbus.SignatureOf("", "", "", "")
			response := &dbus.Message{
				Type: dbus.TypeMethodReply,
				Headers: map[dbus.HeaderField]dbus.Variant{
					dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
					dbus.FieldSender:      dbus.MakeVariant(":1"), // This does not matter.
					// dbus.FieldDestination is provided automatically by DBus test helper.
					dbus.FieldSignature: dbus.MakeVariant(responseSig),
				},
				Body: []interface{}{"name", "vendor", "version", "specVersion"},
			}
			return []*dbus.Message{response}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})
	name, vendor, version, specVersion, err := srv.ServerInformation()
	c.Assert(err, IsNil)
	c.Check(name, Equals, "name")
	c.Check(vendor, Equals, "vendor")
	c.Check(version, Equals, "version")
	c.Check(specVersion, Equals, "specVersion")
}

func (s *fdoSuite) TestServerInformationError(c *C) {
	srv := s.connectWithHandler(c, func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		switch n {
		case 0:
			s.checkGetServerInformationRequest(c, msg)
			response := s.nameHasNoOwnerResponse(c, msg)
			return []*dbus.Message{response}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})
	_, _, _, _, err := srv.ServerInformation()
	c.Assert(err, ErrorMatches, "org.freedesktop.DBus.Error.NameHasNoOwner")
}

func (s *fdoSuite) TestServerCapabilitiesSuccess(c *C) {
	srv := s.connectWithHandler(c, func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		switch n {
		case 0:
			s.checkGetCapabilitiesRequest(c, msg)
			responseSig := dbus.SignatureOf([]string{})
			response := &dbus.Message{
				Type: dbus.TypeMethodReply,
				Headers: map[dbus.HeaderField]dbus.Variant{
					dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
					dbus.FieldSender:      dbus.MakeVariant(":1"), // This does not matter.
					// dbus.FieldDestination is provided automatically by DBus test helper.
					dbus.FieldSignature: dbus.MakeVariant(responseSig),
				},
				Body: []interface{}{
					[]string{"cap-foo", "cap-bar"},
				},
			}
			return []*dbus.Message{response}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})
	caps, err := srv.ServerCapabilities()
	c.Assert(err, IsNil)
	c.Check(caps, DeepEquals, []notification.ServerCapability{"cap-foo", "cap-bar"})
}

func (s *fdoSuite) TestServerCapabilitiesError(c *C) {
	srv := s.connectWithHandler(c, func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		switch n {
		case 0:
			s.checkGetCapabilitiesRequest(c, msg)
			response := s.nameHasNoOwnerResponse(c, msg)
			return []*dbus.Message{response}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})
	_, err := srv.ServerCapabilities()
	c.Assert(err, ErrorMatches, "org.freedesktop.DBus.Error.NameHasNoOwner")
}

func (s *fdoSuite) TestSendNotificationSuccess(c *C) {
	srv := s.connectWithHandler(c, func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		switch n {
		case 0:
			s.checkNotifyRequest(c, msg)
			c.Check(msg.Body[0], Equals, "app-name")
			c.Check(msg.Body[1], Equals, uint32(42))
			c.Check(msg.Body[2], Equals, "icon")
			c.Check(msg.Body[3], Equals, "summary")
			c.Check(msg.Body[4], Equals, "body")
			c.Check(msg.Body[5], DeepEquals, []string{"key-1", "text-1", "key-2", "text-2"})
			c.Check(msg.Body[6], DeepEquals, map[string]dbus.Variant{
				"hint-str":  dbus.MakeVariant("str"),
				"hint-bool": dbus.MakeVariant(true),
			})
			c.Check(msg.Body[7], Equals, int32(1000))
			responseSig := dbus.SignatureOf(uint32(0))
			response := &dbus.Message{
				Type: dbus.TypeMethodReply,
				Headers: map[dbus.HeaderField]dbus.Variant{
					dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
					dbus.FieldSender:      dbus.MakeVariant(":1"), // This does not matter.
					// dbus.FieldDestination is provided automatically by DBus test helper.
					dbus.FieldSignature: dbus.MakeVariant(responseSig),
				},
				Body: []interface{}{uint32(7)},
			}
			return []*dbus.Message{response}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})
	id, err := srv.SendNotification(&notification.Message{
		AppName:       "app-name",
		Icon:          "icon",
		Summary:       "summary",
		Body:          "body",
		ExpireTimeout: time.Second * 1,
		ReplacesID:    notification.ID(42),
		Actions: []notification.Action{
			{ActionKey: "key-1", LocalizedText: "text-1"},
			{ActionKey: "key-2", LocalizedText: "text-2"},
		},
		Hints: []notification.Hint{
			{Name: "hint-str", Value: "str"},
			{Name: "hint-bool", Value: true},
		},
	})
	c.Assert(err, IsNil)
	c.Check(id, Equals, notification.ID(7))
}

func (s *fdoSuite) TestSendNotificationWithServerDecitedExpireTimeout(c *C) {
	srv := s.connectWithHandler(c, func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		switch n {
		case 0:
			s.checkNotifyRequest(c, msg)
			c.Check(msg.Body[7], Equals, int32(-1))
			responseSig := dbus.SignatureOf(uint32(0))
			response := &dbus.Message{
				Type: dbus.TypeMethodReply,
				Headers: map[dbus.HeaderField]dbus.Variant{
					dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
					dbus.FieldSender:      dbus.MakeVariant(":1"), // This does not matter.
					// dbus.FieldDestination is provided automatically by DBus test helper.
					dbus.FieldSignature: dbus.MakeVariant(responseSig),
				},
				Body: []interface{}{uint32(7)},
			}
			return []*dbus.Message{response}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})
	id, err := srv.SendNotification(&notification.Message{
		ExpireTimeout: notification.ServerSelectedExpireTimeout,
	})
	c.Assert(err, IsNil)
	c.Check(id, Equals, notification.ID(7))
}

func (s *fdoSuite) TestSendNotificationError(c *C) {
	srv := s.connectWithHandler(c, func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		switch n {
		case 0:
			s.checkNotifyRequest(c, msg)
			response := s.nameHasNoOwnerResponse(c, msg)
			return []*dbus.Message{response}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})
	_, err := srv.SendNotification(&notification.Message{})
	c.Assert(err, ErrorMatches, "org.freedesktop.DBus.Error.NameHasNoOwner")
}

func (s *fdoSuite) TestCloseNotificationSuccess(c *C) {
	srv := s.connectWithHandler(c, func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		switch n {
		case 0:
			s.checkCloseNotificationRequest(c, msg)
			c.Check(msg.Body[0], Equals, uint32(42))
			response := &dbus.Message{
				Type: dbus.TypeMethodReply,
				Headers: map[dbus.HeaderField]dbus.Variant{
					dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
					dbus.FieldSender:      dbus.MakeVariant(":1"), // This does not matter.
					// dbus.FieldDestination is provided automatically by DBus test helper.
				},
			}
			return []*dbus.Message{response}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})
	err := srv.CloseNotification(notification.ID(42))
	c.Assert(err, IsNil)
}

func (s *fdoSuite) TestCloseNotificationError(c *C) {
	srv := s.connectWithHandler(c, func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		switch n {
		case 0:
			s.checkCloseNotificationRequest(c, msg)
			response := s.nameHasNoOwnerResponse(c, msg)
			return []*dbus.Message{response}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})
	err := srv.CloseNotification(notification.ID(42))
	c.Assert(err, ErrorMatches, "org.freedesktop.DBus.Error.NameHasNoOwner")
}

type testObserver struct {
	notificationClosed func(notification.ID, notification.CloseReason) error
	actionInvoked      func(notification.ID, string) error
}

func (o *testObserver) NotificationClosed(id notification.ID, reason notification.CloseReason) error {
	if o.notificationClosed != nil {
		return o.notificationClosed(id, reason)
	}
	return nil
}

func (o *testObserver) ActionInvoked(id notification.ID, actionKey string) error {
	if o.actionInvoked != nil {
		return o.actionInvoked(id, actionKey)
	}
	return nil
}

func (s *fdoSuite) checkAddMatchRequest(c *C, msg *dbus.Message) {
	c.Assert(msg.Type, Equals, dbus.TypeMethodCall)
	c.Check(msg.Flags, Equals, dbus.Flags(0))
	c.Check(msg.Headers, DeepEquals, map[dbus.HeaderField]dbus.Variant{
		dbus.FieldDestination: dbus.MakeVariant("org.freedesktop.DBus"),
		dbus.FieldPath:        dbus.MakeVariant(dbus.ObjectPath("/org/freedesktop/DBus")),
		dbus.FieldInterface:   dbus.MakeVariant("org.freedesktop.DBus"),
		dbus.FieldMember:      dbus.MakeVariant("AddMatch"),
		dbus.FieldSignature:   dbus.MakeVariant(dbus.SignatureOf("")),
	})
}

func (s *fdoSuite) checkRemoveMatchRequest(c *C, msg *dbus.Message) {
	c.Assert(msg.Type, Equals, dbus.TypeMethodCall)
	c.Check(msg.Flags, Equals, dbus.Flags(0))
	c.Check(msg.Headers, DeepEquals, map[dbus.HeaderField]dbus.Variant{
		dbus.FieldDestination: dbus.MakeVariant("org.freedesktop.DBus"),
		dbus.FieldPath:        dbus.MakeVariant(dbus.ObjectPath("/org/freedesktop/DBus")),
		dbus.FieldInterface:   dbus.MakeVariant("org.freedesktop.DBus"),
		dbus.FieldMember:      dbus.MakeVariant("RemoveMatch"),
		dbus.FieldSignature:   dbus.MakeVariant(dbus.SignatureOf("")),
	})
}

func (s *fdoSuite) addMatchResponse(c *C, msg *dbus.Message) *dbus.Message {
	return &dbus.Message{
		Type: dbus.TypeMethodReply,
		Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
			dbus.FieldSender:      dbus.MakeVariant(":1"), // This does not matter.
			// dbus.FieldDestination is provided automatically by DBus test helper.
		},
	}
}

func (s *fdoSuite) removeMatchResponse(c *C, msg *dbus.Message) *dbus.Message {
	return &dbus.Message{
		Type: dbus.TypeMethodReply,
		Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
			dbus.FieldSender:      dbus.MakeVariant(":1"), // This does not matter.
			// dbus.FieldDestination is provided automatically by DBus test helper.
		},
	}
}

func (s *fdoSuite) TestObserveNotificationsContextAndSignalWatch(c *C) {
	ctx, cancel := context.WithCancel(context.TODO())
	msgsSeen := 0
	addMatchSeen := make(chan struct{}, 1)
	defer close(addMatchSeen)
	srv := s.connectWithHandler(c, func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		msgsSeen++
		switch n {
		case 0:
			s.checkAddMatchRequest(c, msg)
			c.Check(msg.Body, HasLen, 1)
			c.Check(msg.Body[0], Equals, "type='signal',sender='org.freedesktop.Notifications',path='/org/freedesktop/Notifications',interface='org.freedesktop.Notifications'")
			response := s.addMatchResponse(c, msg)
			addMatchSeen <- struct{}{}
			return []*dbus.Message{response}, nil
		case 1:
			s.checkRemoveMatchRequest(c, msg)
			c.Check(msg.Body, HasLen, 1)
			c.Check(msg.Body[0], Equals, "type='signal',sender='org.freedesktop.Notifications',path='/org/freedesktop/Notifications',interface='org.freedesktop.Notifications'")
			response := s.removeMatchResponse(c, msg)
			return []*dbus.Message{response}, nil
		default:
			return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
		}
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err := srv.ObserveNotifications(ctx, &testObserver{})
		c.Assert(err, ErrorMatches, "context canceled")
		wg.Done()
	}()
	// Wait for the signal that we saw the AddMatch message and then stop.
	<-addMatchSeen
	cancel()
	// Wait for ObserveNotifications to return
	wg.Wait()
	c.Check(msgsSeen, Equals, 2)
}

func (s *fdoSuite) TestObserveNotificationsAddWatchError(c *C) {
	srv := s.connectWithHandler(c, func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		switch n {
		case 0:
			s.checkAddMatchRequest(c, msg)
			response := s.nameHasNoOwnerResponse(c, msg)
			return []*dbus.Message{response}, nil
		default:
			return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
		}
	})
	err := srv.ObserveNotifications(context.TODO(), &testObserver{})
	c.Assert(err, ErrorMatches, "org.freedesktop.DBus.Error.NameHasNoOwner")
}

func (s *fdoSuite) TestObserveNotificationsRemoveWatchError(c *C) {
	logBuffer, restore := logger.MockLogger()
	defer restore()

	ctx, cancel := context.WithCancel(context.TODO())
	msgsSeen := 0
	addMatchSeen := make(chan struct{}, 1)
	defer close(addMatchSeen)
	srv := s.connectWithHandler(c, func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		msgsSeen++
		switch n {
		case 0:
			s.checkAddMatchRequest(c, msg)
			response := s.addMatchResponse(c, msg)
			addMatchSeen <- struct{}{}
			return []*dbus.Message{response}, nil
		case 1:
			s.checkRemoveMatchRequest(c, msg)
			response := s.nameHasNoOwnerResponse(c, msg)
			return []*dbus.Message{response}, nil
		default:
			return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
		}
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		err := srv.ObserveNotifications(ctx, &testObserver{})
		// The error from RemoveWatch is not clobbering the return value of ObserveNotifications.
		c.Assert(err, ErrorMatches, "context canceled")
		c.Check(logBuffer.String(), testutil.Contains, "Cannot remove D-Bus signal matcher: org.freedesktop.DBus.Error.NameHasNoOwner\n")
		wg.Done()
	}()
	// Wait for the signal that we saw the AddMatch message and then stop.
	<-addMatchSeen
	cancel()
	// Wait for ObserveNotifications to return
	wg.Wait()
	c.Check(msgsSeen, Equals, 2)
}

func (s *fdoSuite) TestObserveNotificationsProcessingError(c *C) {
	msgsSeen := 0
	srv := s.connectWithHandler(c, func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		msgsSeen++
		switch n {
		case 0:
			s.checkAddMatchRequest(c, msg)
			response := s.addMatchResponse(c, msg)
			sig := &dbus.Message{
				Type: dbus.TypeSignal,
				Headers: map[dbus.HeaderField]dbus.Variant{
					dbus.FieldPath:      dbus.MakeVariant(dbus.ObjectPath("/org/freedesktop/Notifications")),
					dbus.FieldInterface: dbus.MakeVariant("org.freedesktop.Notifications"),
					dbus.FieldMember:    dbus.MakeVariant("ActionInvoked"),
					dbus.FieldSender:    dbus.MakeVariant("org.freedesktop.Notifications"),
					dbus.FieldSignature: dbus.MakeVariant(dbus.SignatureOf(uint32(0), "")),
				},
				Body: []interface{}{uint32(42), "action-key"},
			}
			// Send the DBus response for the method call and an additional signal.
			return []*dbus.Message{response, sig}, nil
		case 1:
			s.checkRemoveMatchRequest(c, msg)
			response := s.removeMatchResponse(c, msg)
			return []*dbus.Message{response}, nil
		default:
			return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
		}
	})
	err := srv.ObserveNotifications(context.TODO(), &testObserver{
		actionInvoked: func(id notification.ID, actionKey string) error {
			c.Check(id, Equals, notification.ID(42))
			c.Check(actionKey, Equals, "action-key")
			return fmt.Errorf("boom")
		},
	})
	c.Assert(err, ErrorMatches, "cannot process ActionInvoked signal: boom")
	c.Check(msgsSeen, Equals, 2)
}

func (s *fdoSuite) TestProcessActionInvokedSignalSuccess(c *C) {
	called := false
	err := notification.ProcessSignal(&dbus.Signal{
		// Sender and Path are not used
		Name: "org.freedesktop.Notifications.ActionInvoked",
		Body: []interface{}{uint32(42), "action-key"},
	}, &testObserver{
		actionInvoked: func(id notification.ID, actionKey string) error {
			called = true
			c.Check(id, Equals, notification.ID(42))
			c.Check(actionKey, Equals, "action-key")
			return nil
		},
	})
	c.Assert(err, IsNil)
	c.Assert(called, Equals, true)
}

func (s *fdoSuite) TestProcessActionInvokedSignalError(c *C) {
	err := notification.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.ActionInvoked",
		Body: []interface{}{uint32(42), "action-key"},
	}, &testObserver{
		actionInvoked: func(id notification.ID, actionKey string) error {
			return fmt.Errorf("boom")
		},
	})
	c.Assert(err, ErrorMatches, "cannot process ActionInvoked signal: boom")
}

func (s *fdoSuite) TestProcessActionInvokedSignalBodyParseErrors(c *C) {
	err := notification.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.ActionInvoked",
		Body: []interface{}{uint32(42), "action-key", "unexpected"},
	}, &testObserver{})
	c.Assert(err, ErrorMatches, "cannot process ActionInvoked signal: unexpected number of body elements: 3")

	err = notification.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.ActionInvoked",
		Body: []interface{}{uint32(42)},
	}, &testObserver{})
	c.Assert(err, ErrorMatches, "cannot process ActionInvoked signal: unexpected number of body elements: 1")

	err = notification.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.ActionInvoked",
		Body: []interface{}{uint32(42), true},
	}, &testObserver{})
	c.Assert(err, ErrorMatches, "cannot process ActionInvoked signal: expected second body element to be string, got bool")

	err = notification.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.ActionInvoked",
		Body: []interface{}{true, "action-key"},
	}, &testObserver{})
	c.Assert(err, ErrorMatches, "cannot process ActionInvoked signal: expected first body element to be uint32, got bool")
}

func (s *fdoSuite) TestProcessNotificationClosedSignalSuccess(c *C) {
	called := false
	err := notification.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.NotificationClosed",
		Body: []interface{}{uint32(42), uint32(2)},
	}, &testObserver{
		notificationClosed: func(id notification.ID, reason notification.CloseReason) error {
			called = true
			c.Check(id, Equals, notification.ID(42))
			c.Check(reason, Equals, notification.CloseReason(2))
			return nil
		},
	})
	c.Assert(err, IsNil)
	c.Assert(called, Equals, true)
}

func (s *fdoSuite) TestProcessNotificationClosedSignalError(c *C) {
	err := notification.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.NotificationClosed",
		Body: []interface{}{uint32(42), uint32(2)},
	}, &testObserver{
		notificationClosed: func(id notification.ID, reason notification.CloseReason) error {
			return fmt.Errorf("boom")
		},
	})
	c.Assert(err, ErrorMatches, "cannot process NotificationClosed signal: boom")
}

func (s *fdoSuite) TestProcessNotificationClosedSignalBodyParseErrors(c *C) {
	err := notification.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.NotificationClosed",
		Body: []interface{}{uint32(42), uint32(2), "unexpected"},
	}, &testObserver{})
	c.Assert(err, ErrorMatches, "cannot process NotificationClosed signal: unexpected number of body elements: 3")

	err = notification.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.NotificationClosed",
		Body: []interface{}{uint32(42)},
	}, &testObserver{})
	c.Assert(err, ErrorMatches, "cannot process NotificationClosed signal: unexpected number of body elements: 1")

	err = notification.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.NotificationClosed",
		Body: []interface{}{uint32(42), true},
	}, &testObserver{})
	c.Assert(err, ErrorMatches, "cannot process NotificationClosed signal: expected second body element to be uint32, got bool")

	err = notification.ProcessSignal(&dbus.Signal{
		Name: "org.freedesktop.Notifications.NotificationClosed",
		Body: []interface{}{true, uint32(2)},
	}, &testObserver{})
	c.Assert(err, ErrorMatches, "cannot process NotificationClosed signal: expected first body element to be uint32, got bool")
}
