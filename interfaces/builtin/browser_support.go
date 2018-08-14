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

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
)

const browserSupportSummary = `allows access to various APIs needed by modern web browsers`

const browserSupportBaseDeclarationSlots = `
  browser-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-connection:
      plug-attributes:
        allow-sandbox: true
    deny-auto-connection:
      plug-attributes:
        allow-sandbox: true
`

const browserSupportConnectedPlugAppArmor = `
# Description: Can access various APIs needed by modern browsers (eg, Google
# Chrome/Chromium and Mozilla) and file paths they expect. This interface is
# transitional and is only in place while upstream's work to change their paths
# and snappy is updated to properly mediate the APIs.

# This allows raising the OOM score of other processes owned by the user.
owner @{PROC}/@{pid}/oom_score_adj rw,

# Chrome/Chromium should be fixed to honor TMPDIR or the snap packaging
# adjusted to use LD_PRELOAD technique from LP: #1577514
/var/tmp/ r,
owner /var/tmp/etilqs_* rw,

# Chrome/Chromium should be modified to use snap.$SNAP_NAME.* or the snap
# packaging adjusted to use LD_PRELOAD technique from LP: #1577514
owner /{dev,run}/shm/{,.}org.chromium.* mrw,
owner /{dev,run}/shm/{,.}com.google.Chrome.* mrw,
owner /{dev,run}/shm/.io.nwjs.* mrw,

# Chrome's Singleton API sometimes causes an ouid/fsuid mismatch denial, so
# for now, allow non-owner read on the singleton socket (LP: #1731012). See
# https://forum.snapcraft.io/t/electron-snap-killed-when-using-app-makesingleinstance-api/2667/20
/run/user/[0-9]*/snap.@{SNAP_NAME}/{,.}org.chromium.*/SS r,
/run/user/[0-9]*/snap.@{SNAP_NAME}/{,.}com.google.Chrome.*/SS r,

# Allow reading platform files
/run/udev/data/+platform:* r,

# miscellaneous accesses
@{PROC}/vmstat r,

# Chromium content api in gnome-shell reads this
/etc/opt/chrome/{,**} r,
/etc/chromium/{,**} r,

# Chrome/Chromium should be adjusted to not use gconf. It is only used with
# legacy systems that don't have snapd
deny dbus (send)
    bus=session
    interface="org.gnome.GConf.Server",

# webbrowser-app/webapp-container tries to read this file to determine if it is
# confined or not, so explicitly deny to avoid noise in the logs.
deny @{PROC}/@{pid}/attr/current r,

# This is an information leak but disallowing it leads to developer confusion
# when using the chromium content api file chooser due to a (harmless) glib
# warning and the noisy AppArmor denial.
owner @{PROC}/@{pid}/mounts r,
owner @{PROC}/@{pid}/mountinfo r,

# Since snapd still uses SECCOMP_RET_KILL, we have added a workaround rule to
# allow mknod on character devices since chromium unconditionally performs
# a mknod() to create the /dev/nvidiactl device, regardless of if it exists or
# not or if the process has CAP_MKNOD or not. Since we don't want to actually
# grant the ability to create character devices, explicitly deny the
# capability. When snapd uses SECCOMP_RET_ERRNO, we can remove this rule.
# https://forum.snapcraft.io/t/call-for-testing-chromium-62-0-3202-62/2569/46
deny capability mknod,
`

const browserSupportConnectedPlugAppArmorWithoutSandbox = `
# ptrace can be used to break out of the seccomp sandbox, but ps requests
# 'ptrace (trace)' even though it isn't tracing other processes. Unfortunately,
# this is due to the kernel overloading trace such that the LSMs are unable to
# distinguish between tracing other processes and other accesses. We deny the
# trace here to silence the log.
# Note: for now, explicitly deny to avoid confusion and accidentally giving
# away this dangerous access frivolously. We may conditionally deny this in the
# future. If the kernel has https://lkml.org/lkml/2016/5/26/354 we could also
# allow this.
deny ptrace (trace) peer=snap.@{SNAP_NAME}.**,
`

const browserSupportConnectedPlugAppArmorWithSandbox = `
# Leaks installed applications
# TODO: should this be somewhere else?
/etc/mailcap r,
/usr/share/applications/{,*} r,
/var/lib/snapd/desktop/applications/{,*} r,
owner @{PROC}/@{pid}/fd/[0-9]* w,

# Various files in /run/udev/data needed by Chrome Settings. Leaks device
# information.
# input
/run/udev/data/c1:[0-9]* r,   # /dev/psaux
/run/udev/data/c10:[0-9]* r,  # /dev/adbmouse
/run/udev/data/c13:[0-9]* r,  # /dev/input/*
/run/udev/data/c180:[0-9]* r, # /dev/vrbuttons
/run/udev/data/c4:[0-9]* r,   # /dev/tty*, /dev/ttyS*
/run/udev/data/c5:[0-9]* r,   # /dev/tty, /dev/console, etc
/run/udev/data/c7:[0-9]* r,   # /dev/vcs*
/run/udev/data/+hid:* r,
/run/udev/data/+input:input[0-9]* r,

# screen
/run/udev/data/c29:[0-9]* r,  # /dev/fb*
/run/udev/data/+backlight:* r,
/run/udev/data/+leds:* r,

# sound
/run/udev/data/c116:[0-9]* r, # alsa
/run/udev/data/+sound:card[0-9]* r,

# miscellaneous
/run/udev/data/c108:[0-9]* r, # /dev/ppp
/run/udev/data/c189:[0-9]* r, # USB serial converters
/run/udev/data/c89:[0-9]* r,  # /dev/i2c-*
/run/udev/data/c81:[0-9]* r,  # video4linux (/dev/video*, etc)
/run/udev/data/c202:[0-9]* r, # /dev/cpu/*/msr
/run/udev/data/c203:[0-9]* r, # /dev/cuse
/run/udev/data/+acpi:* r,
/run/udev/data/+hwmon:hwmon[0-9]* r,
/run/udev/data/+i2c:* r,
/sys/devices/**/bConfigurationValue r,
/sys/devices/**/descriptors r,
/sys/devices/**/manufacturer r,
/sys/devices/**/product r,
/sys/devices/**/revision r,
/sys/devices/**/serial r,
/sys/devices/**/vendor r,
/sys/devices/system/node/node[0-9]*/meminfo r,

# Chromium content api tries to read these. It is an information disclosure
# since these contain the names of snaps. Chromium operates fine without the
# access so just block it.
deny /sys/devices/virtual/block/loop[0-9]*/loop/backing_file r,
deny /sys/devices/virtual/block/dm-[0-9]*/dm/name r,

# networking
/run/udev/data/n[0-9]* r,
/run/udev/data/+bluetooth:hci[0-9]* r,
/run/udev/data/+rfkill:rfkill[0-9]* r,
/run/udev/data/c241:[0-9]* r, # /dev/vhost-vsock

# storage
/run/udev/data/b1:[0-9]* r,   # /dev/ram*
/run/udev/data/b7:[0-9]* r,   # /dev/loop*
/run/udev/data/b8:[0-9]* r,   # /dev/sd*
/run/udev/data/b11:[0-9]* r,  # /dev/scd* and sr*
/run/udev/data/c21:[0-9]* r,  # /dev/sg*
/run/udev/data/+usb:[0-9]* r,

# experimental
/run/udev/data/b252:[0-9]* r,
/run/udev/data/b253:[0-9]* r,
/run/udev/data/b259:[0-9]* r,
/run/udev/data/c24[0-9]:[0-9]* r,
/run/udev/data/c25[0-4]:[0-9]* r,

/sys/bus/**/devices/ r,

# Google Cloud Print
unix (bind)
     type=stream
     addr="@[0-9A-F]*._service_*",

# Policy needed only when using the chrome/chromium setuid sandbox
capability sys_ptrace,
ptrace (trace) peer=snap.@{SNAP_NAME}.**,
unix (receive, send) peer=(label=snap.@{SNAP_NAME}.**),

# If this were going to be allowed to all snaps, then for all the following
# rules we would want to wrap in a 'browser_sandbox' profile, but a limitation
# in AppArmor profile transitions prevents this.
#
# @{INSTALL_DIR}/@{SNAP_NAME}/@{SNAP_REVISION}/opt/google/chrome{,-beta,-unstable}/chrome-sandbox cx -> browser_sandbox,
# profile browser_sandbox {
#   ...
#   # This rule needs to work but generates a parser error
#   @{INSTALL_DIR}/@{SNAP_NAME}/@{SNAP_REVISION}/opt/google/chrome/chrome px -> snap.@{SNAP_NAME}.@{SNAP_APP},
#   ...
# }

# Required for dropping into PID namespace. Keep in mind that until the
# process drops this capability it can escape confinement, but once it
# drops CAP_SYS_ADMIN we are ok.
capability sys_admin,

# All of these are for sanely dropping from root and chrooting
capability chown,
capability fsetid,
capability setgid,
capability setuid,
capability sys_chroot,

# User namespace sandbox
owner @{PROC}/@{pid}/setgroups rw,
owner @{PROC}/@{pid}/uid_map rw,
owner @{PROC}/@{pid}/gid_map rw,

# Webkit uses a particular SHM names # LP: 1578217
owner /{dev,run}/shm/WK2SharedMemory.* mrw,

# Chromium content api on (at least) later versions of Ubuntu just use this
owner /{dev,run}/shm/shmfd-* mrw,
`

const browserSupportConnectedPlugSecComp = `
# Description: Can access various APIs needed by modern browsers (eg, Google
# Chrome/Chromium and Mozilla) and file paths they expect. This interface is
# transitional and is only in place while upstream's work to change their paths
# and snappy is updated to properly mediate the APIs.

# for anonymous sockets
bind
listen
accept
accept4

# TODO: fine-tune when seccomp arg filtering available in stable distro
# releases
setpriority

# Since snapd still uses SECCOMP_RET_KILL, add a workaround rule to allow mknod
# on character devices since chromium unconditionally performs a mknod() to
# create the /dev/nvidiactl device, regardless of if it exists or not or if the
# process has CAP_MKNOD or not. Since we don't want to actually grant the
# ability to create character devices, we added an explicit deny AppArmor rule
# for this capability. When snapd uses SECCOMP_RET_ERRNO, we can remove this
# rule.
# https://forum.snapcraft.io/t/call-for-testing-chromium-62-0-3202-62/2569/46
mknod - |S_IFCHR -
mknodat - - |S_IFCHR -
`

const browserSupportConnectedPlugSecCompWithSandbox = `
# Policy needed only when using the chrome/chromium setuid sandbox
chroot
sched_setscheduler

# TODO: fine-tune when seccomp arg filtering available in stable distro
# releases
setuid
setgid

# Policy needed for Mozilla userns sandbox
unshare
quotactl
`

type browserSupportInterface struct{}

func (iface *browserSupportInterface) Name() string {
	return "browser-support"
}

func (iface *browserSupportInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              browserSupportSummary,
		ImplicitOnCore:       true,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: browserSupportBaseDeclarationSlots,
	}
}

func (iface *browserSupportInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	// It's fine if allow-sandbox isn't specified, but it it is,
	// it needs to be bool
	if v, ok := plug.Attrs["allow-sandbox"]; ok {
		if _, ok = v.(bool); !ok {
			return fmt.Errorf("browser-support plug requires bool with 'allow-sandbox'")
		}
	}

	return nil
}

func (iface *browserSupportInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var allowSandbox bool
	_ = plug.Attr("allow-sandbox", &allowSandbox)
	spec.AddSnippet(browserSupportConnectedPlugAppArmor)
	if allowSandbox {
		spec.AddSnippet(browserSupportConnectedPlugAppArmorWithSandbox)
	} else {
		spec.AddSnippet(browserSupportConnectedPlugAppArmorWithoutSandbox)
	}
	return nil
}

func (iface *browserSupportInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var allowSandbox bool
	_ = plug.Attr("allow-sandbox", &allowSandbox)
	snippet := browserSupportConnectedPlugSecComp
	if allowSandbox {
		snippet += browserSupportConnectedPlugSecCompWithSandbox
	}
	spec.AddSnippet(snippet)
	return nil
}

func (iface *browserSupportInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	return true
}

func init() {
	registerIface(&browserSupportInterface{})
}
