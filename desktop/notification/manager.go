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

type NotificationManager interface {
	SendNotification(id ID, msg *Message) error
	CloseNotification(id ID) error
	IdleDuration() time.Duration

	HandleNotifications(ctx context.Context) error
}

func NewNotificationManager(conn *dbus.Conn, desktopID string) NotificationManager {
	// first try the GTK backend
	if manager := mylog.Check2(newGtkBackend(conn, desktopID)); err == nil {
		return manager
	}

	// fallback to the older FDO API
	return newFdoBackend(conn, desktopID)
}
