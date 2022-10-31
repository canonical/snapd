// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020-2021 Canonical Ltd
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

// Package notification implements bindings to FDO D-Bus notification
// specification, version 1.2, as documented at https://developer.gnome.org/notification-spec/
// and GTK notification API. Appropriate notification backend is determined at
// runtime.
package notification

import (
	"fmt"
	"time"
)

// Message describes a single notification message.
//
// The notification should be related to a specific application, as indicated by
// AppName. In practice this should be the name of the desktop file and could be
// also accompanied by an appropriate hint indicating which icon to use.
//
// Message must include a summary and should include a body. The body may use
// HTML-like markup language to include bold, italic or underline text, as well
// as to include images and hyperlinks.
//
// A notification can automatically expire after the given number of
// milliseconds. This is separate from the notification being visible or
// invisible on-screen. Expired notifications are removed from persistent
// message roster, if one is supported. Two special values are recognized. When
// the expiration timeout is zero a message never expires. When the expiration
// timeout is -1 a message expires after a server-defined duration which may
// vary for the type of the notification message sent.
//
// A notification may optionally carry a number of hints that further customize it
// in a specific way. Refer to various hint constructors for details.
//
// A notification may optionally also carry one of several actions. If
// supported, actions can be invoked by the user, broadcasting a notification
// response back to the session. This mechanism only works if there is someone
// listening for the action being triggered.
//
// In all cases, the specific notification must take into account the
// capabilities of the server. For instance, if a server does not support body
// markup, then such markup is not automatically stripped by either the client
// or the server.
type Message struct {
	AppName       string
	Icon          string
	Title         string
	Body          string
	ExpireTimeout time.Duration // Effective resolution in milliseconds with 31-bit range.
	Priority      Priority
	Actions       []Action

	// XXX: only useful for fdo, should we drop it or rename to FdoHints?
	Hints []Hint
}

type Priority uint32

const (
	PriorityNormal Priority = iota
	PriorityLow
	PriorityHigh
	PriorityUrgent
)

// ServerSelectedExpireTimeout requests the server to pick an expiration timeout
// appropriate for the message type.
const ServerSelectedExpireTimeout = time.Millisecond * -1

// ID is the opaque identifier of a notification assigned by the server.
//
// Notifications with known identifiers can be closed or updated. The identifier
// is valid within one desktop session and should not be used unless the calling
// process initially sent the message.
type ID string

// Action describes a single notification action.
//
// ActionKey is returned in an D-Bus signal when an action is activated by the
// user. The text must be localized for the appropriate language.
type Action struct {
	ActionKey     string
	LocalizedText string
}

// Hint describes supplementeary information that may be used by the server.
//
// Various helpers create hint objects of specifc purpose.
//
// Specification: https://developer.gnome.org/notification-spec/#hints
type Hint struct {
	Name  string
	Value interface{}
}

// ServerCapability describes a single capability of the notification server.
type ServerCapability string

// CloseReason indicates why a notification message was closed.
type CloseReason uint32

const (
	// CloseReasonExpired indicates that a notification message has expired.
	CloseReasonExpired CloseReason = 1
	// CloseReasonDismissed indicates that a notification message was dismissed by the user.
	CloseReasonDismissed CloseReason = 2
	// CloseReasonClosed indicates that a notification message was closed with an API call.
	CloseReasonClosed CloseReason = 3
	// CloseReasonUndefined indicates that no other well-known reason applies.
	CloseReasonUndefined CloseReason = 4
)

// String implements the Stringer interface.
func (r CloseReason) String() string {
	switch r {
	case CloseReasonExpired:
		return "expired"
	case CloseReasonDismissed:
		return "dismissed"
	case CloseReasonClosed:
		return "closed"
	case CloseReasonUndefined:
		return "undefined"
	default:
		return fmt.Sprintf("CloseReason(%d)", uint32(r))
	}
}

// Observer is an interface for observing interactions with notification messages.
//
// An observer can be used to either observe a notification being closed or
// dismissed or to react to actions being invoked by the user. Practical
// implementations must remember the ID of the message they have sent to filter
// out other notifications.
type Observer interface {
	// NotificationClosed is called when a notification is either closed or removed
	// from the persistent roster.
	NotificationClosed(id ID, reason CloseReason) error
	// ActionInvoked is caliled when one of the notification message actions is
	// clicked by the user.
	// XXX: revisit, should we return id at all? Remap to ID?
	ActionInvoked(id uint32, actionKey string) error
}
