// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"bytes"
	"fmt"

	"github.com/snapcore/snapd/interfaces"
)

/* The methods: allowGSMDownload, createMmsDownload, exit and setDefaultThrottle
   are deliberately left out of this profile due to their privileged nature. */
var downloadConnectedPlugAppArmor = []byte(`
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
`)

var downloadPermanentSlotAppArmor = []byte(`
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
`)

var downloadConnectedSlotAppArmor = []byte(`
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
`)

var downloadConnectedPlugSecComp = []byte(`
# Description: Can access download manager.

# dbus
connect
recvmsg
send
sendto
sendmsg
socket
`)

var downloadPermanentSlotSecComp = []byte(`
# Description: Can act as a download manager.

# dbus
connect
recvmsg
send
sendto
sendmsg
socket
`)

type UbuntuDownloadManagerInterface struct{}

func (iface *UbuntuDownloadManagerInterface) Name() string {
	return "ubuntu-download-manager"
}

func (iface *UbuntuDownloadManagerInterface) String() string {
	return iface.Name()
}

func (iface *UbuntuDownloadManagerInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *UbuntuDownloadManagerInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet := bytes.Replace(downloadConnectedPlugAppArmor, old, new, -1)
		return snippet, nil
	case interfaces.SecuritySecComp:
		return downloadConnectedPlugSecComp, nil
	}
	return nil, nil
}

func (iface *UbuntuDownloadManagerInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return downloadPermanentSlotAppArmor, nil
	case interfaces.SecuritySecComp:
		return downloadPermanentSlotSecComp, nil
	}
	return nil, nil
}

func (iface *UbuntuDownloadManagerInterface) ConnectedSlotSnippet(plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###PLUG_SECURITY_TAGS###")
		new := plugAppLabelExpr(plug)
		snippet := bytes.Replace(downloadConnectedSlotAppArmor, old, new, -1)
		old = []byte("###PLUG_NAME###")
		new = []byte(plug.Snap.Name())
		snippet = bytes.Replace(snippet, old, new, -1)
		return snippet, nil
	}
	return nil, nil
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

func (iface *UbuntuDownloadManagerInterface) ValidatePlug(plug *interfaces.Plug, attrs map[string]interface{}) error {
	return nil
}

func (iface *UbuntuDownloadManagerInterface) ValidateSlot(slot *interfaces.Slot, attrs map[string]interface{}) error {
	return nil
}

func (iface *UbuntuDownloadManagerInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
