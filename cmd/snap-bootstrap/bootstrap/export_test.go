// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package bootstrap

import (
	"github.com/chrisccoulson/ubuntu-core-fde-utils"
)

var (
	EnsureLayoutCompatibility = ensureLayoutCompatibility
	DeviceFromRole            = deviceFromRole
)

type TPMSupport = tpmSupport

func MockProvisionTPM(f func(tpm *fdeutil.TPMConnection, mode fdeutil.ProvisionMode, newLockoutAuth []byte,
	auths *fdeutil.ProvisionAuths) error) (restore func()) {
	old := provisionTPM
	provisionTPM = f
	return func() {
		provisionTPM = old
	}
}
func MockSealKeyToTPM(f func(tpm *fdeutil.TPMConnection, keyDest, privateDest string,
	create *fdeutil.CreationParams, policy *fdeutil.PolicyParams, key []byte) error) (restore func()) {
	old := sealKeyToTPM
	sealKeyToTPM = f
	return func() {
		sealKeyToTPM = old
	}
}
