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
	"fmt"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

const desktopSummary = `allows access to basic graphical desktop resources`

// The weird allow-installation/deny-installation construct is
// intended to prevent app snaps from the store that provide this slot
// from installing without an override, while allowing an unpublished
// snap to still be installed.
const desktopBaseDeclarationSlots = `
  desktop:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-installation:
      slot-snap-type:
        - app
`

const desktopBaseDeclarationPlugs = `
  desktop:
    allow-installation:
      plug-attributes:
        desktop-file-prefix: $MISSING
    deny-auto-connection:
      slot-snap-type:
        - app
    deny-connection:
      slot-snap-type:
        - app
`

const desktopConnectedPlugAppArmor = `
# Description: Can access basic graphical desktop resources. To be used with
# other interfaces (eg, wayland).

#include <abstractions/dbus-strict>
#include <abstractions/dbus-session-strict>

# Allow finding the DBus session bus id (eg, via dbus_bus_get_id())
dbus (send)
     bus=session
     path=/org/freedesktop/DBus
     interface=org.freedesktop.DBus
     member=GetId
     peer=(name=org.freedesktop.DBus, label=unconfined),

#include <abstractions/fonts>
owner @{HOME}/.local/share/fonts/{,**} r,
/var/cache/fontconfig/   r,
/var/cache/fontconfig/** mr,
# some applications are known to mmap fonts
/usr/{,local/}share/fonts/** m,

# Allow access to xdg-document-portal file system.  Access control is
# handled by bind mounting a snap-specific sub-tree to this location
# (ie, this is /run/user/<uid>/doc/by-app/snap.@{SNAP_INSTANCE_NAME}
# on the host).
owner /run/user/[0-9]*/doc/{,*/} r,
# Allow rw access without owner match to the documents themselves since
# the user guided the access and can specify anything DAC allows.
/run/user/[0-9]*/doc/*/** rw,

# Allow access to xdg-desktop-portal and xdg-document-portal
dbus (receive, send)
    bus=session
    interface=org.freedesktop.portal.*
    path=/org/freedesktop/portal/{desktop,documents}{,/**}
    peer=(label=unconfined),

dbus (receive, send)
    bus=session
    interface=org.freedesktop.DBus.Properties
    path=/org/freedesktop/portal/{desktop,documents}{,/**}
    peer=(label=unconfined),

# The portals service is normally running and newer versions of
# xdg-desktop-portal include AssumedAppArmor=unconfined. Since older
# systems don't have this and because gtkfilechoosernativeportal.c relies on
# service activation, allow sends to peer=(name=org.freedesktop.portal.{Desktop,Documents})
# for service activation.
dbus (send)
    bus=session
    interface=org.freedesktop.portal.*
    path=/org/freedesktop/portal/desktop{,/**}
    peer=(name=org.freedesktop.portal.Desktop),
dbus (send)
    bus=session
    interface=org.freedesktop.DBus.Properties
    path=/org/freedesktop/portal/desktop{,/**}
    peer=(name=org.freedesktop.portal.Desktop),
dbus (send)
    bus=session
    interface=org.freedesktop.portal.*
    path=/org/freedesktop/portal/documents{,/**}
    peer=(name=org.freedesktop.portal.Documents),
dbus (send)
    bus=session
    interface=org.freedesktop.DBus.Properties
    path=/org/freedesktop/portal/documents{,/**}
    peer=(name=org.freedesktop.portal.Documents),

# Allow to get the current idle time only from Mutter
dbus (send)
    bus=session
    path="/org/gnome/Mutter/IdleMonitor/Core"
    interface="org.gnome.Mutter.IdleMonitor"
    member="GetIdletime"
    peer=(label=###SLOT_SECURITY_TAGS###),
`

const desktopConnectedPlugAppArmorClassic = `
# subset of gnome abstraction
/etc/gtk-3.0/settings.ini r,
owner @{HOME}/.config/gtk-3.0/settings.ini r,
owner @{HOME}/.config/gtk-3.0/*.css r,
# Note: this leaks directory names that wouldn't otherwise be known to the snap
owner @{HOME}/.config/gtk-3.0/bookmarks r,

/usr/share/icons/                          r,
/usr/share/icons/**                        r,
/usr/share/icons/*/index.theme             rk,
/usr/share/pixmaps/                        r,
/usr/share/pixmaps/**                      r,
/usr/share/unity/icons/**                  r,
/usr/share/thumbnailer/icons/**            r,
/usr/share/themes/**                       r,

# The snapcraft desktop part may look for schema files in various locations, so
# allow reading system installed schemas.
/usr/share/glib*/schemas/{,*}              r,
/usr/share/gnome/glib*/schemas/{,*}        r,
/usr/share/ubuntu/glib*/schemas/{,*}       r,

# subset of freedesktop.org
owner @{HOME}/.local/share/mime/**   r,
owner @{HOME}/.config/user-dirs.* r,

/etc/xdg/user-dirs.conf r,
/etc/xdg/user-dirs.defaults r,

# gmenu
dbus (send)
     bus=session
     interface=org.gtk.Actions
     member=Changed
     peer=(label=unconfined),

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

# DesktopAppInfo Launched
dbus (send)
    bus=session
    path=/org/gtk/gio/DesktopAppInfo
    interface=org.gtk.gio.DesktopAppInfo
    member=Launched
    peer=(label=unconfined),

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

# Allow accessing the GNOME crypto services prompt APIs as used by
# applications using libgcr (such as pinentry-gnome3) for secure pin
# entry to unlock GPG keys etc. See:
# https://developer.gnome.org/gcr/unstable/GcrPrompt.html
# https://developer.gnome.org/gcr/unstable/GcrSecretExchange.html
dbus (send)
    bus=session
    path=/org/gnome/keyring/Prompter
    interface=org.gnome.keyring.internal.Prompter
    member="{BeginPrompting,PerformPrompt,StopPrompting}"
    peer=(label=unconfined),

# While the DBus path is not snap-specific, by the time an application
# registers the prompt path via DBus, Gcr will check that it isn't
# already in use and send the client an error if it is. See:
# https://github.com/snapcore/snapd/pull/7673#issuecomment-592229711
dbus (receive)
    bus=session
    path=/org/gnome/keyring/Prompt/p[0-9]*
    interface=org.gnome.keyring.internal.Prompter.Callback
    member="{PromptReady,PromptDone}"
    peer=(label=unconfined),

# Allow use of snapd's internal 'xdg-open'
/usr/bin/xdg-open ixr,
# While /usr/share/applications comes from the base runtime of the snap, it
# has some things that snaps actually need, so allow access to those and deny
# access to the others
/usr/share/applications/ r,
/usr/share/applications/mimeapps.list r,
/usr/share/applications/xdg-open.desktop r,
# silence noisy denials from desktop files in core* snaps that aren't usable by
# snaps
deny /usr/share/applications/python*.desktop r,
deny /usr/share/applications/vim.desktop r,
deny /usr/share/applications/snap-handle-link.desktop r,  # core16

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

# Allow checking status, activating and locking the screensaver
# gnome/kde/freedesktop.org
dbus (send)
    bus=session
    path="/{,org/freedesktop/,org/gnome/}ScreenSaver"
    interface="org.{freedesktop,gnome}.ScreenSaver"
    member="{GetActive,GetActiveTime,Lock,SetActive}"
    peer=(label=unconfined),

dbus (receive)
    bus=session
    path="/{,org/freedesktop/,org/gnome/}ScreenSaver"
    interface="org.{freedesktop,gnome}.ScreenSaver"
    member=ActiveChanged
    peer=(label=unconfined),

# Allow unconfined to introspect us
dbus (receive)
    bus=session
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=unconfined),

# Allow use of snapd's internal 'xdg-settings'
/usr/bin/xdg-settings ixr,
dbus (send)
    bus=session
    path=/io/snapcraft/Settings
    interface=io.snapcraft.Settings
    member={Check,CheckSub,Get,GetSub,Set,SetSub}
    peer=(label=unconfined),

# These accesses are noisy and applications can't do anything with the found
# icon files, so explicitly deny to silence the denials
deny /var/lib/snapd/desktop/icons/{,**/} r,

# These accesses occur when flatpaks are on the system since it updates
# XDG_DATA_DIRS to contain $HOME/.local/share/flatpak/exports/share. Until
# we have better XDG_DATA_DIRS handling, silence these noisy denials.
# https://github.com/snapcrafters/discord/issues/23#issuecomment-637607843
deny @{HOME}/.local/share/flatpak/exports/share/** r,

# Allow access to the IBus portal (IBUS_USE_PORTAL=1)
dbus (send)
      bus=session
      path=/org/freedesktop/IBus
      interface=org.freedesktop.IBus.Portal
      member=CreateInputContext
      peer=(name=org.freedesktop.portal.IBus),

dbus (send, receive)
      bus=session
      path=/org/freedesktop/IBus/InputContext_[0-9]*
      interface=org.freedesktop.IBus.InputContext
      peer=(label=unconfined),

# Allow access to the Fcitx portal, supported by fcitx/fcitx5
dbus (send)
      bus=session
      path=/{,org/freedesktop/portal/}inputmethod
      interface=org.fcitx.Fcitx.InputMethod1
      member={CreateInputContext,Version}
      peer=(name=org.freedesktop.portal.Fcitx),

dbus (send, receive)
      bus=session
      path=/{,org/freedesktop/portal/}inputcontext/**
      interface=org.fcitx.Fcitx.InputContext1
      peer=(label=unconfined),
`

const desktopPermanentSlotAppArmor = `
# Description: Can provide various desktop services

#include <abstractions/dbus-session-strict>

# Able to provide notifications
dbus (receive)
    bus=session
    path=/org/freedesktop/Notifications
    interface=org.freedesktop.Notifications
    member="{GetCapabilities,GetServerInformation,Notify,CloseNotification}"
    peer=(label=unconfined),

dbus (send)
    bus=session
    path=/org/freedesktop/Notifications
    interface=org.freedesktop.Notifications
    member={ActionInvoked,NotificationClosed,NotificationReplied}
    peer=(label=unconfined),

# Able to provide GTK notifications
dbus (receive)
    bus=session
    path=/org/gtk/Notifications
    interface=org.gtk.Notifications
    member="{AddNotification,RemoveNotification}"
    peer=(label=unconfined),

# Allow registering session with GDM, necessary for screen locking
dbus (send)
    bus=system
    path=/org/gnome/DisplayManager/Manager
    interface=org.gnome.DisplayManager.Manager
    member={RegisterSession,OpenReauthenticationChannel}
    peer=(label=unconfined),
dbus (send)
    bus=system
    path=/org/gnome/DisplayManager/Manager
    interface=org.freedesktop.DBus.Properties
    member="Get{,All}"
    peer=(label=unconfined),

# Allow access to GDM's private reauthentication channel socket
/run/gdm3/dbus/dbus-* rw,

# Allow gnome-shell to bind to its various D-Bus names
dbus (bind)
    bus=session
    name=org.gnome.Mutter.*,
dbus (bind)
    bus=session
    name=org.gnome.Shell{,.*},

# Allow gnome-settings-daemon to bind its various D-Bus names
dbus (bind)
    bus=session
    name=org.gnome.SettingsDaemon{,.*},
dbus (bind)
    bus=session
    name=org.gtk.Settings,

# Allow the shell to communicate with colord
dbus (send, receive)
    bus=system
    path=/org/freedesktop/ColorManager{,/**}
    interface=org.freedesktop.ColorManager*
    peer=(label=unconfined),
dbus (send, receive)
    bus=system
    path=/org/freedesktop/ColorManager{,/**}
    interface=org.freedesktop.DBus.Properties
    member={Get,GetAll,PropertiesChanged}
    peer=(label=unconfined),

# Allow unconfined xdg-desktop-portal to communicate with impl
# services provided by the snap.
dbus (receive, send)
    bus=session
    path=/org/freedesktop/portal/desktop{,/**}
    interface=org.freedesktop.impl.portal.*
    peer=(label=unconfined),
dbus (receive, send)
    bus=session
    path=/org/freedesktop/portal/desktop{,/**}
    interface=org.freedesktop.DBus.Properties
    peer=(label=unconfined),

# Allow access to the regular xdg-desktop-portal APIs
dbus (send)
    bus=session
    interface=org.freedesktop.portal.*
    path=/org/freedesktop/portal/desktop{,/**}
    peer=(label=unconfined),

# Allow access to various paths gnome-session and gnome-shell need.
/etc/fonts{,/**} r,
/etc/glvnd{,/**} r,
/etc/gnome/defaults.list r,
/etc/gtk-3.0{,/**} r,
/etc/shells r,
/etc/xdg/autostart{,/**} r,
/etc/xdg/user-dirs.conf r,
/etc/xdg/user-dirs.defaults r,
/run/udev/tags/seat{,/**} r,
`

type desktopInterface struct {
	commonInterface
}

func (iface *desktopInterface) shouldMountHostFontCache(attribs interfaces.Attrer) (bool, error) {
	value, ok := attribs.Lookup("mount-host-font-cache")
	if !ok {
		// If the attribute is not present, we mount the font cache
		return true, nil
	}
	shouldMount, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("desktop plug requires bool with 'mount-host-font-cache'")
	}
	return shouldMount, nil
}

func (iface *desktopInterface) fontconfigDirs(plug *interfaces.ConnectedPlug) ([]string, error) {
	fontDirs := []string{
		dirs.SystemFontsDir,
		dirs.SystemLocalFontsDir,
	}

	shouldMountHostFontCache, err := iface.shouldMountHostFontCache(plug)
	if err != nil {
		return nil, err
	}
	if shouldMountHostFontCache {
		fontDirs = append(fontDirs, dirs.SystemFontconfigCacheDirs...)
	}

	return fontDirs, nil
}

func (iface *desktopInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	var new string
	if implicitSystemConnectedSlot(slot) {
		// we are running on a system that has the desktop slot
		// provided by the OS snap and so will run unconfined
		new = "unconfined"
	} else {
		new = spec.SnapAppSet().SlotLabelExpression(slot)
	}
	snippet := strings.Replace(desktopConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	if implicitSystemConnectedSlot(slot) {
		// Extra rules that have not been ported to work with
		// a desktop slot provided by a snap.
		spec.AddSnippet(desktopConnectedPlugAppArmorClassic)
	}

	// Allow mounting document portal
	emit := spec.AddUpdateNSf
	emit("  # Mount the document portal\n")
	emit("  mount options=(bind) /run/user/[0-9]*/doc/by-app/snap.%s/ -> /run/user/[0-9]*/doc/,\n", plug.Snap().InstanceName())
	emit("  umount /run/user/[0-9]*/doc/,\n\n")

	// Allow mounting fonts. For the app-provided slot case, we
	// assume that the slot snap is using the boot base snap as
	// its base, and that base contains fonts.
	fontDirs, err := iface.fontconfigDirs(plug)
	if err != nil {
		return err
	}
	for _, dir := range fontDirs {
		source := "/var/lib/snapd/hostfs" + dir
		target := dirs.StripRootDir(dir)
		emit("  # Read-only access to %s\n", target)
		emit("  mount options=(bind) %s/ -> %s/,\n", source, target)
		emit("  remount options=(bind, ro) %s/,\n", target)
		emit("  umount %s/,\n\n", target)
	}

	return nil
}

func (iface *desktopInterface) MountConnectedPlug(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	appId := "snap." + plug.Snap().InstanceName()
	spec.AddUserMountEntry(osutil.MountEntry{
		Name:    "$XDG_RUNTIME_DIR/doc/by-app/" + appId,
		Dir:     "$XDG_RUNTIME_DIR/doc",
		Options: []string{"bind", "rw", osutil.XSnapdIgnoreMissing()},
	})

	fontDirs, err := iface.fontconfigDirs(plug)
	if err != nil {
		return err
	}
	for _, dir := range fontDirs {
		if !osutil.IsDirectory(dir) {
			continue
		}
		if release.DistroLike("arch", "fedora") {
			// XXX: on Arch and Fedora 32+ there is a known
			// incompatibility between the binary fonts cache files
			// and ones expected by desktop snaps; even though the
			// cache format level is same for both, the host
			// generated cache files cause instability, segfaults or
			// incorrect rendering of fonts, for this reason do not
			// mount the cache directories on those distributions,
			// see https://bugs.launchpad.net/snapd/+bug/1877109
			if strutil.ListContains(dirs.SystemFontconfigCacheDirs, dir) {
				continue
			}
		}
		// Since /etc/fonts/fonts.conf in the snap mount ns is the same
		// as on the host, we need to preserve the original directory
		// paths for the fontconfig runtime to poke the correct
		// locations
		spec.AddMountEntry(osutil.MountEntry{
			Name:    "/var/lib/snapd/hostfs" + dir,
			Dir:     dirs.StripRootDir(dir),
			Options: []string{"bind", "ro"},
		})
	}

	return nil
}

func (iface *desktopInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	if !implicitSystemPermanentSlot(slot) {
		spec.AddSnippet(desktopPermanentSlotAppArmor)
	}
	return nil
}

func (iface *desktopInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	_, err := iface.shouldMountHostFontCache(plug)
	return err
}

func init() {
	registerIface(&desktopInterface{
		commonInterface: commonInterface{
			name:                 "desktop",
			summary:              desktopSummary,
			implicitOnClassic:    true,
			baseDeclarationSlots: desktopBaseDeclarationSlots,
			baseDeclarationPlugs: desktopBaseDeclarationPlugs,
			// affects the plug snap because of mount backend
			affectsPlugOnRefresh: true,
		},
	})
}
