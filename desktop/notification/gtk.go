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

package notification

import (
	"context"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"
)

const (
	gtkBusName    = "org.gtk.Notifications"
	gtkObjectPath = "/org/gtk/Notifications"
	gtkInterface  = "org.gtk.Notifications"
)

type gtkBackend struct {
	conn      *dbus.Conn
	manager   dbus.BusObject
	desktopID string
	firstUse  time.Time
}

// TODO: support actions via session agent.
var newGtkBackend = func(conn *dbus.Conn, desktopID string) (NotificationManager, error) {
	// If the D-Bus service is not already running, assume it is
	// not available.
	// Use owner to verify that the return values of the method call have the
	// types we expect, which is generally a good validity check.
	var owner string
	mylog.Check(conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, gtkBusName).Store(&owner))

	b := &gtkBackend{
		conn:      conn,
		manager:   conn.Object(gtkBusName, gtkObjectPath),
		desktopID: desktopID,
		firstUse:  time.Now(),
	}
	return b, nil
}

func gtkPriority(priority Priority) string {
	switch priority {
	case PriorityLow:
		return "low"
	case PriorityNormal:
		return "normal"
	case PriorityHigh:
		return "high"
	case PriorityUrgent:
		return "urgent"
	default:
		return "normal" // default to normal
	}
}

type icon struct {
	Type  string
	Value dbus.Variant
}

func (srv *gtkBackend) SendNotification(id ID, msg *Message) error {
	info := make(map[string]dbus.Variant)
	if msg.Title != "" {
		info["title"] = dbus.MakeVariant(msg.Title)
	}
	if msg.Body != "" {
		info["body"] = dbus.MakeVariant(msg.Body)
	}
	if msg.Icon != "" {
		icon := icon{Type: "file", Value: dbus.MakeVariant(msg.Icon)}
		info["icon"] = dbus.MakeVariant(icon)
	}
	info["priority"] = dbus.MakeVariant(gtkPriority(msg.Priority))
	call := srv.manager.Call(gtkInterface+".AddNotification", 0, srv.desktopID, id, info)
	return call.Store()
}

func (srv *gtkBackend) CloseNotification(id ID) error {
	call := srv.manager.Call(gtkInterface+".RemoveNotification", 0, srv.desktopID, id)
	return call.Store()
}

func (srv *gtkBackend) HandleNotifications(context.Context) error {
	// do nothing
	return nil
}

func (srv *gtkBackend) IdleDuration() time.Duration {
	return time.Since(srv.firstUse)
}
