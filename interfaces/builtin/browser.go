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
	"github.com/snapcore/snapd/interfaces"
)

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/log-observe
const browserConnectedPlugAppArmor = `
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
`

const browserConnectedPlugAppArmorWithSandbox = `
# Policy needed only when using the chrome/chromium setuid sandbox
unix (receive, send) peer=(label=snap.@{SNAP_NAME}.**),
ptrace (trace) peer=snap.@{SNAP_NAME}.**,

# FIXME: make this as attribute for setuid sandbox policy
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
`

const browserConnectedPlugSecComp = `
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

const browserConnectedPlugSecCompWithSandbox = `
# Policy needed only when using the chrome/chromium setuid sandbox
chroot
# TODO: fine-tune when seccomp arg filtering available in stable distro
# releases
setuid
setgid
`

// NewBrowserInterface returns a new "browser" interface.
func NewBrowserInterface() interfaces.Interface {
	return &commonInterface{
		name: "browser",
		connectedPlugAppArmor: browserConnectedPlugAppArmor,
		connectedPlugSecComp:  browserConnectedPlugSecComp,
		reservedForOS:         true,
		autoConnect:           true,
	}
}
