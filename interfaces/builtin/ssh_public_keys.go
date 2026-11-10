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

const sshPublicKeysSummary = `allows reading ssh public keys and host public keys and non-sensitive configuration`

const sshPublicKeysBaseDeclarationSlots = `
  ssh-public-keys:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const sshPublicKeysConnectedPlugAppArmor = `
# Description: Can read ssh public keys and non-sensitive configuration as well as host public keys.

/usr/bin/ssh ixr,
owner @{HOME}/.ssh/ r,
owner @{HOME}/.ssh/environment r,
owner @{HOME}/.ssh/*.pub r,
/etc/ssh/ssh_host_ecdsa_key.pub r,
/etc/ssh/ssh_host_ed25519_key.pub r,
/etc/ssh/ssh_host_rsa_key.pub r,
`

func init() {
	registerIface(&commonInterface{
		name:                  "ssh-public-keys",
		summary:               sshPublicKeysSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  sshPublicKeysBaseDeclarationSlots,
		connectedPlugAppArmor: sshPublicKeysConnectedPlugAppArmor,
	})
}
