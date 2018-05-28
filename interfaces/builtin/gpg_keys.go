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

const gpgKeysSummary = `allows reading gpg user configuration and keys and updating gpg's random seed file`

const gpgKeysBaseDeclarationSlots = `
  gpg-keys:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const gpgKeysConnectedPlugAppArmor = `
# Description: Can read gpg user configuration as well as public and private
# keys. Also allows updating gpg's random seed file.

# Allow gpg encrypt, decrypt, list-keys, verify, sign, etc
/usr/bin/gpg{,1,2,v} ixr,
/usr/share/gnupg/options.skel r,

owner @{HOME}/.gnupg/{,**} r,
# gpg sometimes updates the trustdb to decide whether or not to update the
# trustdb. For now, silence the denial since no other policy references this
deny @{HOME}/.gnupg/trustdb.gpg w,

# 'wk' is required for gpg encrypt and sign unless --no-random-seed-file is
# used. Ideally we would not allow this access, but denying it causes gpg to
# hang for all applications that don't specify --no-random-seed-file. Allow it
# with the understanding that this interface already requires a high level of
# trust to access the user's keys and the level of trust for the random_seed
# is commensurate with accessing the private keys.
owner @{HOME}/.gnupg/random_seed wk,
`

func init() {
	registerIface(&commonInterface{
		name:                  "gpg-keys",
		summary:               gpgKeysSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  gpgKeysBaseDeclarationSlots,
		connectedPlugAppArmor: gpgKeysConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
