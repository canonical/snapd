// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package secboot

// EncryptionType specifies what encryption backend should be used (if any)
type EncryptionType string

const (
	EncryptionTypeNone        EncryptionType = ""
	EncryptionTypeLUKS        EncryptionType = "cryptsetup"
	EncryptionTypeLUKSWithICE EncryptionType = "cryptsetup-with-inline-crypto-engine"
)

// TODO:ICE: all EncryptionTypes are LUKS based now so this could be removed?
func (et EncryptionType) IsLUKS() bool {
	return et == EncryptionTypeLUKS || et == EncryptionTypeLUKSWithICE
}

type RecoveryKeyDevice struct {
	// PartLabel of the device
	PartLabel string
	// AuthorizingKeyFile is the path to the key to authorize the
	// operation, if empty, then it is assumed that the authorization key is
	// present in the user session keyring
	AuthorizingKeyFile string
}
