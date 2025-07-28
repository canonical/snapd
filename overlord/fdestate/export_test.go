// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package fdestate

import (
	"time"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/testutil"
)

var (
	FdeMgr = fdeMgr

	UpdateParameters = updateParameters

	IsEFISecurebootDBUpdateBlocked = isEFISecurebootDBUpdateBlocked

	FindFirstPendingExternalOperationByKind = findFirstPendingExternalOperationByKind
	FindFirstExternalOperationByChangeID    = findFirstExternalOperationByChangeID
	AddExternalOperation                    = addExternalOperation
	AddEFISecurebootDBUpdateChange          = addEFISecurebootDBUpdateChange
	UpdateExternalOperation                 = updateExternalOperation

	NotifyDBXUpdatePrepareDoneOK = notifyDBXUpdatePrepareDoneOK
	DbxUpdatePreparedOKChan      = dbxUpdatePreparedOKChan

	DbxUpdateAffectedSnaps        = dbxUpdateAffectedSnaps
	AddProtectedKeysAffectedSnaps = addProtectedKeysAffectedSnaps

	CheckFDEChangeConflict = checkFDEChangeConflict
)

type ExternalOperation = externalOperation

func MockBackendResealKeyForBootChains(f func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error) (restore func()) {
	restore = testutil.Backup(&backendResealKeyForBootChains)
	backendResealKeyForBootChains = f
	return restore
}

func MockBackendResealKeysForSignaturesDBUpdate(f func(updateState backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, payload []byte) error) (restore func()) {
	restore = testutil.Backup(&backendResealKeysForSignaturesDBUpdate)
	backendResealKeysForSignaturesDBUpdate = f
	return restore
}

func MockBackendNewInMemoryRecoveryKeyCache(f func() backend.RecoveryKeyCache) (restore func()) {
	return testutil.Mock(&backendNewInMemoryRecoveryKeyCache, f)
}

func MockBackendLoadParametersForBootChains(f func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, bootChains boot.BootChains) error) (restore func()) {
	return testutil.Mock(&backendLoadParametersForBootChains, f)
}

func MockKeysNewRecoveryKey(f func() (keys.RecoveryKey, error)) (restore func()) {
	return testutil.Mock(&keysNewRecoveryKey, f)
}

func MockTimeNow(f func() time.Time) (restore func()) {
	return testutil.Mock(&timeNow, f)
}

var NewModel = newModel

func (m *FDEManager) IsFunctional() error { return m.isFunctional() }

func MockBootHostUbuntuDataForMode(f func(mode string, mod gadget.Model) ([]string, error)) (restore func()) {
	old := bootHostUbuntuDataForMode
	bootHostUbuntuDataForMode = f
	return func() {
		bootHostUbuntuDataForMode = old
	}
}

func EncryptedContainer(uuid string, containerRole string, legacyKeys map[string]string) *encryptedContainer {
	return &encryptedContainer{
		uuid:          uuid,
		containerRole: containerRole,
		legacyKeys:    legacyKeys,
	}
}

func MockSecbootCheckRecoveryKey(f func(devicePath string, rkey keys.RecoveryKey) error) (restore func()) {
	return testutil.Mock(&secbootCheckRecoveryKey, f)
}

func MockSecbootReadContainerKeyData(f func(devicePath string, slotName string) (secboot.KeyData, error)) (restore func()) {
	return testutil.Mock(&secbootReadContainerKeyData, f)
}

func (k *Keyslot) SetDevPath(devPath string) {
	k.devPath = devPath
}

func MockSecbootListContainerRecoveryKeyNames(f func(devicePath string) ([]string, error)) (restore func()) {
	return testutil.Mock(&secbootListContainerRecoveryKeyNames, f)
}

func MockSecbootListContainerUnlockKeyNames(f func(devicePath string) ([]string, error)) (restore func()) {
	return testutil.Mock(&secbootListContainerUnlockKeyNames, f)
}

func MockSecbootAddContainerRecoveryKey(f func(devicePath string, slotName string, rkey keys.RecoveryKey) error) (restore func()) {
	return testutil.Mock(&secbootAddContainerRecoveryKey, f)
}

func MockSecbootAddContainerTPMProtectedKey(f func(devicePath string, slotName string, params *secboot.ProtectKeyParams) error) (restore func()) {
	return testutil.Mock(&secbootAddContainerTPMProtectedKey, f)
}

func MockSecbootDeleteContainerKey(f func(devicePath string, slotName string) error) (restore func()) {
	return testutil.Mock(&secbootDeleteContainerKey, f)
}

func MockSecbootRenameContainerKey(f func(devicePath string, oldName string, newName string) error) (restore func()) {
	return testutil.Mock(&secbootRenameContainerKey, f)
}

func KeyslotsAlreadyExistsError(keyslots []Keyslot) keyslotsAlreadyExistsError {
	return keyslotsAlreadyExistsError{keyslots: keyslots}
}

func MockChangeAuthOptionsInCache(st *state.State, old, new string) (restore func()) {
	st.Lock()
	defer st.Unlock()
	st.Cache(changeAuthOptionsKey{}, &changeAuthOptions{old: old, new: new})

	return func() { st.Cache(changeAuthOptionsKey{}, nil) }
}

func GetChangeAuthOptionsFromCache(st *state.State) *changeAuthOptions {
	cached := st.Cached(changeAuthOptionsKey{})
	if cached == nil {
		return nil
	}
	return cached.(*changeAuthOptions)
}

func (o *changeAuthOptions) Old() string {
	return o.old
}

func (o *changeAuthOptions) New() string {
	return o.new
}

func VolumesAuthOptionsKey() volumesAuthOptionsKey {
	return volumesAuthOptionsKey{}
}
