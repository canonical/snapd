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

import "fmt"

// WithActionIcons returns a hint asking the server to use action key as icon names.
//
// A server that has the "action-icons" capability will attempt to interpret any
// action key as a named icon. The localized display name will be used to
// annotate the icon for accessibility purposes. The icon name should be
// compliant with the Freedesktop.org Icon Naming Specification.
//
// Requires server version >= 1.2
func WithActionIcons() Hint {
	t := true
	return Hint{Name: "action-icons", Value: &t}
}

// Urgency describes the importance of a notification message.
//
// Specification: https://developer.gnome.org/notification-spec/#urgency-levels
type Urgency byte

const (
	// LowUrgency indicates that a notification message is below normal priority.
	LowUrgency Urgency = 0
	// NormalUrgency indicates that a notification message has the regular priority.
	NormalUrgency Urgency = 1
	// CriticalUrgency indicates that a notification message is above normal priority.
	CriticalUrgency Urgency = 2
)

// String implements the Stringer interface.
func (u Urgency) String() string {
	switch u {
	case LowUrgency:
		return "low"
	case NormalUrgency:
		return "normal"
	case CriticalUrgency:
		return "critical"
	default:
		return fmt.Sprintf("Urgency(%d)", byte(u))
	}
}

// WithUrgency returns a hint asking the server to set message urgency.
//
// Notification servers may show messages with higher urgency before messages
// with lower urgency. In addition some urgency levels may not be shown when the
// user has enabled a do-not-distrub mode.
func WithUrgency(u Urgency) Hint {
	return Hint{Name: "urgency", Value: &u}
}

// Category is a string indicating the category of a notification message.
//
// Specification: https://developer.gnome.org/notification-spec/#categories
type Category string

const (
	// DeviceCategory is a generic notification category related to hardware devices.
	DeviceCategory Category = "device"
	// DeviceAddedCategory indicates that a device was added to the system.
	DeviceAddedCategory Category = "device.added"
	// DeviceErrorCategory indicates that a device error occurred.
	DeviceErrorCategory Category = "device.error"
	// DeviceRemovedCategory indicates that a device was removed from the system.
	DeviceRemovedCategory Category = "device.removed"

	// EmailCategory is a generic notification category related to electronic mail.
	EmailCategory Category = "email"
	// EmailArrivedCategory indicates that an e-mail has arrived.
	EmailArrivedCategory Category = "email.arrived"
	// EmailBouncedCategory indicates that an e-mail message has bounced.
	EmailBouncedCategory Category = "email.bounced"

	// InstantMessageCategory is a generic notification category related to instant messages.
	InstantMessageCategory Category = "im"
	// InstantMessageErrorCategory indicates that an instant message error occurred.
	InstantMessageErrorCategory Category = "im.error"
	// InstantMessageReceivedCategory indicates that an instant mesage has been received.
	InstantMessageReceivedCategory Category = "im.received"

	// NetworkCategory is a generic notification category related to network.
	NetworkCategory Category = "network"
	// NetworkConnectedCategory indicates that a network connection has been established.
	NetworkConnectedCategory Category = "network.connected"
	// NetworkDisconnectedCategory indicates that a network connection has been lost.
	NetworkDisconnectedCategory Category = "network.disconnected"
	// NetworkErrorCategory indicates that a network error occurred.
	NetworkErrorCategory Category = "network.error"

	// PresenceCategory is a generic notification category related to on-line presence.
	PresenceCategory Category = "presence"
	// PresenceOfflineCategory indicates that a contact disconnected from the network.
	PresenceOfflineCategory Category = "presence.offline"
	// PresenceOnlineCategory indicates that a contact connected to the network.
	PresenceOnlineCategory Category = "presence.online"

	// TransferCategory is a generic notification category for file transfers or downloads.
	TransferCategory Category = "transfer"
	// TransferCompleteCategory indicates that a file transfer has completed.
	TransferCompleteCategory Category = "transfer.complete"
	// TransferErrorCategory indicates that a file transfer error occurred.
	TransferErrorCategory Category = "transfer.error"
)

// WithCategory returns a hint asking the server to set message category.
func WithCategory(c Category) Hint {
	return Hint{Name: "category", Value: &c}
}

// WithDesktopEntry returns a hint asking the server to associate a desktop file with a message.
//
// The desktopEntryName is the name of the desktop file without the ".desktop"
// extension. The server may use this information to derive correct icon, for
// logging, etc.
func WithDesktopEntry(desktopEntryName string) Hint {
	return Hint{Name: "desktop-entry", Value: &desktopEntryName}
}

// WithTransient returns a hint asking the server to bypass message persistence.
//
// When set the server will treat the notification as transient and by-pass the
// server's persistence capability, if it should exist.
//
// Requires server version >= 1.2
func WithTransient() Hint {
	t := true
	return Hint{Name: "transient", Value: &t}
}

// WithResident returns a hint asking the server to keep the message after an action is invoked.
//
// When set the server will not automatically remove the notification when an
// action has been invoked. The notification will remain resident in the server
// until it is explicitly removed by the user or by the sender. This hint is
// likely only useful when the server has the "persistence" capability.
//
// Requires server version >= 1.2
func WithResident() Hint {
	t := true
	return Hint{Name: "resident", Value: &t}
}

// WithPointToX returns a hint asking the server to point the notification at a specific X coordinate.
//
// The coordinate is in desktop pixel units. Both X and Y hints must be included in the message.
func WithPointToX(x int) Hint {
	return Hint{Name: "x", Value: &x}
}

// WithPointToY returns a hint asking the server to point the notification at a specific Y coordinate.
//
// The coordinate is in desktop pixel units. Both X and Y hints must be included in the message.
func WithPointToY(y int) Hint {
	return Hint{Name: "y", Value: &y}
}

// WithImageFile returns a hint asking the server display an image loaded from file.
//
// When multiple hits related to images are used, the following priority list applies:
// 1) "image-data" (not implemented in Go).
// 2) "image-path", as provided by WithImageFile.
// 3) Message.Icon field.
func WithImageFile(path string) Hint {
	// The hint name is image-path but the function is called WithImageFile for consistency with WithSoundFile.
	return Hint{Name: "image-path", Value: &path}
}

// TODO: add WithImageData

// WithSoundFile returns a hint asking the server to play a sound loaded from file.
func WithSoundFile(path string) Hint {
	return Hint{Name: "sound-file", Value: &path}
}

// WithSoundName returns a hint asking the server to play a sound from the sound theme.
func WithSoundName(name string) Hint {
	return Hint{Name: "sound-name", Value: &name}
}

// WithSuppressSound returns a hint asking the server not to play any notification sounds.
func WithSuppressSound() Hint {
	t := true
	return Hint{Name: "suppress-sound", Value: &t}
}
