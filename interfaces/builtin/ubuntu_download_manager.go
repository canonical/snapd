// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package builtin

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
)

/* The methods: allowGSMDownload, createMmsDownload, exit and setDefaultThrottle
   are deliberately left out of this profile due to their privileged nature. */
const downloadConnectedPlugAppArmor = `
# Description: Can access the download manager.

#include <abstractions/dbus-session-strict>

# allow communicating with download-manager service
dbus (send)
     bus=session
     interface="org.freedesktop.DBus.Introspectable"
     path=/
     member=Introspect
     peer=(label=###SLOT_SECURITY_TAGS###),
dbus (send)
     bus=session
     interface="org.freedesktop.DBus.Introspectable"
     path=/com/canonical/applications/download/**
     member=Introspect
     peer=(label=###SLOT_SECURITY_TAGS###),
# Allow DownloadManager to send us signals, etc
dbus (receive)
     bus=session
     interface=com.canonical.applications.Download{,er}Manager
     peer=(label=###SLOT_SECURITY_TAGS###),
dbus (receive, send)
     bus=session
     path=/com/canonical/applications/download/@{PROFILE_DBUS}/**
     interface=com.canonical.applications.Download
     peer=(label=###SLOT_SECURITY_TAGS###),
dbus (receive)
     bus=session
     path=/com/canonical/applications/download/@{PROFILE_DBUS}/**
     interface=org.freedesktop.DBus.Properties
     peer=(label=###SLOT_SECURITY_TAGS###),
dbus (receive, send)
     bus=session
     path=/com/canonical/applications/download/@{PROFILE_DBUS}/**
     interface=com.canonical.applications.GroupDownload
     peer=(label=###SLOT_SECURITY_TAGS###),
# Be explicit about the allowed members we can send to
dbus (send)
     bus=session
     path=/
     interface=com.canonical.applications.DownloadManager
     member=createDownload
     peer=(label=###SLOT_SECURITY_TAGS###),
dbus (send)
     bus=session
     path=/
     interface=com.canonical.applications.DownloadManager
     member=createDownloadGroup
     peer=(label=###SLOT_SECURITY_TAGS###),
dbus (receive, send)
     bus=session
     path=/
     interface=com.canonical.applications.DownloadManager
     member=getAllDownloads
     peer=(label=###SLOT_SECURITY_TAGS###),
dbus (send)
     bus=session
     path=/
     interface=com.canonical.applications.DownloadManager
     member=getAllDownloadsWithMetadata
     peer=(label=###SLOT_SECURITY_TAGS###),
dbus (send)
     bus=session
     path=/
     interface=com.canonical.applications.DownloadManager
     member=defaultThrottle
     peer=(label=###SLOT_SECURITY_TAGS###),
dbus (send)
     bus=session
     path=/
     interface=com.canonical.applications.DownloadManager
     member=isGSMDownloadAllowed
     peer=(label=###SLOT_SECURITY_TAGS###),
`

const downloadPermanentSlotAppArmor = `
# Description: Allow operating as a download manager.

# DBus accesses
#include <abstractions/dbus-session-strict>

# https://specifications.freedesktop.org/download-spec/latest/
# allow binding to the DBus download interface
dbus (bind)
    bus=session
    name="com.canonical.applications.Downloader",

dbus (send)
    bus=session
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member="GetConnectionUnix{ProcessID,User}"
    peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (send)
    bus=session
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member="{RequestName,ReleaseName}"
    peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (send)
    bus=session
    path=/
    interface=org.freedesktop.DBus
    member="GetConnectionAppArmorSecurityContext"
    peer=(name=org.freedesktop.DBus, label=unconfined),
`

const downloadConnectedSlotAppArmor = `
# Allow connected clients to interact with the download manager
dbus (receive)
     bus=session
     path=/
     interface=com.canonical.applications.DownloadManager
     member=getAllDownloads
     peer=(label=###PLUG_SECURITY_TAGS###),

dbus (receive)
     bus=session
     path=/
     interface=com.canonical.applications.DownloadManager
     member=createDownload
     peer=(label=###PLUG_SECURITY_TAGS###),

dbus (receive)
     bus=session
     path=/com/canonical/applications/download/**
     interface=com.canonical.applications.Download
     peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=session
    path=/com/canonical/applications/download/**
    interface=com.canonical.applications.Download
    peer=(name=org.freedesktop.DBus, label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=session
    path=/com/canonical/applications/download/**
    interface=org.freedesktop.DBus
    peer=(name=org.freedesktop.DBus, label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=session
    path=/com/canonical/applications/download/**
    interface=org.freedesktop.DBus.Properties
    peer=(name=org.freedesktop.DBus, label=###PLUG_SECURITY_TAGS###),

# Allow writing to app download directories
owner @{HOME}/snap/###PLUG_NAME###/common/Downloads/    rw,
owner @{HOME}/snap/###PLUG_NAME###/common/Downloads/**  rwk,
`

type UbuntuDownloadManagerInterface struct{}

func (iface *UbuntuDownloadManagerInterface) Name() string {
	return "ubuntu-download-manager"
}

func (iface *UbuntuDownloadManagerInterface) String() string {
	return iface.Name()
}

func (iface *UbuntuDownloadManagerInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	old := "###SLOT_SECURITY_TAGS###"
	new := slotAppLabelExpr(slot)
	snippet := strings.Replace(downloadConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *UbuntuDownloadManagerInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *interfaces.Slot) error {
	spec.AddSnippet(downloadPermanentSlotAppArmor)
	return nil
}

func (iface *UbuntuDownloadManagerInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	old := "###PLUG_SECURITY_TAGS###"
	new := plugAppLabelExpr(plug)
	snippet := strings.Replace(downloadConnectedSlotAppArmor, old, new, -1)
	old = "###PLUG_NAME###"
	new = plug.Snap.Name()
	snippet = strings.Replace(snippet, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *UbuntuDownloadManagerInterface) SanitizePlug(slot *interfaces.Plug) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	return nil
}

func (iface *UbuntuDownloadManagerInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}
	return nil
}

func (iface *UbuntuDownloadManagerInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}

func (iface *UbuntuDownloadManagerInterface) ValidatePlug(plug *interfaces.Plug, attrs map[string]interface{}) error {
	return nil
}

func (iface *UbuntuDownloadManagerInterface) ValidateSlot(slot *interfaces.Slot, attrs map[string]interface{}) error {
	return nil
}
