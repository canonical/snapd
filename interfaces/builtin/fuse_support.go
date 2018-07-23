// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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
	"github.com/snapcore/snapd/release"
)

const fuseSupportSummary = `allows access to the FUSE file system`

const fuseSupportBaseDeclarationSlots = `
  fuse-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const fuseSupportConnectedPlugSecComp = `
# Description: Can run a FUSE filesystem. Unprivileged fuse mounts are
# not supported at this time.

mount
`

const fuseSupportConnectedPlugAppArmor = `
# Description: Can run a FUSE filesystem. Unprivileged fuse mounts are
# not supported at this time.

# Allow communicating with fuse kernel driver
# https://www.kernel.org/doc/Documentation/filesystems/fuse.txt
/dev/fuse rw,

# Required for mounts
capability sys_admin,

# Allow mounts to our snap-specific writable directories
# Note 1: fstype is 'fuse.<command>', eg 'fuse.sshfs'
# Note 2: due to LP: #1612393 - @{HOME} can't be used in mountpoint
# Note 3: local fuse mounts of filesystem directories are mediated by
#         AppArmor. The actual underlying file in the source directory is
#         mediated, not the presentation layer of the target directory, so
#         we can safely allow all local mounts to our snap-specific writable
#         directories.
# Note 4: fuse supports a lot of different mount options, and applications
#         are not obligated to use fusermount to mount fuse filesystems, so
#         be very strict and only support the default (rw,nosuid,nodev) and
#         read-only.
mount fstype=fuse.* options=(ro,nosuid,nodev) ** -> /home/*/snap/@{SNAP_NAME}/@{SNAP_REVISION}/{,**/},
mount fstype=fuse.* options=(rw,nosuid,nodev) ** -> /home/*/snap/@{SNAP_NAME}/@{SNAP_REVISION}/{,**/},
mount fstype=fuse.* options=(ro,nosuid,nodev) ** -> /var/snap/@{SNAP_NAME}/@{SNAP_REVISION}/{,**/},
mount fstype=fuse.* options=(rw,nosuid,nodev) ** -> /var/snap/@{SNAP_NAME}/@{SNAP_REVISION}/{,**/},
mount fstype=fuse.* options=(ro,nosuid,nodev) ** -> /home/*/snap/@{SNAP_NAME}/common/{,**/},
mount fstype=fuse.* options=(rw,nosuid,nodev) ** -> /home/*/snap/@{SNAP_NAME}/common/{,**/},
mount fstype=fuse.* options=(ro,nosuid,nodev) ** -> /var/snap/@{SNAP_NAME}/common/{,**/},
mount fstype=fuse.* options=(rw,nosuid,nodev) ** -> /var/snap/@{SNAP_NAME}/common/{,**/},

# Explicitly deny reads to /etc/fuse.conf. We do this to ensure that
# the safe defaults of fuse are used (which are enforced by our mount
# rules) and not system-specific options from /etc/fuse.conf that
# may conflict with our mount rules.
deny /etc/fuse.conf r,

# Allow read access to the fuse filesystem
/sys/fs/fuse/ r,
/sys/fs/fuse/** r,

# Unprivileged fuser mounts must use the setuid helper in the core snap
# (not currently available, so don't include in policy at this time).
#/{,usr/}bin/fusermount ixr,
`

var fuseSupportConnectedPlugUDev = []string{`KERNEL=="fuse"`}

func init() {
	registerIface(&commonInterface{
		name:                  "fuse-support",
		summary:               fuseSupportSummary,
		implicitOnCore:        true,
		implicitOnClassic:     !(release.ReleaseInfo.ID == "ubuntu" && release.ReleaseInfo.VersionID == "14.04"),
		baseDeclarationSlots:  fuseSupportBaseDeclarationSlots,
		reservedForOS:         true,
		connectedPlugAppArmor: fuseSupportConnectedPlugAppArmor,
		connectedPlugSecComp:  fuseSupportConnectedPlugSecComp,
		connectedPlugUDev:     fuseSupportConnectedPlugUDev,
	})
}
