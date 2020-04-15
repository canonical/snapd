// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !nosecboot

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

package secboot

import (
	sb "github.com/snapcore/secboot"
)

type TPMSupport = tpmSupport

func MockSbConnectToDefaultTPM(f func() (*secboot.TPMConnection, error)) (restore func()) {
	old := secbootConnectToDefaultTPM
	secbootConnectToDefaultTPM = f
	return func() {
		secbootConnectToDefaultTPM = old
	}
}

func MockSbProvisionTPM(f func(tpm *sb.TPMConnection, mode sb.ProvisionMode, newLockoutAuth []byte) error) (restore func()) {
	old := sbProvisionTPM
	sbProvisionTPM = f
	return func() {
		sbProvisionTPM = old
	}
}

func MockSbAddEFISecureBootPolicyProfile(f func(profile *sb.PCRProtectionProfile, params *sb.EFISecureBootPolicyProfileParams) error) (restore func()) {
	old := sbAddEFISecureBootPolicyProfile
	sbAddEFISecureBootPolicyProfile = f
	return func() {
		sbAddEFISecureBootPolicyProfile = old
	}
}

func MockSbAddSystemdEFIStubProfile(f func(profile *sb.PCRProtectionProfile, params *sb.SystemdEFIStubProfileParams) error) (restore func()) {
	old := sbAddSystemdEFIStubProfile
	sbAddSystemdEFIStubProfile = f
	return func() {
		sbAddSystemdEFIStubProfile = old
	}
}

func MockSbSealKeyToTPM(f func(tpm *sb.TPMConnection, key []byte, keyPath, policyUpdatePath string, params *sb.KeyCreationParams) error) (restore func()) {
	old := sbSealKeyToTPM
	sbSealKeyToTPM = f
	return func() {
		sbSealKeyToTPM = old
	}
}
