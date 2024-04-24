// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
)

const desktopLegacySummary = `allows privileged access to desktop legacy methods`

// While this gives privileged access to legacy methods we should auto-connect
// this transitional interface since most desktop applications will need it.
// When safe alternative methods are added to the desktop interface by default,
// we can consider making this manually connected.
const desktopLegacyBaseDeclarationSlots = `
  desktop-legacy:
    allow-installation:
      slot-snap-type:
        - core
`

const desktopLegacyConnectedPlugAppArmor = `
# Description: Can access common desktop legacy methods. This gives privileged
# access to the user's input.

# accessibility (a11y)
#include <abstractions/dbus-session-strict>
dbus (send)
    bus=session
    path=/org/a11y/bus
    interface=org.a11y.Bus
    member=GetAddress
    peer=(label=unconfined),

#include <abstractions/dbus-accessibility-strict>

# Allow access to the non-abstract D-Bus socket used by at-spi > 2.42.0
#   https://gitlab.gnome.org/GNOME/at-spi2-core/-/issues/43
owner /{,var/}run/user/[0-9]*/at-spi/bus* rw,

# Allow access to the socket used by speech-dispatcher
owner /{,var/}run/user/[0-9]*/speech-dispatcher/speechd.sock rw,

# Allow the accessibility services in the user session to send us any events
dbus (receive)
    bus=accessibility
    peer=(label=unconfined),

# Allow querying for capabilities and registering
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/accessible/root"
    interface="org.a11y.atspi.Socket"
    member="Embed"
    peer=(name=org.a11y.atspi.Registry, label=unconfined),
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/registry"
    interface="org.a11y.atspi.Registry"
    member="GetRegisteredEvents"
    peer=(name=org.a11y.atspi.Registry, label=unconfined),
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/registry/deviceeventcontroller"
    interface="org.a11y.atspi.DeviceEventController"
    member="Get{DeviceEvent,Keystroke}Listeners"
    peer=(name=org.a11y.atspi.Registry, label=unconfined),
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/registry/deviceeventcontroller"
    interface="org.a11y.atspi.DeviceEventController"
    member="NotifyListenersSync"
    peer=(name=org.a11y.atspi.Registry, label=unconfined),

# org.a11y.atspi is not designed for application isolation and these rules
# can be used to send change events for other processes.
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/accessible/root"
    interface="org.a11y.atspi.Event.Object"
    member="ChildrenChanged"
    peer=(name=org.freedesktop.DBus, label=unconfined),
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/accessible/root"
    interface="org.a11y.atspi.Accessible"
    member="Get*"
    peer=(label=unconfined),
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/accessible/[0-9]*"
    interface="org.a11y.atspi.Event.Object"
    member="{ChildrenChanged,PropertyChange,StateChanged,TextCaretMoved}"
    peer=(name=org.freedesktop.DBus, label=unconfined),
dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/accessible/[0-9]*"
    interface="org.freedesktop.DBus.Properties"
    member="Get{,All}"
    peer=(label=unconfined),

dbus (send)
    bus=accessibility
    path="/org/a11y/atspi/cache"
    interface="org.a11y.atspi.Cache"
    member="{Add,Remove}Accessible"
    peer=(name=org.freedesktop.DBus, label=unconfined),


# ibus
# subset of ibus abstraction
/usr/lib/@{multiarch}/gtk-2.0/[0-9]*/immodules/im-ibus.so mr,
owner @{HOME}/.config/ibus/      r,
owner @{HOME}/.config/ibus/bus/  r,
owner @{HOME}/.config/ibus/bus/* r,

# allow communicating with ibus-daemon (this allows sniffing key events)
unix (connect, receive, send)
    type=stream
    peer=(addr="@/tmp/ibus/dbus-*"),

# abstract path in ibus >= 1.5.22 uses $XDG_CACHE_HOME (ie, @{HOME}/.cache)
# This should use this, but due to LP: #1856738 we cannot
#unix (connect, receive, send)
#    type=stream
#    peer=(addr="@@{HOME}/.cache/ibus/dbus-*"),
unix (connect, receive, send)
     type=stream
     peer=(addr="@/home/*/.cache/ibus/dbus-*"),

# when running with glib >= 2.75.0, ibus uses a regular socket
owner @{HOME}/.cache/ibus/dbus-* rw,


# mozc
# allow communicating with mozc server
unix (connect, receive, send)
     type=stream
     peer=(addr="@tmp/.mozc.*"),


# fcitx
# allow communicating with fcitx dbus service
dbus send
    bus=fcitx
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member={Hello,AddMatch,RemoveMatch,GetNameOwner,NameHasOwner,StartServiceByName}
    peer=(name=org.freedesktop.DBus),

owner @{HOME}/.config/fcitx/dbus/* r,

# allow creating an input context
dbus send
    bus={fcitx,session}
    path=/inputmethod
    interface=org.fcitx.Fcitx.InputMethod
    member=CreateIC*
    peer=(label=unconfined),

# allow setting up and tearing down the input context
dbus send
    bus={fcitx,session}
    path=/inputcontext_[0-9]*
    interface=org.fcitx.Fcitx.InputContext
    member="{Close,Destroy,Enable}IC"
    peer=(label=unconfined),

dbus send
    bus={fcitx,session}
    path=/inputcontext_[0-9]*
    interface=org.fcitx.Fcitx.InputContext
    member=Reset
    peer=(label=unconfined),

# allow service to send us signals
dbus receive
    bus=fcitx
    peer=(label=unconfined),

dbus receive
    bus=session
    interface=org.fcitx.Fcitx.*
    peer=(label=unconfined),

# use the input context
dbus send
    bus={fcitx,session}
    path=/inputcontext_[0-9]*
    interface=org.fcitx.Fcitx.InputContext
    member="Focus{In,Out}"
    peer=(label=unconfined),

dbus send
    bus={fcitx,session}
    path=/inputcontext_[0-9]*
    interface=org.fcitx.Fcitx.InputContext
    member="{CommitPreedit,Set*}"
    peer=(label=unconfined),

# this is an information leak and allows key and mouse sniffing. If the input
# context path were tied to the process' security label, this would not be an
# issue.
dbus send
    bus={fcitx,session}
    path=/inputcontext_[0-9]*
    interface=org.fcitx.Fcitx.InputContext
    member="{MouseEvent,ProcessKeyEvent}"
    peer=(label=unconfined),

# this method does not exist with the sunpinyin backend (at least), so allow
# it for other input methods. This may consitute an information leak (which,
# again, could be avoided if the path were tied to the process' security
# label).
dbus send
    bus={fcitx,session}
    path=/inputcontext_[0-9]*
    interface=org.freedesktop.DBus.Properties
    member=GetAll
    peer=(label=unconfined),

# gtk2/gvfs gtk_show_uri()
dbus (send)
    bus=session
    path=/org/gtk/vfs/mounttracker
    interface=org.gtk.vfs.MountTracker
    member=ListMountableInfo,
dbus (send)
    bus=session
    path=/org/gtk/vfs/mounttracker
    interface=org.gtk.vfs.MountTracker
    member=LookupMount,

###SNAP_DESKTOP_FILE_RULES###
# Snaps are unable to use the data in mimeinfo.cache (since they can't execute
# the returned desktop file themselves). unity messaging menu doesn't require
# mimeinfo.cache and xdg-mime will fallback to reading the desktop files
# directly to look for MimeType. Since reading the snap's own desktop files is
# allowed, we can safely deny access to this file (and xdg-mime will either
# return one of the snap's mimetypes, or none).
deny /var/lib/snapd/desktop/applications/mimeinfo.cache r,

# glib-networking's GLib proxy (different than the portal's proxy service
# org.freedesktop.portal.ProxyResolver). The Lookup API allows specifying
# various URLs (eg, file://, http:// and https://) which will be given to the
# unconfined glib-pacrunner.
dbus (send)
    bus=session
    path=/org/gtk/GLib/PACRunner
    interface=org.gtk.GLib.PACRunner
    member=Lookup
    peer=(label=unconfined),

# app-indicators
dbus (send)
    bus=session
    path=/StatusNotifierWatcher
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(name=org.kde.StatusNotifierWatcher, label=unconfined),

dbus (send)
    bus=session
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member="{GetConnectionUnixProcessID,RequestName,ReleaseName}"
    peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (bind)
    bus=session
    name=org.kde.StatusNotifierItem-[0-9]*,

dbus (send)
    bus=session
    path=/StatusNotifierWatcher
    interface=org.freedesktop.DBus.Properties
    member=Get
    peer=(name=org.kde.StatusNotifierWatcher, label=unconfined),

dbus (send)
    bus=session
    path=/{StatusNotifierWatcher,org/ayatana/NotificationItem/*}
    interface=org.kde.StatusNotifierWatcher
    member=RegisterStatusNotifierItem
    peer=(label="{plasmashell,unconfined}"),

dbus (send)
    bus=session
    path=/{StatusNotifierItem,org/ayatana/NotificationItem/*}
    interface=org.kde.StatusNotifierItem
    member="New{AttentionIcon,Icon,IconThemePath,OverlayIcon,Status,Title,ToolTip}"
    peer=(name=org.freedesktop.DBus, label="{plasmashell,unconfined}"),

dbus (receive)
    bus=session
    path=/{StatusNotifierItem,org/ayatana/NotificationItem/*}
    interface=org.kde.StatusNotifierItem
    member={Activate,ContextMenu,Scroll,SecondaryActivate,ProvideXdgActivationToken,XAyatanaSecondaryActivate}
    peer=(label="{plasmashell,unconfined}"),

dbus (send)
    bus=session
    path=/{StatusNotifierItem/menu,org/ayatana/NotificationItem/*/Menu}
    interface=com.canonical.dbusmenu
    member="{LayoutUpdated,ItemsPropertiesUpdated}"
    peer=(name=org.freedesktop.DBus, label="{plasmashell,unconfined}"),

dbus (receive)
    bus=session
    path=/{StatusNotifierItem,StatusNotifierItem/menu,org/ayatana/NotificationItem/**}
    interface={org.freedesktop.DBus.Properties,com.canonical.dbusmenu}
    member={Get*,AboutTo*,Event*}
    peer=(label="{plasmashell,unconfined}"),

# notifications
dbus (send)
    bus=session
    path=/org/freedesktop/Notifications
    interface=org.freedesktop.Notifications
    member="{GetCapabilities,GetServerInformation,Notify,CloseNotification}"
    peer=(label=unconfined),

dbus (receive)
    bus=session
    path=/org/freedesktop/Notifications
    interface=org.freedesktop.Notifications
    member={ActionInvoked,NotificationClosed,NotificationReplied}
    peer=(label=unconfined),

# KDE Plasma's Inhibited property indicating "do not disturb" mode
# https://invent.kde.org/plasma/plasma-workspace/-/blob/master/libnotificationmanager/dbus/org.freedesktop.Notifications.xml#L42
dbus (send)
    bus=session
    path=/org/freedesktop/Notifications
    interface=org.freedesktop.DBus.Properties
    member="Get{,All}"
    peer=(label=unconfined),

dbus (receive)
    bus=session
    path=/org/freedesktop/Notifications
    interface=org.freedesktop.DBus.Properties
    member=PropertiesChanged
    peer=(label=unconfined),

dbus (send)
    bus=session
    path=/org/ayatana/NotificationItem/*
    interface=org.kde.StatusNotifierItem
    member=XAyatanaNew*
    peer=(name=org.freedesktop.DBus, label="{plasmashell,unconfined}"),
`

const desktopLegacyConnectedPlugSecComp = `
# Description: Can access common desktop legacy methods. This gives privileged
# access to the user's input.

listen
accept
accept4
`

type desktopLegacyInterface struct {
	commonInterface
}

func (iface *desktopLegacyInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	snippet := strings.Join(getDesktopFileRules(plug.Snap().DesktopPrefix()), "\n")
	spec.AddSnippet(strings.Replace(desktopLegacyConnectedPlugAppArmor, "###SNAP_DESKTOP_FILE_RULES###", snippet+"\n", -1))

	return nil
}

func init() {
	registerIface(&desktopLegacyInterface{
		commonInterface: commonInterface{
			name:                 "desktop-legacy",
			summary:              desktopLegacySummary,
			implicitOnClassic:    true,
			baseDeclarationSlots: desktopLegacyBaseDeclarationSlots,
			connectedPlugSecComp: desktopLegacyConnectedPlugSecComp,
		},
	})
}
