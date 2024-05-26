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
	"sync"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/logger"
)

const (
	dBusName          = "org.freedesktop.Notifications"
	dBusObjectPath    = "/org/freedesktop/Notifications"
	dBusInterfaceName = "org.freedesktop.Notifications"
)

// Server holds a connection to a notification server interactions.
type fdoBackend struct {
	conn            *dbus.Conn
	obj             dbus.BusObject
	mu              sync.Mutex
	serverToLocalID map[uint32]ID
	localToServerID map[ID]uint32
	lastRemove      time.Time
	desktopID       string
}

// New returns new connection to a freedesktop.org message notification server.
//
// Each server offers specific capabilities. It is advised to provide graceful
// degradation of functionality, depending on the supported capabilities, so
// that the notification messages are useful on a wide range of desktop
// environments.
var newFdoBackend = func(conn *dbus.Conn, desktopID string) NotificationManager {
	return &fdoBackend{
		conn:            conn,
		obj:             conn.Object(dBusName, dBusObjectPath),
		serverToLocalID: make(map[uint32]ID),
		localToServerID: make(map[ID]uint32),
		desktopID:       desktopID,
	}
}

// ServerInformation returns the information about the notification server.
func (srv *fdoBackend) ServerInformation() (name, vendor, version, specVersion string, err error) {
	call := srv.obj.Call(dBusInterfaceName+".GetServerInformation", 0)
	mylog.Check(call.Store(&name, &vendor, &version, &specVersion))

	return name, vendor, version, specVersion, nil
}

// ServerCapabilities returns the list of notification capabilities provided by the session.
func (srv *fdoBackend) ServerCapabilities() ([]ServerCapability, error) {
	call := srv.obj.Call(dBusInterfaceName+".GetCapabilities", 0)
	var caps []ServerCapability
	mylog.Check(call.Store(&caps))

	return caps, nil
}

// SendNotification sends a new notification or updates an existing
// notification. The id is a client-side id. fdoBackend remaps it internally
// to a server-assigned id.
func (srv *fdoBackend) SendNotification(id ID, msg *Message) error {
	hints := mapHints(msg.Hints)
	if _, ok := hints["urgency"]; !ok {
		hints["urgency"] = dbus.MakeVariant(fdoPriority(msg.Priority))
	}
	if _, ok := hints["desktop-entry"]; !ok {
		hints["desktop-entry"] = dbus.MakeVariant(srv.desktopID)
	}

	// serverSideId may be 0, but if it exists it is going to replace previous
	// notification with same local id.
	srv.mu.Lock()
	serverSideId := srv.localToServerID[id]
	srv.mu.Unlock()
	call := srv.obj.Call(dBusInterfaceName+".Notify", 0,
		msg.AppName, serverSideId, msg.Icon, msg.Title, msg.Body,
		flattenActions(msg.Actions), hints,
		int32(msg.ExpireTimeout.Nanoseconds()/1e6))
	mylog.Check(call.Store(&serverSideId))

	srv.mu.Lock()
	defer srv.mu.Unlock()
	srv.serverToLocalID[serverSideId] = id
	srv.localToServerID[id] = serverSideId
	return nil
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

func fdoPriority(priority Priority) uint8 {
	switch priority {
	case PriorityLow:
		return 0
	case PriorityNormal, PriorityHigh:
		return 1
	case PriorityUrgent:
		return 2
	default:
		return 1 // default to normal
	}
}

// CloseNotification closes a notification message with the given ID.
func (srv *fdoBackend) CloseNotification(id ID) error {
	srv.mu.Lock()
	serverSideId, ok := srv.localToServerID[id]
	srv.mu.Unlock()

	if !ok {
		return fmt.Errorf("unknown notification with id %q", id)
	}
	call := srv.obj.Call(dBusInterfaceName+".CloseNotification", 0, serverSideId)
	return call.Store()
}

func (srv *fdoBackend) IdleDuration() time.Duration {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.serverToLocalID) > 0 {
		return 0
	}
	return time.Since(srv.lastRemove)
}

func (srv *fdoBackend) HandleNotifications(ctx context.Context) error {
	return srv.ObserveNotifications(ctx, nil)
}

// ObserveNotifications blocks and processes message notification signals.
//
// The bus connection is configured to deliver signals from the notification
// server. All received signals are dispatched to the provided observer. This
// process continues until stopped by the context, or if an error occurs.
func (srv *fdoBackend) ObserveNotifications(ctx context.Context, observer Observer) (err error) {
	// TODO: upgrade godbus and use un-buffered channel.
	ch := make(chan *dbus.Signal, 10)

	// XXX: do not close as this may lead to panic on already closed channel due
	// to https://github.com/godbus/dbus/issues/271
	// defer close(ch)

	srv.conn.Signal(ch)
	defer srv.conn.RemoveSignal(ch)

	matchRules := []dbus.MatchOption{
		dbus.WithMatchSender(dBusName),
		dbus.WithMatchObjectPath(dBusObjectPath),
		dbus.WithMatchInterface(dBusInterfaceName),
	}
	mylog.Check(srv.conn.AddMatchSignal(matchRules...))

	defer func() {
		mylog.Check(srv.conn.RemoveMatchSignal(matchRules...))
		// XXX: this should not fail for us in practice but we don't want
		// to clobber the actual error being returned from the function in
		// general, so ignore RemoveMatchSignal errors and just log them
		// instead.
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case sig, ok := <-ch:
			if !ok {
				return nil
			}
			mylog.Check(srv.processSignal(sig, observer))

		}
	}
}

func (srv *fdoBackend) processSignal(sig *dbus.Signal, observer Observer) error {
	switch sig.Name {
	case dBusInterfaceName + ".NotificationClosed":
		mylog.Check(srv.processNotificationClosed(sig, observer))

	case dBusInterfaceName + ".ActionInvoked":
		mylog.Check(srv.processActionInvoked(sig, observer))

	}
	return nil
}

func (srv *fdoBackend) processNotificationClosed(sig *dbus.Signal, observer Observer) error {
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

	srv.mu.Lock()

	// we may receive signals for notifications we don't know about, silently
	// ignore them.
	localID, ok := srv.serverToLocalID[id]
	if !ok {
		srv.mu.Unlock()
		return nil
	}

	delete(srv.localToServerID, localID)
	delete(srv.serverToLocalID, id)
	if len(srv.serverToLocalID) == 0 {
		srv.lastRemove = time.Now()
	}

	// unlock the mutex before calling observer
	srv.mu.Unlock()

	if observer != nil {
		return observer.NotificationClosed(localID, CloseReason(reason))
	}
	return nil
}

func (srv *fdoBackend) processActionInvoked(sig *dbus.Signal, observer Observer) error {
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

	if observer != nil {
		return observer.ActionInvoked(id, actionKey)
	}
	return nil
}
