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

package notificationtest

import (
	"fmt"
	"sort"
	"sync"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/dbusutil"
)

const (
	gtkBusName    = "org.gtk.Notifications"
	gtkObjectPath = "/org/gtk/Notifications"
	gtkInterface  = "org.gtk.Notifications"
)

type GtkNotification struct {
	DesktopID string
	ID        string
	Info      map[string]dbus.Variant
}

type GtkServer struct {
	conn *dbus.Conn
	err  *dbus.Error

	mu            sync.Mutex
	notifications map[string]*GtkNotification
}

func NewGtkServer() (*GtkServer, error) {
	conn := mylog.Check2(dbusutil.SessionBusPrivate())

	server := &GtkServer{
		conn:          conn,
		notifications: make(map[string]*GtkNotification),
	}
	conn.Export(gtkApi{server}, gtkObjectPath, gtkInterface)

	reply := mylog.Check2(conn.RequestName(gtkBusName, dbus.NameFlagDoNotQueue))

	if reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return nil, fmt.Errorf("cannot obtain bus name %q", gtkBusName)
	}
	return server, nil
}

func (server *GtkServer) Get(id string) *GtkNotification {
	server.mu.Lock()
	defer server.mu.Unlock()

	return server.notifications[id]
}

func (server *GtkServer) GetAll() []*GtkNotification {
	server.mu.Lock()
	defer server.mu.Unlock()

	notifications := make([]*GtkNotification, 0, len(server.notifications))
	for _, n := range server.notifications {
		notifications = append(notifications, n)
	}
	sort.Slice(notifications, func(i, j int) bool {
		return notifications[i].ID < notifications[j].ID
	})
	return notifications
}

func (server *GtkServer) ReleaseName() error {
	mylog.Check2(server.conn.ReleaseName(gtkBusName))

	return nil
}

func (server *GtkServer) Stop() error {
	mylog.Check2(server.conn.ReleaseName(gtkBusName))

	return server.conn.Close()
}

// SetError sets an error to be returned by the D-Bus interface.
//
// If not nil, all the gtkApi methods will return the provided error
// in place of performing their usual task.
func (server *GtkServer) SetError(err *dbus.Error) {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.err = err
}

func (server *GtkServer) Close(id string) error {
	server.mu.Lock()
	defer server.mu.Unlock()

	if _, ok := server.notifications[id]; !ok {
		return fmt.Errorf("no such notification: %s", id)
	}
	delete(server.notifications, id)

	// XXX: does real server emit any signal like with fdo?
	return nil
}

type gtkApi struct {
	server *GtkServer
}

func (a gtkApi) AddNotification(desktopID, id string, info map[string]dbus.Variant) *dbus.Error {
	a.server.mu.Lock()
	defer a.server.mu.Unlock()

	if a.server.err != nil {
		return a.server.err
	}

	notification := &GtkNotification{
		ID:        id,
		DesktopID: desktopID,
		Info:      info,
	}
	a.server.notifications[id] = notification
	return nil
}

func (a gtkApi) RemoveNotification(desktopId, id string) *dbus.Error {
	// Close() called below locks the server, so the error check must be
	// locked separately
	dErr := func() *dbus.Error {
		a.server.mu.Lock()
		defer a.server.mu.Unlock()
		return a.server.err
	}()
	if dErr != nil {
		return dErr
	}
	mylog.Check(a.server.Close(id))

	return nil
}
