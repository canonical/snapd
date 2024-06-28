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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
)

const unity7Summary = `allows interacting with Unity 7 services`

const unity7BaseDeclarationSlots = `
  unity7:
    allow-installation:
      slot-snap-type:
        - core
`

const unity7ConnectedPlugAppArmor = `
# Description: Can access Unity7. Note, Unity 7 runs on X and requires access
# to various DBus services and this environment does not prevent eavesdropping
# or apps interfering with one another.

#include <abstractions/dbus-strict>
#include <abstractions/dbus-session-strict>

# Allow finding the DBus session bus id (eg, via dbus_bus_get_id())
dbus (send)
     bus=session
     path=/org/freedesktop/DBus
     interface=org.freedesktop.DBus
     member=GetId
     peer=(name=org.freedesktop.DBus, label=unconfined),

#include <abstractions/X>

#include <abstractions/fonts>
owner @{HOME}/.local/share/fonts/{,**} r,
/var/cache/fontconfig/   r,
/var/cache/fontconfig/** mr,

# subset of gnome abstraction
/etc/gnome/defaults.list r,

/etc/gtk-*/*                               r,
/usr/lib{,32,64}/gtk-*/**                  mr,
/usr/lib{,32,64}/gdk-pixbuf-*/**           mr,
/usr/lib/@{multiarch}/gtk-*/**             mr,
/usr/lib/@{multiarch}/gdk-pixbuf-*/**      mr,

/etc/pango/*                               r,
/usr/lib{,32,64}/pango/**                  mr,
/usr/lib/@{multiarch}/pango/**             mr,

/usr/share/icons/                          r,
/usr/share/icons/**                        r,
/usr/share/icons/*/index.theme             rk,
/usr/share/pixmaps/                        r,
/usr/share/pixmaps/**                      r,

# The snapcraft desktop part may look for schema files in various locations, so
# allow reading system installed schemas.
/usr/share/glib*/schemas/{,*}              r,

# Snappy's 'xdg-open' talks to the snapd-xdg-open service which currently works
# only in environments supporting dbus-send (eg, X11). In the future once
# snappy's xdg-open supports all snaps images, this access may move to another
# interface. This is duplicated from desktop for compatibility with existing
# snaps.
/usr/bin/xdg-open ixr,
# While /usr/share/applications comes from the base runtime of the snap, it
# has some things that snaps actually need, so allow access to those and deny
# access to the others. This is duplicated from desktop for compatibility with
# existing snaps.
/usr/share/applications/ r,
/usr/share/applications/mimeapps.list r,
/usr/share/applications/xdg-open.desktop r,
# silence noisy denials from desktop files in core* snaps that aren't usable by
# snaps
deny /usr/share/applications/python*.desktop r,
deny /usr/share/applications/vim.desktop r,
deny /usr/share/applications/snap-handle-link.desktop r,  # core16

# This allow access to the first version of the snapd-xdg-open
# version which was shipped outside of snapd
dbus (send)
    bus=session
    path=/
    interface=com.canonical.SafeLauncher
    member=OpenURL
    peer=(label=unconfined),
# ... and this allows access to the new xdg-open service which
# is now part of snapd itself.
dbus (send)
    bus=session
    path=/io/snapcraft/Launcher
    interface=io.snapcraft.Launcher
    member={OpenURL,OpenFile}
    peer=(label=unconfined),

# Allow use of snapd's internal 'xdg-settings'
/usr/bin/xdg-settings ixr,
dbus (send)
    bus=session
    path=/io/snapcraft/Settings
    interface=io.snapcraft.Settings
    member={Check,CheckSub,Get,GetSub,Set,SetSub}
    peer=(label=unconfined),

# input methods (ibus)
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


# input methods (mozc)
# allow communicating with mozc server (TODO: investigate if allows sniffing)
unix (connect, receive, send)
     type=stream
     peer=(addr="@tmp/.mozc.*"),


# input methods (fcitx)
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

# Needed by QtSystems on X to detect mouse and keyboard. Note, the 'netlink
# raw' rule is not finely mediated by apparmor so we mediate with seccomp arg
# filtering.
network netlink raw,
/run/udev/data/c13:[0-9]* r,
/run/udev/data/+input:* r,

# subset of freedesktop.org
/usr/share/mime/**                   r,
owner @{HOME}/.local/share/mime/**   r,
owner @{HOME}/.config/user-dirs.* r,

/etc/xdg/user-dirs.conf r,
/etc/xdg/user-dirs.defaults r,

# gtk settings (subset of gnome abstraction)
owner @{HOME}/.config/gtk-2.0/gtkfilechooser.ini r,
owner @{HOME}/.config/gtk-3.0/settings.ini r,
# Note: this leaks directory names that wouldn't otherwise be known to the snap
owner @{HOME}/.config/gtk-3.0/bookmarks r,

# accessibility
#include <abstractions/dbus-accessibility-strict>
dbus (send)
    bus=session
    path=/org/a11y/bus
    interface=org.a11y.Bus
    member=GetAddress
    peer=(label=unconfined),
dbus (send)
    bus=session
    path=/org/a11y/bus
    interface=org.freedesktop.DBus.Properties
    member=Get{,All}
    peer=(label=unconfined),

# unfortunate, but org.a11y.atspi is not designed for separation
dbus (receive, send)
    bus=accessibility
    path=/org/a11y/atspi/**
    peer=(label=unconfined),

# org.freedesktop.Accounts
dbus (send)
    bus=system
    path=/org/freedesktop/Accounts
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=unconfined),

dbus (send)
    bus=system
    path=/org/freedesktop/Accounts
    interface=org.freedesktop.Accounts
    member=FindUserById
    peer=(label=unconfined),

# Get() is an information leak
# TODO: verify what it is leaking
dbus (receive, send)
    bus=system
    path=/org/freedesktop/Accounts/User[0-9]*
    interface=org.freedesktop.DBus.Properties
    member={Get,PropertiesChanged}
    peer=(label=unconfined),

# gmenu
# Note: the gmenu DBus api was not designed for application isolation and apps
# may specify anything as their 'path'. For example, these work in the many
# cases:
# - /org/gtk/Application/anonymous{,/**}
# - /com/canonical/unity/gtk/window/[0-9]*
# but libreoffice does:
# - /org/libreoffice{,/**}
# As such, cannot mediate by DBus path so we'll be as strict as we can in the
# other mediated parts
dbus (send)
    bus=session
    interface=org.gtk.Actions
    member=Changed
    peer=(label=unconfined),

dbus (receive)
    bus=session
    interface=org.gtk.Actions
    member={Activate,DescribeAll,SetState}
    peer=(label=unconfined),

dbus (receive)
    bus=session
    interface=org.gtk.Menus
    member={Start,End}
    peer=(label=unconfined),

dbus (send)
    bus=session
    interface=org.gtk.Menus
    member=Changed
    peer=(label=unconfined),

# Ubuntu menus
dbus (send)
    bus=session
    path="/com/ubuntu/MenuRegistrar"
    interface="com.ubuntu.MenuRegistrar"
    member="{Register,Unregister}{App,Surface}Menu"
    peer=(label=unconfined),

# url helper
dbus (send)
    bus=session
    interface=com.canonical.SafeLauncher.OpenURL
    peer=(label=unconfined),
# new url helper (part of snap userd)
dbus (send)
    bus=session
    interface=io.snapcraft.Launcher.OpenURL
    peer=(label=unconfined),

# dbusmenu
dbus (send)
    bus=session
    path=/{MenuBar{,/[0-9A-F]*},com/canonical/{menu/[0-9A-F]*,dbusmenu}}
    interface=com.canonical.dbusmenu
    member="{LayoutUpdated,ItemsPropertiesUpdated}"
    peer=(label="{plasmashell,unconfined}"),

dbus (receive)
    bus=session
    path=/{MenuBar{,/[0-9A-F]*},com/canonical/{menu/[0-9A-F]*,dbusmenu}}
    interface="{com.canonical.dbusmenu,org.freedesktop.DBus.Properties}"
    member=Get*
    peer=(label="{plasmashell,unconfined}"),

dbus (receive)
    bus=session
    path=/{MenuBar{,/[0-9A-F]*},com/canonical/{menu/[0-9A-F]*,dbusmenu}}
    interface=com.canonical.dbusmenu
    member="{AboutTo*,Event*}"
    peer=(label="{plasmashell,unconfined}"),

dbus (receive)
    bus=session
    path=/{MenuBar{,/[0-9A-F]*},com/canonical/{menu/[0-9A-F]*,dbusmenu}}
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label="{plasmashell,unconfined}"),

dbus (receive)
    bus=session
    path=/com/canonical/dbusmenu
    interface=org.freedesktop.DBus.Properties
    member=Get*
    peer=(label="{plasmashell,unconfined}"),

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
    peer=(label="{plasmashell,unconfined}"),

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
    peer=(label="{plasmashell,unconfined}"),

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
    peer=(label="{plasmashell,unconfined}"),

dbus (receive)
    bus=session
    path=/org/freedesktop/Notifications
    interface=org.freedesktop.Notifications
    member={ActionInvoked,NotificationClosed,NotificationReplied}
    peer=(label="{plasmashell,unconfined}"),

# KDE Plasma's Inhibited property indicating "do not disturb" mode
# https://invent.kde.org/plasma/plasma-workspace/-/blob/master/libnotificationmanager/dbus/org.freedesktop.Notifications.xml#L42
dbus (send)
    bus=session
    path=/org/freedesktop/Notifications
    interface=org.freedesktop.DBus.Properties
    member="Get{,All}"
    peer=(label="{plasmashell,unconfined}"),

dbus (receive)
    bus=session
    path=/org/freedesktop/Notifications
    interface=org.freedesktop.DBus.Properties
    member=PropertiesChanged
    peer=(label="{plasmashell,unconfined}"),

dbus (send)
    bus=session
    path=/org/ayatana/NotificationItem/*
    interface=org.kde.StatusNotifierItem
    member=XAyatanaNew*
    peer=(label="{plasmashell,unconfined}"),

# unity launcher
dbus (send)
    bus=session
    path=/com/canonical/unity/launcherentry/[0-9]*
    interface=com.canonical.Unity.LauncherEntry
    member=Update
    peer=(label=unconfined),

dbus (send)
    bus=session
    path=/com/canonical/unity/launcherentry/[0-9]*
    interface=com.canonical.dbusmenu
    member="{LayoutUpdated,ItemsPropertiesUpdated}"
    peer=(label=unconfined),

dbus (receive)
    bus=session
    path=/com/canonical/unity/launcherentry/[0-9]*
    interface="{com.canonical.dbusmenu,org.freedesktop.DBus.Properties}"
    member=Get*
    peer=(label=unconfined),

###SNAP_DESKTOP_FILE_RULES###
# Snaps are unable to use the data in mimeinfo.cache (since they can't execute
# the returned desktop file themselves). unity messaging menu doesn't require
# mimeinfo.cache and xdg-mime will fallback to reading the desktop files
# directly to look for MimeType. Since reading the snap's own desktop files is
# allowed, we can safely deny access to this file (and xdg-mime will either
# return one of the snap's mimetypes, or none).
deny /var/lib/snapd/desktop/applications/mimeinfo.cache r,

# then allow talking to Unity DBus service
dbus (send)
    bus=session
    interface=org.freedesktop.DBus.Properties
    path=/com/canonical/indicator/messages/service
    member=GetAll
    peer=(label=unconfined),

dbus (send)
    bus=session
    path=/com/canonical/indicator/messages/service
    interface=com.canonical.indicator.messages.service
    member={Register,Unregister}Application
    peer=(label=unconfined),

# When @{SNAP_NAME} == @{SNAP_INSTANCE_NAME}, this rule
# allows the snap to access parallel installs of this snap.
dbus (receive)
    bus=session
    interface=org.freedesktop.DBus.Properties
    path=/com/canonical/indicator/messages/###UNITY_SNAP_NAME###_*_desktop
    member=GetAll
    peer=(label=unconfined),

# When @{SNAP_NAME} == @{SNAP_INSTANCE_NAME}, this rule
# allows the snap to access parallel installs of this snap.
dbus (receive, send)
    bus=session
    interface=com.canonical.indicator.messages.application
    path=/com/canonical/indicator/messages/###UNITY_SNAP_NAME###_*_desktop
    peer=(label=unconfined),

# This rule is meant to be covered by abstractions/dbus-session-strict but
# the unity launcher code has a typo that uses /org/freedesktop/dbus as the
# path instead of /org/freedesktop/DBus, so we need to all it here.
dbus (send)
    bus=session
    path=/org/freedesktop/dbus
    interface=org.freedesktop.DBus
    member=NameHasOwner
    peer=(name=org.freedesktop.DBus, label=unconfined),

# appmenu
dbus (send)
    bus=session
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member=ListNames
    peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (send)
    bus=session
    path=/com/canonical/AppMenu/Registrar
    interface=com.canonical.AppMenu.Registrar
    member="{RegisterWindow,UnregisterWindow}"
    peer=(label=unconfined),

dbus (send)
    bus=session
    path=/com/canonical/AppMenu/Registrar
    interface=com.canonical.dbusmenu
    member=UnregisterWindow
    peer=(label=unconfined),

dbus (receive)
    bus=session
    path=/com/canonical/menu/[0-9]*
    interface="{org.freedesktop.DBus.Properties,com.canonical.dbusmenu}"
    member="{GetAll,GetLayout}"
    peer=(label="{plasmashell,unconfined}"),

# Allow requesting interest in receiving media key events. This tells Gnome
# settings that our application should be notified when key events we are
# interested in are pressed, and allows us to receive those events.
dbus (receive, send)
  bus=session
  interface=org.gnome.SettingsDaemon.MediaKeys
  path=/org/gnome/SettingsDaemon/MediaKeys
  peer=(label=unconfined),
dbus (send)
  bus=session
  interface=org.freedesktop.DBus.Properties
  path=/org/gnome/SettingsDaemon/MediaKeys
  member="Get{,All}"
  peer=(label=unconfined),

# Allow checking status, activating and locking the screensaver
# mate
dbus (send)
    bus=session
    path="/{,org/mate/}ScreenSaver"
    interface=org.mate.ScreenSaver
    member="{GetActive,GetActiveTime,Lock,SetActive}"
    peer=(label=unconfined),

dbus (receive)
    bus=session
    path="/{,org/mate/}ScreenSaver"
    interface=org.mate.ScreenSaver
    member=ActiveChanged
    peer=(label=unconfined),

# Unity
dbus (send)
  bus=session
  interface=com.canonical.Unity.Session
  path=/com/canonical/Unity/Session
  member="{ActivateScreenSaver,IsLocked,Lock}"
  peer=(label=unconfined),

# Allow unconfined to introspect us
dbus (receive)
    bus=session
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
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
`

const unity7ConnectedPlugSeccomp = `
# Description: Can access Unity7. Note, Unity 7 runs on X and requires access
# to various DBus services and this environment does not prevent eavesdropping
# or apps interfering with one another.

# Needed by QtSystems on X to detect mouse and keyboard
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
bind
`

type unity7Interface struct{}

func (iface *unity7Interface) Name() string {
	return "unity7"
}

func (iface *unity7Interface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              unity7Summary,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: unity7BaseDeclarationSlots,
	}
}

func (iface *unity7Interface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// Unity7 will take the desktop filename and convert all '-' and '+'
	// (and '.', but we don't care about that here because the rule above
	// already does that) to '_'. Since we know that the desktop filename
	// starts with the snap name, perform this conversion on the snap name.
	//
	// parallel-installs: UNITY_SNAP_NAME is used in the context of dbus
	// mediation, this unintentionally opens access to dbus paths of keyed
	// instances of @{SNAP_NAME} to @{SNAP_NAME} snap
	new := strings.Replace(plug.Snap().DesktopPrefix(), "-", "_", -1)
	new = strings.Replace(new, "+", "_", -1)
	old := "###UNITY_SNAP_NAME###"
	snippet := strings.Replace(unity7ConnectedPlugAppArmor, old, new, -1)

	old = "###SNAP_DESKTOP_FILE_RULES###"
	new = strings.Join(getDesktopFileRules(plug.Snap().DesktopPrefix()), "\n")
	snippet = strings.Replace(snippet, old, new+"\n", -1)

	spec.AddSnippet(snippet)
	return nil
}

func (iface *unity7Interface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(unity7ConnectedPlugSeccomp)
	return nil
}

func (iface *unity7Interface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&unity7Interface{})
}
