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
	"github.com/snapcore/secboot"
)

var (
	EnsureLayoutCompatibility = ensureLayoutCompatibility
	DeviceFromRole            = deviceFromRole
	KernelCmdlines            = kernelCmdlines
)

type TPMSupport = tpmSupport

func MockProvisionTPM(f func(tpm *secboot.TPMConnection, mode secboot.ProvisionMode,
	newLockoutAuth []byte) error) (restore func()) {
	old := provisionTPM
	provisionTPM = f
	return func() {
		provisionTPM = old
	}
}

func MockAddEFISecureBootPolicyProfile(f func(profile *secboot.PCRProtectionProfile,
	params *secboot.EFISecureBootPolicyProfileParams) error) (restore func()) {
	old := addEFISecureBootPolicyProfile
	addEFISecureBootPolicyProfile = f
	return func() {
		addEFISecureBootPolicyProfile = old
	}
}

func MockAddSystemdEFIStubProfile(f func(profile *secboot.PCRProtectionProfile,
	params *secboot.SystemdEFIStubProfileParams) error) (restore func()) {
	old := addSystemdEFIStubProfile
	addSystemdEFIStubProfile = f
	return func() {
		addSystemdEFIStubProfile = old
	}
}

func MockSealKeyToTPM(f func(tpm *secboot.TPMConnection, key []byte, keyPath, policyUpdatePath string,
	params *secboot.KeyCreationParams) error) (restore func()) {
	old := sealKeyToTPM
	sealKeyToTPM = f
	return func() {
		sealKeyToTPM = old
	}
}
