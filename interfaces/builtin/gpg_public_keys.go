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

const gpgPublicKeysSummary = `allows reading gpg public keys and non-sensitive configuration`

const gpgPublicKeysBaseDeclarationSlots = `
  gpg-public-keys:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const gpgPublicKeysConnectedPlugAppArmor = `
# Description: Can read gpg public keys and non-sensitive configuration

# Allow gpg encrypt, list-keys, verify, etc
/usr/bin/gpg{,1,2,v} ixr,
/usr/share/gnupg/options.skel r,

owner @{HOME}/.gnupg/ r,
owner @{HOME}/.gnupg/gpg.conf r,
owner @{HOME}/.gnupg/openpgp-revocs.d/{,*} r,
owner @{HOME}/.gnupg/pubring.gpg r,
owner @{HOME}/.gnupg/pubring.kbx r,
owner @{HOME}/.gnupg/trustedkeys.gpg r,

owner @{HOME}/.gnupg/trustdb.gpg r,
# gpg sometimes updates the trustdb to decide whether or not to update the
# trustdb. For now, silence the denial since no other policy references this
deny @{HOME}/.gnupg/trustdb.gpg w,
`

func init() {
	registerIface(&commonInterface{
		name:                  "gpg-public-keys",
		summary:               gpgPublicKeysSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  gpgPublicKeysBaseDeclarationSlots,
		connectedPlugAppArmor: gpgPublicKeysConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
