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

const gpgKeysSummary = `allows reading gpg user configuration and keys`

const gpgKeysBaseDeclarationSlots = `
  gpg-keys:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const gpgKeysConnectedPlugAppArmor = `
# Description: Can read gpg user configuration as well as public and private
# keys.

# Allow gpg encrypt, decrypt, list-keys, verify, sign, etc
/usr/bin/gpg{,1,2,v} ixr,
/usr/share/gnupg/options.skel r,

owner @{HOME}/.gnupg/{,**} r,
# gpg sometimes updates the trustdb to decide whether or not to update the
# trustdb. For now, silence the denial since no other policy references this
deny @{HOME}/.gnupg/trustdb.gpg w,

# gpg stores its internal random pool in @{HOME}/.gnupg/random_seed to make
# random number generation faster. 'gpg --no-random-seed-file' disables this
# optimization. Force consumers of this interface to use --no-random-seed-file.
audit deny @{HOME}/.gnupg/random_seed wk,
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
