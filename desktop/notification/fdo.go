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

package notification

import (
	"context"
	"fmt"

	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/logger"
)

const (
	dBusName          = "org.freedesktop.Notifications"
	dBusObjectPath    = "/org/freedesktop/Notifications"
	dBusInterfaceName = "org.freedesktop.Notifications"
)

// Server holds a connection to a notification server interactions.
type Server struct {
	conn *dbus.Conn
	obj  dbus.BusObject
}

// New returns new connection to a freedesktop.org message notification server.
//
// Each server offers specific capabilities. It is advised to provide graceful
// degradation of functionality, depending on the supported capabilities, so
// that the notification messages are useful on a wide range of desktop
// environments.
func New(conn *dbus.Conn) *Server {
	return &Server{
		conn: conn,
		obj:  conn.Object(dBusName, dBusObjectPath),
	}
}

// ServerInformation returns the information about the notification server.
func (srv *Server) ServerInformation() (name, vendor, version, specVersion string, err error) {
	call := srv.obj.Call(dBusInterfaceName+".GetServerInformation", 0)
	if err := call.Store(&name, &vendor, &version, &specVersion); err != nil {
		return "", "", "", "", err
	}
	return name, vendor, version, specVersion, nil
}

// ServerCapabilities returns the list of notification capabilities provided by the session.
func (srv *Server) ServerCapabilities() ([]ServerCapability, error) {
	call := srv.obj.Call(dBusInterfaceName+".GetCapabilities", 0)
	var caps []ServerCapability
	if err := call.Store(&caps); err != nil {
		return nil, err
	}
	return caps, nil
}

// SendNotification sends a new notification or updates an existing
// notification. In both cases the ID of the notification, as assigned by the
// server, is returned. The ID can be used to cancel a notification, update it
// or react to invoked user actions.
func (srv *Server) SendNotification(msg *Message) (ID, error) {
	call := srv.obj.Call(dBusInterfaceName+".Notify", 0,
		msg.AppName, msg.ReplacesID, msg.Icon, msg.Summary, msg.Body,
		flattenActions(msg.Actions), mapHints(msg.Hints),
		int32(msg.ExpireTimeout.Nanoseconds()/1e6))
	var id ID
	if err := call.Store(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func flattenActions(actions []Action) []string {
	result := make([]string, len(actions)*2)
	for i, action := range actions {
		result[i*2] = action.ActionKey
		result[i*2+1] = action.LocalizedText
	}
	return result
}

func mapHints(hints []Hint) map[string]dbus.Variant {
	result := make(map[string]dbus.Variant, len(hints))
	for _, hint := range hints {
		result[hint.Name] = dbus.MakeVariant(hint.Value)
	}
	return result
}

// CloseNotification closes a notification message with the given ID.
func (srv *Server) CloseNotification(id ID) error {
	call := srv.obj.Call(dBusInterfaceName+".CloseNotification", 0, id)
	return call.Store()
}

// ObserveNotifications blocks and processes message notification signals.
//
// The bus connection is configured to deliver signals from the notification
// server. All received signals are dispatched to the provided observer. This
// process continues until stopped by the context, or if an error occurs.
func (srv *Server) ObserveNotifications(ctx context.Context, observer Observer) (err error) {
	// TODO: upgrade godbus and use un-buffered channel.
	ch := make(chan *dbus.Signal, 10)
	defer close(ch)

	srv.conn.Signal(ch)
	defer srv.conn.RemoveSignal(ch)

	matchRules := []dbus.MatchOption{
		dbus.WithMatchSender(dBusName),
		dbus.WithMatchObjectPath(dBusObjectPath),
		dbus.WithMatchInterface(dBusInterfaceName),
	}
	if err := srv.conn.AddMatchSignal(matchRules...); err != nil {
		return err
	}
	defer func() {
		if err := srv.conn.RemoveMatchSignal(matchRules...); err != nil {
			// XXX: this should not fail for us in practice but we don't want
			// to clobber the actual error being returned from the function in
			// general, so ignore RemoveMatchSignal errors and just log them
			// instead.
			logger.Noticef("Cannot remove D-Bus signal matcher: %v", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case sig := <-ch:
			if err := processSignal(sig, observer); err != nil {
				return err
			}
		}
	}
}

func processSignal(sig *dbus.Signal, observer Observer) error {
	switch sig.Name {
	case dBusInterfaceName + ".NotificationClosed":
		if err := processNotificationClosed(sig, observer); err != nil {
			return fmt.Errorf("cannot process NotificationClosed signal: %v", err)
		}
	case dBusInterfaceName + ".ActionInvoked":
		if err := processActionInvoked(sig, observer); err != nil {
			return fmt.Errorf("cannot process ActionInvoked signal: %v", err)
		}
	}
	return nil
}

func processNotificationClosed(sig *dbus.Signal, observer Observer) error {
	if len(sig.Body) != 2 {
		return fmt.Errorf("unexpected number of body elements: %d", len(sig.Body))
	}
	id, ok := sig.Body[0].(uint32)
	if !ok {
		return fmt.Errorf("expected first body element to be uint32, got %T", sig.Body[0])
	}
	reason, ok := sig.Body[1].(uint32)
	if !ok {
		return fmt.Errorf("expected second body element to be uint32, got %T", sig.Body[1])
	}
	return observer.NotificationClosed(ID(id), CloseReason(reason))
}

func processActionInvoked(sig *dbus.Signal, observer Observer) error {
	if len(sig.Body) != 2 {
		return fmt.Errorf("unexpected number of body elements: %d", len(sig.Body))
	}
	id, ok := sig.Body[0].(uint32)
	if !ok {
		return fmt.Errorf("expected first body element to be uint32, got %T", sig.Body[0])
	}
	actionKey, ok := sig.Body[1].(string)
	if !ok {
		return fmt.Errorf("expected second body element to be string, got %T", sig.Body[1])
	}
	return observer.ActionInvoked(ID(id), actionKey)
}
