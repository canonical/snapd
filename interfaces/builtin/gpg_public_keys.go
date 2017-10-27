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

owner @{HOME}/.gnupg/ r,
owner @{HOME}/.gnupg/gpg.conf r,
owner @{HOME}/.gnupg/pubring.gpg{,.lock} r,
owner @{HOME}/.gnupg/pubring.gpg.lock k,
owner @{HOME}/.gnupg/pubring.kbx{,.lock} r,
owner @{HOME}/.gnupg/pubring.kbx.lock k,
owner @{HOME}/.gnupg/trustdb.gpg{,.lock} r,
owner @{HOME}/.gnupg/trustdb.gpg.lock k,
owner @{HOME}/.gnupg/openpgp-revocs.d/{,*} r,
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
