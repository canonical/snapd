// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"github.com/snapcore/snapd/snap"
)

const dmCryptSummary = `allows encryption and decryption of block storage devices`

const dmCryptBaseDeclarationSlots = `
  dm-crypt:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const dmCryptBaseDeclarationPlugs = `
  dm-crypt:
    allow-installation: false
    deny-auto-connection: true
`

// The type for this interface
type dmCryptInterface struct{}

// XXX: this should not hardcode mount points like /run/media/ but unless we
// have an interface like "mount-control" this is needed
const dmCryptConnectedPlugAppArmor = `
# Allow mapper access
/dev/mapper/control rw,
/dev/dm-[0-9]* rwk,
# allow use of cryptsetup from core snap
/{,usr/}sbin/cryptsetup ixr,
# Mount points could be in /run/media/<user>/* or /media/<user>/*
/run/systemd/seats/* r,
/{,run/}media/{,**} rw,
mount options=(ro,nosuid,nodev) /dev/dm-[0-9]* -> /{,run/}media/**,
mount options=(rw,nosuid,nodev) /dev/dm-[0-9]* -> /{,run/}media/**,

#  exec mount/umount to do the actual operations
/{,usr/}bin/mount ixr,
/{,usr/}bin/umount ixr,

# mount/umount (via libmount) track some mount info in these files
/{,var/}run/mount/utab* wrlk,

# Allow access to the file locking mechanism
/{,var/}run/cryptsetup/ rw,
/{,var/}run/cryptsetup/* rwk,
/{,var/}run/ r,
`

const dmCryptConnectedPlugSecComp = `
# Description: Allow kernel keyring manipulation
add_key
keyctl
request_key
`

// dm-crypt
// Note that often dm-crypt is statically linked into the kernel (CONFIG_DM_CRYPT=y)
// This is usual for the custom kernels for projects where disk encryption is required.
var dmCryptConnectedPlugKmod = []string{
	"dm_crypt",
}

var dmCryptConnectedPlugUDev = []string{
	`KERNEL=="device-mapper"`,
	`KERNEL=="dm-[0-9]"`,
	`SUBSYSTEM=="block"`,
}

func (iface *dmCryptInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// Allow what is allowed in the declarations
	return true
}

func init() {
	registerIface(&commonInterface{
		name:                     "dm-crypt",
		summary:                  dmCryptSummary,
		implicitOnCore:           true,
		implicitOnClassic:        true,
		baseDeclarationSlots:     dmCryptBaseDeclarationSlots,
		baseDeclarationPlugs:     dmCryptBaseDeclarationPlugs,
		connectedPlugAppArmor:    dmCryptConnectedPlugAppArmor,
		connectedPlugSecComp:     dmCryptConnectedPlugSecComp,
		connectedPlugKModModules: dmCryptConnectedPlugKmod,
		connectedPlugUDev:        dmCryptConnectedPlugUDev,
	})
}
