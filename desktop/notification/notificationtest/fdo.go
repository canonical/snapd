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
	fdoBusName    = "org.freedesktop.Notifications"
	fdoObjectPath = "/org/freedesktop/Notifications"
	fdoInterface  = "org.freedesktop.Notifications"
)

type FdoNotification struct {
	ID      uint32
	AppName string
	Icon    string
	Summary string
	Body    string
	Actions []string
	Hints   map[string]dbus.Variant
	Expires int32
}

type FdoServer struct {
	conn *dbus.Conn
	err  *dbus.Error

	mu            sync.Mutex
	lastID        uint32
	notifications map[uint32]*FdoNotification
}

func NewFdoServer() (*FdoServer, error) {
	conn := mylog.Check2(dbusutil.SessionBusPrivate())

	server := &FdoServer{
		conn:          conn,
		notifications: make(map[uint32]*FdoNotification),
	}
	conn.Export(fdoApi{server}, fdoObjectPath, fdoInterface)

	reply := mylog.Check2(conn.RequestName(fdoBusName, dbus.NameFlagDoNotQueue))

	if reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return nil, fmt.Errorf("cannot obtain bus name %q", fdoBusName)
	}
	return server, nil
}

func (server *FdoServer) Stop() error {
	mylog.Check2(server.conn.ReleaseName(fdoBusName))

	return server.conn.Close()
}

// SetError sets an error to be returned by the D-Bus interface.
//
// If not nil, all the fdoApi methods will return the provided error
// in place of performing their usual task.
func (server *FdoServer) SetError(err *dbus.Error) {
	server.mu.Lock()
	defer server.mu.Unlock()

	server.err = err
}

func (server *FdoServer) Get(id uint32) *FdoNotification {
	server.mu.Lock()
	defer server.mu.Unlock()

	return server.notifications[id]
}

func (server *FdoServer) GetAll() []*FdoNotification {
	server.mu.Lock()
	defer server.mu.Unlock()

	notifications := make([]*FdoNotification, 0, len(server.notifications))
	for _, n := range server.notifications {
		notifications = append(notifications, n)
	}
	sort.Slice(notifications, func(i, j int) bool {
		return notifications[i].ID < notifications[j].ID
	})
	return notifications
}

func (server *FdoServer) Close(id, reason uint32) error {
	server.mu.Lock()
	defer server.mu.Unlock()

	if _, ok := server.notifications[id]; !ok {
		return fmt.Errorf("No such notification: %d", id)
	}
	delete(server.notifications, id)
	return server.conn.Emit(fdoObjectPath, fdoInterface+".NotificationClosed", id, reason)
}

func (server *FdoServer) InvokeAction(id uint32, actionKey string) error {
	return server.conn.Emit(fdoObjectPath, fdoInterface+".ActionInvoked", id, actionKey)
}

type fdoApi struct {
	server *FdoServer
}

func (a fdoApi) GetCapabilities() ([]string, *dbus.Error) {
	a.server.mu.Lock()
	defer a.server.mu.Unlock()

	if a.server.err != nil {
		return nil, a.server.err
	}

	return []string{"cap-foo", "cap-bar"}, nil
}

func (a fdoApi) Notify(appName string, replacesID uint32, icon, summary, body string, actions []string, hints map[string]dbus.Variant, expires int32) (uint32, *dbus.Error) {
	a.server.mu.Lock()
	defer a.server.mu.Unlock()

	if a.server.err != nil {
		return 0, a.server.err
	}

	a.server.lastID += 1
	notification := &FdoNotification{
		ID:      a.server.lastID,
		AppName: appName,
		Icon:    icon,
		Summary: summary,
		Body:    body,
		Actions: actions,
		Hints:   hints,
		Expires: expires,
	}
	if replacesID != 0 {
		delete(a.server.notifications, replacesID)
	}
	a.server.notifications[notification.ID] = notification

	return notification.ID, nil
}

func (a fdoApi) CloseNotification(id uint32) *dbus.Error {
	dErr := func() *dbus.Error {
		a.server.mu.Lock()
		defer a.server.mu.Unlock()
		return a.server.err
	}()
	if dErr != nil {
		return dErr
	}
	mylog.Check(

		// close reason 3 is "closed by a call to CloseNotification"
		// https://specifications.freedesktop.org/notification-spec/latest/ar01s09.html#signal-notification-closed
		a.server.Close(id, 3))

	return nil
}

func (a fdoApi) GetServerInformation() (name, vendor, version, specVersion string, err *dbus.Error) {
	a.server.mu.Lock()
	defer a.server.mu.Unlock()

	if a.server.err != nil {
		return "", "", "", "", a.server.err
	}

	return "name", "vendor", "version", "specVersion", nil
}
