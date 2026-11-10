// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

// https://www.kernel.org/doc/html/latest/crypto/userspace-if.html
// https://www.kernel.org/doc/html/latest/crypto/intro.html
const kernelCryptoAPISummary = `allows access to the Linux kernel crypto API`

// The kernel crypto API is designed to be used by any process (ie, using it
// requires no special privileges). Since it provides a kernel surface and
// has a CVE history, manually connect for now.
const kernelCryptoAPIBaseDeclarationSlots = `
  kernel-crypto-api:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const kernelCryptoAPIConnectedPlugAppArmor = `
# Description: Can access the Linux kernel crypto API
@{PROC}/crypto r,

# socket(AF_ALG, SOCK_SEQPACKET, ...)
network alg seqpacket,

# socket(AF_NETLINK, SOCK_{DGRAM,RAW}, NETLINK_CRYPTO)
network netlink dgram,
network netlink raw,
`

const kernelCryptoAPIConnectedPlugSeccomp = `
# Description: Can access the Linux kernel crypto API
socket AF_NETLINK - NETLINK_CRYPTO
bind
accept
`

func init() {
	registerIface(&commonInterface{
		name:                  "kernel-crypto-api",
		summary:               kernelCryptoAPISummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		connectedPlugAppArmor: kernelCryptoAPIConnectedPlugAppArmor,
		connectedPlugSecComp:  kernelCryptoAPIConnectedPlugSeccomp,
		baseDeclarationSlots:  kernelCryptoAPIBaseDeclarationSlots,
	})
}
