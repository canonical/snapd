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
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
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
	"github.com/snapcore/snapd/strutil"
)

var (
	backendResealKeyForBootChains        = backend.ResealKeyForBootChains
	backendNewInMemoryRecoveryKeyCache   = backend.NewInMemoryRecoveryKeyCache
	disksDMCryptUUIDFromMountPoint       = disks.DMCryptUUIDFromMountPoint
	bootHostUbuntuDataForMode            = boot.HostUbuntuDataForMode
	keysNewRecoveryKey                   = keys.NewRecoveryKey
	timeNow                              = time.Now
	secbootCheckRecoveryKey              = secboot.CheckRecoveryKey
	secbootReadContainerKeyData          = secboot.ReadContainerKeyData
	secbootListContainerRecoveryKeyNames = secboot.ListContainerRecoveryKeyNames
	secbootListContainerUnlockKeyNames   = secboot.ListContainerUnlockKeyNames
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

	m.recoveryKeyCache = backendNewInMemoryRecoveryKeyCache()

	st.Lock()
	defer st.Unlock()
	st.Cache(fdeMgrKey{}, m)

	snapstate.RegisterAffectedSnapsByKind("efi-secureboot-db-update", dbxUpdateAffectedSnaps)
	// this will block essential snaps from being refreshed with a conflict error
	// which is strange from a UX perspective but it is necessary to prevent
	// conflicting reseal/seal operations from racing when reading/writing
	// FDE state parameters.
	snapstate.RegisterAffectedSnapsByKind("fde-add-protected-keys", addProtectedKeysAffectedSnaps)

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

	runner.AddHandler("fde-add-recovery-keys", m.doAddRecoveryKeys, nil)
	runner.AddHandler("fde-remove-keys", m.doRemoveKeys, nil)
	runner.AddHandler("fde-rename-keys", m.doRenameKeys, nil)
	runner.AddHandler("fde-change-auth", m.doChangeAuth, nil)
	runner.AddHandler("fde-add-protected-keys", m.doAddProtectedKeys, nil)
	runner.AddBlocked(func(t *state.Task, running []*state.Task) bool {
		if isFDETask(t) {
			for _, tRunning := range running {
				if isFDETask(tRunning) {
					// prevent two fde operations from running in parallel
					return true
				}
			}
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

	for i := 0; i < len(foundDisks); i++ {
		for j := i + 1; j < len(foundDisks); j++ {
			// given how the list of found disks is built, this should never happen.
			//
			// it is still important to issue a warning if this ever happens
			// since most key slot operations under assume that the mapping
			// from container-role to volume is unique.
			if foundDisks[i].ContainerRole() == foundDisks[j].ContainerRole() {
				msg := fmt.Sprintf("unexpected system state detected, container roles for disk volumes should map to one volume only: container role %q maps to %s and %s", foundDisks[i].ContainerRole(), foundDisks[i].DevPath(), foundDisks[j].DevPath())
				state.Warnf(msg)
				logger.Noticef(msg)
			}
		}
	}

	return foundDisks, nil
}

type KeyslotType string

const (
	KeyslotTypeRecovery KeyslotType = "recovery"
	KeyslotTypePlatform KeyslotType = "platform"
)

// Keyslot represents a key associated with an encrypted container.
type Keyslot struct {
	// The unique key slot name on the corresponding encrypted container.
	Name string
	// This indicates whether this is a recovery or platform protected key.
	Type KeyslotType
	// This indicates the container role of the corresponding encrypted container.
	ContainerRole string

	devPath string
	keyData secboot.KeyData
}

// Ref returns the corresponding key slot reference.
func (k *Keyslot) Ref() KeyslotRef {
	return KeyslotRef{ContainerRole: k.ContainerRole, Name: k.Name}
}

// KeyData returns secboot.KeyData corresponding to the keyslot.
// This can only be called for KeyslotTypePlatform.
//
// Note: KeyData is lazy loaded once then reused in subsequent calls.
func (k *Keyslot) KeyData() (secboot.KeyData, error) {
	if k.keyData != nil {
		return k.keyData, nil
	}

	if k.Type != KeyslotTypePlatform {
		return nil, fmt.Errorf("internal error: Keyslot.KeyData() is only available for KeyslotTypePlatform, found %q", k.Type)
	}

	keyData, err := secbootReadContainerKeyData(k.devPath, k.Name)
	if err != nil {
		return nil, fmt.Errorf("cannot read key data for %q from %q: %v", k.Name, k.devPath, err)
	}
	k.keyData = keyData
	return keyData, nil
}

// GetKeyslots returns the key slots for the specified key slot references.
// If keyslotRefs is empty, all key slots on all encrypted containers will
// be returned.
func (m *FDEManager) GetKeyslots(keyslotRefs []KeyslotRef) (keyslots []Keyslot, missingRefs []KeyslotRef, err error) {
	allKeyslots := len(keyslotRefs) == 0

	targetKeyslotNamesByContainerRole := make(map[string][]string)
	for _, keyslotRef := range keyslotRefs {
		targetKeyslotNamesByContainerRole[keyslotRef.ContainerRole] = append(targetKeyslotNamesByContainerRole[keyslotRef.ContainerRole], keyslotRef.Name)
	}

	containers, err := m.GetEncryptedContainers()
	if err != nil {
		return nil, nil, err
	}

	keyslots = make([]Keyslot, 0, len(keyslotRefs))
	missingRefs = make([]KeyslotRef, 0)
	for _, container := range containers {
		targetKeyslotNames := targetKeyslotNamesByContainerRole[container.ContainerRole()]
		if !allKeyslots && len(targetKeyslotNames) == 0 {
			continue
		}

		var matchedContainerKeyslots []Keyslot

		// collect recovery key slots
		recoveryKeyNames, err := secbootListContainerRecoveryKeyNames(container.DevPath())
		if err != nil {
			return nil, nil, fmt.Errorf("cannot obtain recovery keys for %q: %v", container.DevPath(), err)
		}
		for _, recoveryKeyName := range recoveryKeyNames {
			if allKeyslots || strutil.ListContains(targetKeyslotNames, recoveryKeyName) {
				matchedContainerKeyslots = append(matchedContainerKeyslots, Keyslot{
					Name:          recoveryKeyName,
					Type:          KeyslotTypeRecovery,
					ContainerRole: container.ContainerRole(),
					devPath:       container.DevPath(),
				})
			}
		}

		// collect platform key slots
		platformKeyNames, err := secbootListContainerUnlockKeyNames(container.DevPath())
		if err != nil {
			return nil, nil, fmt.Errorf("cannot obtain platform keys for %q: %v", container.DevPath(), err)
		}
		for _, platformKeyName := range platformKeyNames {
			if allKeyslots || strutil.ListContains(targetKeyslotNames, platformKeyName) {
				matchedContainerKeyslots = append(matchedContainerKeyslots, Keyslot{
					Name:          platformKeyName,
					Type:          KeyslotTypePlatform,
					ContainerRole: container.ContainerRole(),
					devPath:       container.DevPath(),
				})
			}
		}

		// detect missing key slot references
		for _, targetKeyslotName := range targetKeyslotNames {
			found := false
			for _, keyslot := range matchedContainerKeyslots {
				if keyslot.Name == targetKeyslotName {
					found = true
					break
				}
			}
			if !found {
				missingRefs = append(missingRefs, KeyslotRef{ContainerRole: container.ContainerRole(), Name: targetKeyslotName})
			}
		}

		keyslots = append(keyslots, matchedContainerKeyslots...)
	}

	// XXX: return error if len(keyslots) != keyslotRefs to indicate duplicates?
	return keyslots, missingRefs, nil
}

// GetKeyslots returns the key slots for the specified key slot references.
// If keyslotRefs is empty, all key slots on all encrypted containers will
// be returned.
//
// The state needs to be locked by the caller.
func GetKeyslots(st *state.State, keyslotRefs []KeyslotRef) (keyslots []Keyslot, missingRefs []KeyslotRef, err error) {
	mgr := fdeMgr(st)
	return mgr.GetKeyslots(keyslotRefs)
}

var _ backend.FDEStateManager = (*unlockedStateManager)(nil)

func (m *FDEManager) resealKeyForBootChains(unlocker boot.Unlocker, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
	wrapped := &unlockedStateManager{
		FDEManager: m,
		unlocker:   unlocker,
	}
	return backendResealKeyForBootChains(wrapped, method, rootdir, params)
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

func (m *FDEManager) ensureParametersLoaded(role, containerRole string) error {
	hasParameters, _, _, _, err := m.GetParameters(role, containerRole)
	if err != nil {
		return err
	}
	if hasParameters {
		// nothing to do
		return nil
	}

	method, err := device.SealedKeysMethod(dirs.GlobalRootDir)
	if err != nil {
		return err
	}

	wrapped := &unlockedStateManager{
		FDEManager: m,
		unlocker:   m.state.Unlocker(),
	}
	return boot.WithBootChains(func(bc boot.BootChains) error {
		params := boot.ResealKeyForBootChainsParams{
			BootChains: bc,
			Options:    boot.ResealKeyToModeenvOptions{Force: true},
		}
		return backendResealKeyForBootChains(wrapped, method, dirs.GlobalRootDir, &params)
	}, method)
}

const recoveryKeyExpireAfter = 5 * time.Minute

func recoveryKeyID(rkey keys.RecoveryKey) (string, error) {
	hash := sha1.New()
	n, err := hash.Write(rkey[:])
	if err != nil {
		return "", err
	}
	if n != len(rkey) {
		return "", fmt.Errorf("internal error: %d bytes written, expected %d", n, len(rkey))
	}
	keyDigest := base64.URLEncoding.EncodeToString(hash.Sum(nil))
	return keyDigest[:10], nil
}

// GenerateRecoveryKey generates a recovery key and its corresponding id
// with an expiration time `recoveryKeyExpireAfter`.
func (m *FDEManager) GenerateRecoveryKey() (rkey keys.RecoveryKey, keyID string, err error) {
	if m.recoveryKeyCache == nil {
		return keys.RecoveryKey{}, "", errors.New("internal error: recoveryKeyCache is nil")
	}

	const maxRetries = 10
	var retryCnt int
	for {
		if retryCnt >= maxRetries {
			return keys.RecoveryKey{}, "", errors.New("internal error: cannot generate recovery key: max retries reached")
		}
		rkey, err = keysNewRecoveryKey()
		if err != nil {
			return keys.RecoveryKey{}, "", fmt.Errorf("internal error: cannot generate recovery key: %v", err)
		}
		keyID, err = recoveryKeyID(rkey)
		if err != nil {
			return keys.RecoveryKey{}, "", err
		}
		// check for key-id hash collision
		_, err := m.recoveryKeyCache.Key(keyID)
		if errors.Is(err, backend.ErrNoRecoveryKey) {
			// no collision
			break
		}
		if err != nil {
			return keys.RecoveryKey{}, "", err
		}
		// collision detected, retry
		retryCnt++
	}

	rkeyInfo := backend.CachedRecoverKey{
		Key:        rkey,
		Expiration: timeNow().Add(recoveryKeyExpireAfter),
	}

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
	return mgr.GenerateRecoveryKey()
}

// GetRecoveryKey retrieves a recovery key by its key-id. The key can only
// be retrieved once and is immediately deleted after being retrieved.
// An error is returned if the corresponding recovery key is expired.
//
// The state needs to be locked by the caller.
func GetRecoveryKey(st *state.State, keyID string) (rkey keys.RecoveryKey, err error) {
	mgr := fdeMgr(st)

	if mgr.recoveryKeyCache == nil {
		return keys.RecoveryKey{}, errors.New("internal error: recoveryKeyCache is nil")
	}

	rkeyInfo, err := mgr.recoveryKeyCache.Key(keyID)
	if err != nil {
		return keys.RecoveryKey{}, err
	}
	// generated recovery key can only be used once.
	if err := mgr.recoveryKeyCache.RemoveKey(keyID); err != nil {
		return keys.RecoveryKey{}, err
	}

	if rkeyInfo.Expired(time.Now()) {
		return keys.RecoveryKey{}, errors.New("recovery key has expired")
	}

	return rkeyInfo.Key, nil
}

// CheckRecoveryKey tests that the specified recovery key unlocks the
// specified container roles. If containerRoles is empty, the recovery
// key will be tested against all container roles. Also, If a container
// role from containerRoles does not exist, an error is returned.
//
// The state must be locked by the caller.
func (m *FDEManager) CheckRecoveryKey(rkey keys.RecoveryKey, containerRoles []string) error {
	containers, err := m.GetEncryptedContainers()
	if err != nil {
		return err
	}

	allContainers := len(containerRoles) == 0
	found := make(map[string]bool, len(containerRoles))
	for _, container := range containers {
		if allContainers || strutil.ListContains(containerRoles, container.ContainerRole()) {
			if err := secbootCheckRecoveryKey(container.DevPath(), rkey); err != nil {
				return fmt.Errorf("recovery key failed for %q: %v", container.ContainerRole(), err)
			}
			found[container.ContainerRole()] = true
		}
	}

	if !allContainers {
		for _, containerRole := range containerRoles {
			if !found[containerRole] {
				return fmt.Errorf("encrypted container role %q does not exist", containerRole)
			}
		}
	}

	return nil
}

// systemEncrypted reports whether FDE is enabled on the system.
// It returns true if FDE is enabled, or false otherwise.
func (m *FDEManager) systemEncrypted() (bool, error) {
	var s FdeState
	err := m.state.Get(fdeStateKey, &s)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return false, err
	}

	// Systems with FDE enabled will have a snapd managed primary key
	// with the exception of two legacy scenarios. See comment on type
	// FdeState field PrimaryKeys for details.
	hasPrimaryKey := len(s.PrimaryKeys) > 0
	return hasPrimaryKey, nil
}

func MockDisksDMCryptUUIDFromMountPoint(f func(mountpoint string) (string, error)) (restore func()) {
	osutil.MustBeTestBinary("mocking disks.DMCryptUUIDFromMountPoint can be done only from tests")

	old := disksDMCryptUUIDFromMountPoint
	disksDMCryptUUIDFromMountPoint = f
	return func() {
		disksDMCryptUUIDFromMountPoint = old
	}
}

func MockKeyslotKeyData(keyslot *Keyslot, kd secboot.KeyData) {
	osutil.MustBeTestBinary("mocking Keyslot.keyData can be done only from tests")
	keyslot.keyData = kd
}
