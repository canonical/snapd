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
	"bytes"
	"fmt"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

const desktopSummary = `allows access to basic graphical desktop resources`

const desktopBaseDeclarationSlots = `
  desktop:
    allow-installation:
      slot-snap-type:
        - core
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

# subset of gnome abstraction
/etc/gtk-3.0/settings.ini r,
owner @{HOME}/.config/gtk-3.0/settings.ini r,
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
     peer=(name=org.freedesktop.DBus, label=unconfined),

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
    member={ActionInvoked,NotificationClosed}
    peer=(label=unconfined),

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

# Allow use of snapd's internal 'xdg-open'
/usr/bin/xdg-open ixr,
/usr/share/applications/{,*} r,
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
    member={Check,Get,Set}
    peer=(label=unconfined),

## Allow access to xdg-document-portal file system.  Access control is
## handled by bind mounting a snap-specific sub-tree to this location.
owner /run/user/[0-9]*/doc/ r,
owner /run/user/[0-9]*/doc/** rw,

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
`

type desktopInterface struct{}

func (iface *desktopInterface) Name() string {
	return "desktop"
}

func (iface *desktopInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              desktopSummary,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: desktopBaseDeclarationSlots,
	}
}

func (iface *desktopInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	return sanitizeSlotReservedForOS(iface, slot)
}

func (iface *desktopInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func (iface *desktopInterface) fontconfigDirs() []string {
	return []string{
		dirs.SystemFontsDir,
		dirs.SystemLocalFontsDir,
		dirs.SystemFontconfigCacheDir,
	}
}

func (iface *desktopInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(desktopConnectedPlugAppArmor)

	// Allow mounting document portal
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "  # Mount the document portal\n")
	// TODO parallel-install: use of proper instance/store name
	fmt.Fprintf(&buf, "  mount options=(bind) /run/user/[0-9]*/doc/by-app/snap.%s/ -> /run/user/[0-9]*/doc/,\n", plug.Snap().InstanceName())
	fmt.Fprintf(&buf, "  umount /run/user/[0-9]*/doc/,\n\n")
	spec.AddUpdateNS(buf.String())

	if !release.OnClassic {
		// We only need the font mount rules on classic systems
		return nil
	}

	// Allow mounting fonts
	for _, dir := range iface.fontconfigDirs() {
		var buf bytes.Buffer
		source := "/var/lib/snapd/hostfs" + dir
		target := dirs.StripRootDir(dir)
		fmt.Fprintf(&buf, "  # Read-only access to %s\n", target)
		fmt.Fprintf(&buf, "  mount options=(bind) %s/ -> %s/,\n", source, target)
		fmt.Fprintf(&buf, "  remount options=(bind, ro) %s/,\n", target)
		fmt.Fprintf(&buf, "  umount %s/,\n\n", target)
		spec.AddUpdateNS(buf.String())
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

	if !release.OnClassic {
		// We only need the font mount rules on classic systems
		return nil
	}

	for _, dir := range iface.fontconfigDirs() {
		if !osutil.IsDirectory(dir) {
			continue
		}
		spec.AddMountEntry(osutil.MountEntry{
			Name:    "/var/lib/snapd/hostfs" + dir,
			Dir:     dirs.StripRootDir(dir),
			Options: []string{"bind", "ro"},
		})
	}

	return nil
}

func init() {
	registerIface(&desktopInterface{})
}
