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

/usr/bin/gpg{,2} ixr,
/usr/share/gnupg/options.skel r,
owner @{HOME}/.gnupg/{,**} r,
# 'wk' is required for gpg encrypt/decrypt
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
