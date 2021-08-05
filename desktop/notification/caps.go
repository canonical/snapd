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

const (
	// ActionIconsCapability indicates that the server supports using icons
	// instead of text for displaying actions. Using icons for actions must be
	// enabled on a per-notification basis using the "action-icons" hint.
	ActionIconsCapability ServerCapability = "action-icons"
	// ActionsCapability indicates that the server will provide the specified
	// actions to the user. Even if this cap is missing, actions may still be
	// specified by the client, however the server is free to ignore them.
	ActionsCapability ServerCapability = "actions"
	// BodyCapability indicates that the server supports body text. Some
	// implementations may only show the summary (for instance, onscreen
	// displays, marquee/scrollers).
	BodyCapability ServerCapability = "body"
	// BodyHyperlinksCapability indicates that the server supports hyperlinks in
	// the notifications.
	BodyHyperlinksCapability ServerCapability = "body-hyperlinks"
	// BodyImagesCapability indicates that the server supports images in the
	// notifications.
	BodyImagesCapability ServerCapability = "body-images"
	// BodyMarkupCapability indicates that the server supports markup in the
	// body text. If marked up text is sent to a server that does not give this
	// cap, the markup will show through as regular text so must be stripped
	// clientside.
	BodyMarkupCapability ServerCapability = "body-markup"
	// IconMultiCapability indicates that the server will render an animation of
	// all the frames in a given image array. The client may still specify
	// multiple frames even if this cap and/or "icon-static" is missing, however
	// the server is free to ignore them and use only the primary frame.
	IconMultiCapability ServerCapability = "icon-multi"
	// IconStaticCapability indicates that the server supports display of
	// exactly one frame of any given image array. This value is mutually
	// exclusive with "icon-multi", it is a protocol error for the server to
	// specify both.
	IconStaticCapability ServerCapability = "icon-static"
	// PersistenceCapability indicates that the server supports persistence of
	// notifications. Notifications will be retained until they are acknowledged
	// or removed by the user or recalled by the sender. The presence of this
	// capability allows clients to depend on the server to ensure a
	// notification is seen and eliminate the need for the client to display a
	// reminding function (such as a status icon) of its own.
	PersistenceCapability ServerCapability = "persistence"
	// SoundCapability indicates that the server supports sounds on
	// notifications. If returned, the server must support the "sound-file" and
	// "suppress-sound" hints.
	SoundCapability ServerCapability = "sound"
)
