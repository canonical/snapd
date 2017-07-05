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

const classicSupportSummary = `special permissions for the classic snap`

const classicSupportBaseDeclarationPlugs = `
  classic-support:
    allow-installation: false
    deny-auto-connection: true
`

const classicSupportBaseDeclarationSlots = `
  classic-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const classicSupportPlugAppArmor = `
# Description: permissions to use classic dimension. This policy is
# intentionally not restricted. This gives device ownership to
# connected snaps.

# Description: permissions to use classic dimension. This policy is intentionally
# not restricted. This gives device ownership to connected snaps.

# for 'create'
/{,usr/}bin/unsquashfs ixr,
/var/lib/snapd/snaps/core_*.snap r,
capability chown,
capability fowner,
capability mknod,

# This allows running anything unconfined
/{,usr/}bin/sudo Uxr,
capability fsetid,
capability dac_override,

# Allow copying configuration to the chroot
/etc/{,**} r,
/var/lib/extrausers/{,*} r,

# Allow bind mounting various directories
capability sys_admin,
/{,usr/}bin/mount ixr,
/{,usr/}bin/mountpoint ixr,
/run/mount/utab rw,
@{PROC}/[0-9]*/mountinfo r,
mount options=(rw bind) /home/ -> /var/snap/@{SNAP_NAME}/**/,
mount options=(rw bind) /run/ -> /var/snap/@{SNAP_NAME}/**/,
mount options=(rw bind) /proc/ -> /var/snap/@{SNAP_NAME}/**/,
mount options=(rw bind) /sys/ -> /var/snap/@{SNAP_NAME}/**/,
mount options=(rw bind) /dev/ -> /var/snap/@{SNAP_NAME}/**/,
mount options=(rw bind) / -> /var/snap/@{SNAP_NAME}/**/,
mount fstype=devpts options=(rw) devpts -> /dev/pts/,
mount options=(rw rprivate) -> /var/snap/@{SNAP_NAME}/**/,

# reset
/{,usr/}bin/umount ixr,
umount /var/snap/@{SNAP_NAME}/**/,

# These rules allow running anything unconfined as well as managing systemd
/usr/bin/systemd-run Uxr,
/bin/systemctl Uxr,
`

const classicSupportPlugSecComp = `
# Description: permissions to use classic dimension. This policy is intentionally
# not restricted. This gives device ownership to connected snaps.
# create
chown
chown32
lchown
lchown32
fchown
fchown32
fchownat
mknod
chroot

# sudo
bind
sendmsg
sendmmsg
sendto
recvfrom
recvmsg
setgroups
setgroups32

# classic
mount
getsockopt

# reset
umount
umount2
`

func init() {
	registerIface(&commonInterface{
		name:                  "classic-support",
		summary:               classicSupportSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  classicSupportBaseDeclarationPlugs,
		baseDeclarationSlots:  classicSupportBaseDeclarationSlots,
		connectedPlugAppArmor: classicSupportPlugAppArmor,
		connectedPlugSecComp:  classicSupportPlugSecComp,
	})
}
