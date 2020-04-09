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

func MockSecbootProvisionTPM(f func(tpm *secboot.TPMConnection, mode secboot.ProvisionMode,
	newLockoutAuth []byte) error) (restore func()) {
	old := secbootProvisionTPM
	secbootProvisionTPM = f
	return func() {
		secbootProvisionTPM = old
	}
}

func MockSecbootAddEFISecureBootPolicyProfile(f func(profile *secboot.PCRProtectionProfile,
	params *secboot.EFISecureBootPolicyProfileParams) error) (restore func()) {
	old := secbootAddEFISecureBootPolicyProfile
	secbootAddEFISecureBootPolicyProfile = f
	return func() {
		secbootAddEFISecureBootPolicyProfile = old
	}
}

func MockSecbootAddSystemdEFIStubProfile(f func(profile *secboot.PCRProtectionProfile,
	params *secboot.SystemdEFIStubProfileParams) error) (restore func()) {
	old := secbootAddSystemdEFIStubProfile
	secbootAddSystemdEFIStubProfile = f
	return func() {
		secbootAddSystemdEFIStubProfile = old
	}
}

func MockSecbootSealKeyToTPM(f func(tpm *secboot.TPMConnection, key []byte, keyPath, policyUpdatePath string,
	params *secboot.KeyCreationParams) error) (restore func()) {
	old := secbootSealKeyToTPM
	secbootSealKeyToTPM = f
	return func() {
		secbootSealKeyToTPM = old
	}
}
