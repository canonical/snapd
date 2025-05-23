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
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
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

	DbxUpdateAffectedSnaps = dbxUpdateAffectedSnaps
)

type ExternalOperation = externalOperation

func MockBackendResealKeyForBootChains(f func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error) (restore func()) {
	restore = testutil.Backup(&backendResealKeyForBootChains)
	backendResealKeyForBootChains = f
	return restore
}

func MockBackendResealKeysForSignaturesDBUpdate(f func(updateState backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, payload []byte) error) (restore func()) {
	restore = testutil.Backup(&backendResealKeysForSignaturesDBUpdate)
	backendResealKeysForSignaturesDBUpdate = f
	return restore
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
