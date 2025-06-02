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

// Package fdestate implements the manager and state responsible for
// managing full disk encryption keys.
package fdestate

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/snapdenv"
)

var (
	backendResealKeyForBootChains      = backend.ResealKeyForBootChains
	backendNewInMemoryRecoveryKeyStore = backend.NewInMemoryRecoveryKeyCache
	disksDMCryptUUIDFromMountPoint     = disks.DMCryptUUIDFromMountPoint
	bootHostUbuntuDataForMode          = boot.HostUbuntuDataForMode
	keysNewRecoveryKey                 = keys.NewRecoveryKey
	timeNow                            = time.Now
)

// FDEManager is responsible for managing full disk encryption keys.
type FDEManager struct {
	state   *state.State
	initErr error

	preseed bool
	mode    string

	recoveryKeyCache backend.RecoveryKeyCache
}

type fdeMgrKey struct{}

func initModeFromModeenv(m *FDEManager) error {
	mode, explicit, err := boot.SystemMode("")
	if err != nil {
		return err
	}

	if explicit {
		// FDE manager is only relevant on systems where mode set explicitly,
		// that is UC20
		m.mode = mode
	}
	return nil
}

func Manager(st *state.State, runner *state.TaskRunner) (*FDEManager, error) {
	m := &FDEManager{
		state:   st,
		preseed: snapdenv.Preseeding(),
	}

	boot.ResealKeyForBootChains = m.resealKeyForBootChains

	if !secboot.WithSecbootSupport {
		m.initErr = fmt.Errorf("FDE manager is not operational in builds without secboot support")
	} else if m.preseed {
		// nothing to do in preseeding mode, but set the init error so that
		// attempts to use fdemgr will fail
		m.initErr = fmt.Errorf("internal error: FDE manager cannot be used in preseeding mode")
	} else {
		if err := initModeFromModeenv(m); err != nil {
			return nil, err
		}
	}

	m.recoveryKeyCache = backendNewInMemoryRecoveryKeyStore()

	st.Lock()
	defer st.Unlock()
	st.Cache(fdeMgrKey{}, m)

	snapstate.RegisterAffectedSnapsByKind("efi-secureboot-db-update", dbxUpdateAffectedSnaps)

	runner.AddHandler("efi-secureboot-db-update-prepare",
		m.doEFISecurebootDBUpdatePrepare, m.undoEFISecurebootDBUpdatePrepare)
	runner.AddCleanup("efi-secureboot-db-update-prepare", m.doEFISecurebootDBUpdatePrepareCleanup)
	runner.AddHandler("efi-secureboot-db-update", m.doEFISecurebootDBUpdate, nil)
	runner.AddBlocked(func(t *state.Task, running []*state.Task) bool {
		switch t.Kind() {
		case "efi-secureboot-db-update":
			return isEFISecurebootDBUpdateBlocked(t)
		}

		return false
	})

	return m, nil
}

// Ensure implements StateManager.Ensure
func (m *FDEManager) Ensure() error {
	return nil
}

// StartUp implements StateStarterUp.Startup
func (m *FDEManager) StartUp() error {
	if m.initErr != nil {
		// FDE manager was already disabled in constructor
		return nil
	}

	m.state.Lock()
	defer m.state.Unlock()

	err := func() error {
		if m.mode == "run" {
			// TODO:FDEM: should we try to initialize the state in
			// install/recover/factory-reset modes?
			if err := initializeState(m.state); err != nil {
				return fmt.Errorf("cannot initialize FDE state: %v", err)
			}
		}
		return nil
	}()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot complete FDE state manager startup: %v\n", err)
		logger.Noticef("cannot complete FDE state manager startup: %v", err)
		// keep track of the error
		m.initErr = err
	}

	return nil
}

func (m *FDEManager) isFunctional() error {
	// TODO:FDEM: use more specific errors to capture different error states
	return m.initErr
}

// ReloadModeenv is a helper function for forcing a reload of modeenv. Only
// useful in integration testing.
func (m *FDEManager) ReloadModeenv() error {
	osutil.MustBeTestBinary("ReloadModeenv can only be called from tests")
	return initModeFromModeenv(m)
}

type unlockedStateManager struct {
	*FDEManager
	unlocker boot.Unlocker
}

func (m *unlockedStateManager) Update(role string, containerRole string, parameters *backend.SealingParameters) error {
	return m.UpdateParameters(role, containerRole, parameters.BootModes, parameters.Models, parameters.TpmPCRProfile)
}

func (m *unlockedStateManager) Get(role string, containerRole string) (parameters *backend.SealingParameters, err error) {
	hasParamters, bootModes, models, tpmPCRProfile, err := m.GetParameters(role, containerRole)
	if err != nil || !hasParamters {
		return nil, err
	}

	return &backend.SealingParameters{
		BootModes:     bootModes,
		Models:        models,
		TpmPCRProfile: tpmPCRProfile,
	}, nil
}

func (m *unlockedStateManager) Unlock() (relock func()) {
	if m.unlocker != nil {
		return m.unlocker()
	}
	return func() {}
}

type encryptedContainer struct {
	uuid          string
	containerRole string
	legacyKeys    map[string]string
}

func (disk *encryptedContainer) ContainerRole() string {
	return disk.containerRole
}

func (disk *encryptedContainer) LegacyKeys() map[string]string {
	return disk.legacyKeys
}

func (disk *encryptedContainer) DevPath() string {
	return fmt.Sprintf("/dev/disk/by-uuid/%s", disk.uuid)
}

// GetEncryptedContainers returns the encrypted disks with their keys for
// the current device.
// The list of encrypted disks has no specific order.
func (m *FDEManager) GetEncryptedContainers() ([]backend.EncryptedContainer, error) {
	return getEncryptedContainers(m.state)
}

func getEncryptedContainers(state *state.State) ([]backend.EncryptedContainer, error) {
	var foundDisks []backend.EncryptedContainer

	deviceCtx, err := snapstate.DeviceCtx(state, nil, nil)
	if err != nil {
		return nil, err
	}
	model := deviceCtx.Model()

	dataMountPoints, err := bootHostUbuntuDataForMode(deviceCtx.SystemMode(), model)
	if err != nil {
		logger.Noticef("cannot determine the data mount in this mode: %v", err)
	}
	if err == nil && len(dataMountPoints) != 0 {
		uuid, err := disksDMCryptUUIDFromMountPoint(dataMountPoints[0])
		if err != nil {
			if !errors.Is(err, disks.ErrNoDmUUID) {
				return nil, fmt.Errorf("cannot find UUID for mount %s: %v", dataMountPoints[0], err)
			}
		} else {
			legacyKeys := make(map[string]string)
			defaultPath := device.DataSealedKeyUnder(boot.InitramfsBootEncryptionKeyDir)
			if osutil.FileExists(defaultPath) {
				legacyKeys["default"] = defaultPath
			}
			defaultFallbackPath := device.FallbackDataSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir)
			if osutil.FileExists(defaultFallbackPath) {
				legacyKeys["default-fallback"] = defaultFallbackPath
			}

			foundDisks = append(foundDisks, &encryptedContainer{uuid: uuid, containerRole: "system-data", legacyKeys: legacyKeys})
		}
	}

	uuid, err := disksDMCryptUUIDFromMountPoint(dirs.SnapSaveDir)

	if err != nil {
		if !errors.Is(err, disks.ErrNoDmUUID) {
			return nil, fmt.Errorf("cannot find UUID for mount %s: %v", dirs.SnapSaveDir, err)
		}
	} else {
		legacyKeys := make(map[string]string)
		defaultFactoryResetFallbackPath := device.FactoryResetFallbackSaveSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir)
		defaultFallbackPath := device.FallbackSaveSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir)
		if osutil.FileExists(defaultFactoryResetFallbackPath) {
			legacyKeys["default-fallback"] = defaultFactoryResetFallbackPath
		} else if osutil.FileExists(defaultFallbackPath) {
			legacyKeys["default-fallback"] = defaultFallbackPath
		}
		foundDisks = append(foundDisks, &encryptedContainer{uuid: uuid, containerRole: "system-save", legacyKeys: legacyKeys})
	}

	return foundDisks, nil
}

var _ backend.FDEStateManager = (*unlockedStateManager)(nil)

func (m *FDEManager) resealKeyForBootChains(unlocker boot.Unlocker, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
	wrapped := &unlockedStateManager{
		FDEManager: m,
		unlocker:   unlocker,
	}
	return backendResealKeyForBootChains(wrapped, method, rootdir, params, expectReseal)
}

func fdeMgr(st *state.State) *FDEManager {
	c := st.Cached(fdeMgrKey{})
	if c == nil {
		panic("internal error: FDE manager is not yet associated with state")
	}
	return c.(*FDEManager)
}

func (m *FDEManager) UpdateParameters(role string, containerRole string, bootModes []string, models []secboot.ModelForSealing, tpmPCRProfile []byte) error {
	return updateParameters(m.state, role, containerRole, bootModes, models, tpmPCRProfile)
}

func (m *FDEManager) GetParameters(role string, containerRole string) (hasParameters bool, bootModes []string, models []secboot.ModelForSealing, tpmPCRProfile []byte, err error) {
	var s FdeState
	err = m.state.Get(fdeStateKey, &s)
	if err != nil {
		return false, nil, nil, nil, err
	}

	return s.getParameters(role, containerRole)
}

const recoveryKeyExpireAfter = 5 * time.Minute

func (m *FDEManager) generateRecoveryKey() (rkey keys.RecoveryKey, keyID string, err error) {
	if m.recoveryKeyCache == nil {
		return keys.RecoveryKey{}, "", errors.New("internal error: recoveryKeyCache is nil")
	}

	rkey, err = keysNewRecoveryKey()
	if err != nil {
		return keys.RecoveryKey{}, "", err
	}

	rkeyInfo := backend.CachedRecoverKey{
		Key:        rkey,
		Expiration: timeNow().Add(recoveryKeyExpireAfter),
	}

	var lastRecoveryKeyID int
	err = m.state.Get("last-recovery-key-id", &lastRecoveryKeyID)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return keys.RecoveryKey{}, "", err
	}
	lastRecoveryKeyID++
	keyID = strconv.Itoa(lastRecoveryKeyID)
	m.state.Set("last-recovery-key-id", lastRecoveryKeyID)

	if err := m.recoveryKeyCache.AddKey(keyID, rkeyInfo); err != nil {
		return keys.RecoveryKey{}, "", err
	}

	return rkey, keyID, nil
}

// GenerateRecoveryKey generates a recovery key and its corresponding id
// with an expiration time `recoveryKeyExpireAfter`.
//
// The state needs to be locked by the caller.
func GenerateRecoveryKey(st *state.State) (rkey keys.RecoveryKey, keyID string, err error) {
	mgr := fdeMgr(st)
	return mgr.generateRecoveryKey()
}

func (m *FDEManager) getRecoveryKey(keyID string) (rkey keys.RecoveryKey, err error) {
	if m.recoveryKeyCache == nil {
		return keys.RecoveryKey{}, errors.New("internal error: recoveryKeyCache is nil")
	}

	rkeyInfo, err := m.recoveryKeyCache.Key(keyID)
	if err != nil {
		return keys.RecoveryKey{}, err
	}
	// generated recovery key can only be used once.
	if err := m.recoveryKeyCache.RemoveKey(keyID); err != nil {
		return keys.RecoveryKey{}, err
	}

	if rkeyInfo.Expired(time.Now()) {
		return keys.RecoveryKey{}, errors.New("recovery key has expired")
	}

	return rkeyInfo.Key, nil
}

// GetRecoveryKey retrieves a recovery key by its key-id. The key can only
// be retrieved once and is immediately deleted after being retrieved.
// An error is returned if the corresponding recovery key is expired.
//
// The state needs to be locked by the caller.
func GetRecoveryKey(st *state.State, keyID string) (rkey keys.RecoveryKey, err error) {
	mgr := fdeMgr(st)
	return mgr.getRecoveryKey(keyID)
}

func MockDisksDMCryptUUIDFromMountPoint(f func(mountpoint string) (string, error)) (restore func()) {
	osutil.MustBeTestBinary("mocking disks.DMCryptUUIDFromMountPoint can be done only from tests")

	old := disksDMCryptUUIDFromMountPoint
	disksDMCryptUUIDFromMountPoint = f
	return func() {
		disksDMCryptUUIDFromMountPoint = old
	}
}
