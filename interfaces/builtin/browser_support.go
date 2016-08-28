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
	"fmt"

	"github.com/snapcore/snapd/interfaces"
)

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/log-observe
const browserSupportConnectedPlugAppArmor = `
# Description: Can access various APIs needed by modern browers (eg, Google
# Chrome/Chromium and Mozilla) and file paths they expect. This interface is
# transitional and is only in place while upstream's work to change their paths
# and snappy is updated to properly mediate the APIs.
# Usage: reserved

# This allows raising the OOM score of other processes owned by the user.
owner @{PROC}/@{pid}/oom_score_adj rw,

# Chrome/Chromium should be fixed to honor TMPDIR or the snap packaging
# adjusted to use LD_PRELOAD technique from LP: #1577514
/var/tmp/ r,
owner /var/tmp/etilqs_* rw,

# Chrome/Chromium should be modified to use snap.$SNAP_NAME.* or the snap
# packaging adjusted to use LD_PRELOAD technique from LP: #1577514
owner /dev/shm/.org.chromium.Chromium.* rw,
owner /dev/shm/.com.google.Chrome.* rw,

# Chrome/Chromium should be adjusted to not use gconf. It is only used with
# legacy systems that don't have snapd
deny dbus (send)
    bus=session
    interface="org.gnome.GConf.Server",
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

# Policy needed only when using the chrome/chromium setuid sandbox
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
`

const browserSupportConnectedPlugSecComp = `
# Description: Can access various APIs needed by modern browers (eg, Google
# Chrome/Chromium and Mozilla) and file paths they expect. This interface is
# transitional and is only in place while upstream's work to change their paths
# and snappy is updated to properly mediate the APIs.
# Usage: reserved

# for anonymous sockets
bind
listen

# TODO: fine-tune when seccomp arg filtering available in stable distro
# releases
setpriority
`

const browserSupportConnectedPlugSecCompWithSandbox = `
# Policy needed only when using the chrome/chromium setuid sandbox
chroot
# TODO: fine-tune when seccomp arg filtering available in stable distro
# releases
setuid
setgid

# Policy needed for Mozilla userns sandbox
unshare
quotactl
`

type BrowserSupportInterface struct{}

func (iface *BrowserSupportInterface) Name() string {
	return "browser-support"
}

func (iface *BrowserSupportInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *BrowserSupportInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface.Name()))
	}

	// It's fine if allow-sandbox isn't specified, but it it is,
	// it needs to be bool
	if v, ok := plug.Attrs["allow-sandbox"]; ok {
		if _, ok = v.(bool); !ok {
			return fmt.Errorf("browser-support plug requires bool with 'allow-sandbox'")
		}
	}

	return nil
}

func (iface *BrowserSupportInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *BrowserSupportInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {

	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *BrowserSupportInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	allowSandbox, _ := plug.Attrs["allow-sandbox"].(bool)

	switch securitySystem {
	case interfaces.SecurityAppArmor:
		snippet := []byte(browserSupportConnectedPlugAppArmor)
		if allowSandbox {
			snippet = append(snippet, browserSupportConnectedPlugAppArmorWithSandbox...)
		} else {
			snippet = append(snippet, browserSupportConnectedPlugAppArmorWithoutSandbox...)
		}
		return snippet, nil
	case interfaces.SecuritySecComp:
		snippet := []byte(browserSupportConnectedPlugSecComp)
		if allowSandbox {
			snippet = append(snippet, browserSupportConnectedPlugSecCompWithSandbox...)
		}
		return snippet, nil
	case interfaces.SecurityDBus, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *BrowserSupportInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *BrowserSupportInterface) AutoConnect() bool {
	return true
}
