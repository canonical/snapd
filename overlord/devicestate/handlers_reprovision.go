// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package devicestate

import (
	"context"
	"errors"
	"fmt"
	"os"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/fdestate"
	fdeBackend "github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/snap"
)

var (
	bootMakeRunnableReprovision = boot.MakeRunnableSystemReprovision

	secbootListContainerUnlockKeyNames   = secboot.ListContainerUnlockKeyNames
	secbootListContainerRecoveryKeyNames = secboot.ListContainerRecoveryKeyNames
	secbootTestProtectorKey              = secboot.TestProtectorKey
	secbootRenameContainerKey            = secboot.RenameContainerKey
	secbootGetPCRHandleFromToken         = secboot.GetPCRHandleFromToken
	secbootReleasePCRResourceHandle      = secboot.ReleasePCRResourceHandle
	secbootDeleteContainerKey            = secboot.DeleteContainerKey
	secbootSaveCheckResult               = (*secboot.PreinstallCheckContext).SaveCheckResult
	secbootCheckResult                   = (*secboot.PreinstallCheckContext).CheckResult

	keysNewProtectorKey    = keys.NewProtectorKey
	keysCreateProtectedKey = (keys.ProtectorKey).CreateProtectedKey
	keysPlainKeyWrite      = (*keys.PlainKey).Write
	keysSaveProtectorKey   = func(key keys.ProtectorKey, path string) error { return key.SaveToFile(path) }

	snapstateKernelInfo = snapstate.KernelInfo

	fdestateGetEncryptedContainers = fdestate.GetEncryptedContainers
)

func convertToBootstrappedContainer(devicePath string) (secboot.BootstrappedContainer, error) {
	bootstrapKey, err := keys.NewEncryptionKey()
	if err != nil {
		return nil, fmt.Errorf("cannot create encryption key: %v", err)
	}

	if err := secbootDeleteContainerKey(devicePath, "bootstrap-key"); err != nil {
		logger.Debugf("cannot delete bootstrap-key on %s", devicePath)
	}

	if err := secbootAddBootstrapKeyOnExistingDisk(devicePath, bootstrapKey); err != nil {
		return nil, err
	}

	return secbootCreateBootstrappedContainer(secboot.DiskUnlockKey(bootstrapKey), devicePath), nil
}

func hookKeyProtectorFactoryImpl(m *DeviceManager, kernelInfo *snap.Info) (secboot.KeyProtectorFactory, error) {
	return m.hookKeyProtectorFactory(kernelInfo)
}

var hookKeyProtectorFactory = hookKeyProtectorFactoryImpl

func (m *DeviceManager) doReprovision(t *state.Task, _ *tomb.Tomb) error {
	renames := []struct {
		// new is the name of latest keyslot if there are 2
		// keyslots
		new string
		// old is the name of the older keyslot, that is the
		// backup keyslot if it is alone, it should get
		// renamed to new.
		old string
	}{
		{"default", "snapd-reprovision-default"},
		{"default-fallback", "snapd-reprovision-default-fallback"},
		{"default-recovery", "snapd-reprovision-default-recovery"},
	}

	// TODO: we should allow unlocking during sealing since hashing
	// boot assets is expected to take time and will block snapd
	// from answering.
	st := t.State()
	st.Lock()
	defer st.Unlock()

	disks, err := fdestateGetEncryptedContainers(st)
	if err != nil {
		return err
	}

	var dataDisk fdeBackend.EncryptedContainer
	var saveDisk fdeBackend.EncryptedContainer
	for _, disk := range disks {
		switch disk.ContainerRole() {
		case "system-data":
			if dataDisk != nil {
				return fmt.Errorf("multiple containers found with role system-data")
			}
			dataDisk = disk
		case "system-save":
			if saveDisk != nil {
				return fmt.Errorf("multiple containers found with role system-save")
			}
			saveDisk = disk
		}
	}
	if saveDisk == nil {
		return fmt.Errorf("no save container found")
	}
	if dataDisk == nil {
		return fmt.Errorf("no data container found")
	}

	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return fmt.Errorf("cannot get device context: %v", err)
	}

	saveKeyPath := device.SaveKeyUnder(dirs.SnapFDEDir)
	oldSaveKey, err := os.ReadFile(saveKeyPath)
	if err != nil {
		return err
	}

	// revertReprovisionAttempt is called either if we detect that we have already
	// called reprovision but did not reach step 6. Or if we
	// return with error before step 6.
	revertReprovisionAttempt := func() {
		removedNvIndices := map[uint32]bool{}

		// Rollback steps:
		//   1. Erase the nv indices from new keys that were not used
		//   2. Delete all new keyslots
		//   3. Rename old keyslots back to the original names
		for _, disk := range []string{dataDisk.DevPath(), saveDisk.DevPath()} {
			platformKeyslots, err := secbootListContainerUnlockKeyNames(disk)
			if err != nil {
				logger.Debugf("cannot list keyslots on %s, ignoring disk.", disk)
				continue
			}
			hasPlatformKeyslot := map[string]bool{}
			hasKeySlot := map[string]bool{}
			for _, k := range platformKeyslots {
				hasPlatformKeyslot[k] = true
				hasKeySlot[k] = true
			}
			recoveryKeyNames, err := secbootListContainerRecoveryKeyNames(disk)
			if err != nil {
				logger.Debugf("cannot list recovery keyslots on %s, ignoring disk.", disk)
				continue
			}
			for _, k := range recoveryKeyNames {
				hasKeySlot[k] = true
			}

			for _, rename := range renames {
				if hasPlatformKeyslot[rename.old] && hasPlatformKeyslot[rename.new] {
					nv, err := secbootGetPCRHandleFromToken(disk, rename.new)
					if err != nil {
						logger.Debugf("cannot read nv index for %s on %s", rename.new, disk)
					} else if (nv != 0) && !removedNvIndices[nv] {
						if err := secbootReleasePCRResourceHandle(nv); err != nil {
							logger.Debugf("cannot release nv index for %s on %s", rename.new, disk)
						} else {
							removedNvIndices[nv] = true
						}
					}
				}

				if !hasKeySlot[rename.old] {
					logger.Debugf("keyslot %s does not exist on %s, it must have been renamed already.", rename.old, disk)
					continue
				}

				// The save disk default key needs to be handled last outside the loop in case we crash.
				// This is will be the marker for completion.
				if rename.new == "default" && disk == saveDisk.DevPath() {
					continue
				}
				if err := secbootDeleteContainerKey(disk, rename.new); err != nil {
					logger.Debugf("cannot remove %s on %s", rename.new, disk)
				}
				if err := secbootRenameContainerKey(disk, rename.old, rename.new); err != nil {
					logger.Debugf("cannot rename %s to %s on %s", rename.old, rename.new, disk)
				}
			}
		}
		if err := secbootDeleteContainerKey(dataDisk.DevPath(), "bootstrap-key"); err != nil {
			logger.Debugf("cannot delete bootstrap-key on %s", dataDisk.DevPath())
		}
		if err := secbootDeleteContainerKey(saveDisk.DevPath(), "bootstrap-key"); err != nil {
			logger.Debugf("cannot delete bootstrap-key on %s", saveDisk.DevPath())
		}

		// The last clean up
		if err := secbootDeleteContainerKey(saveDisk.DevPath(), "default"); err != nil {
			logger.Debugf("could remove default on %s", saveDisk.DevPath())
		}
		if err := secbootRenameContainerKey(saveDisk.DevPath(), "snapd-reprovision-default", "default"); err != nil {
			logger.Debugf("cannot rename snapd-reprovision-default to default on %s", saveDisk.DevPath())
		}
	}

	platformKeyNames, err := secbootListContainerUnlockKeyNames(saveDisk.DevPath())
	if err != nil {
		return err
	}

	for _, key := range platformKeyNames {
		if key == "snapd-reprovision-default" {
			// TODO: use a timeout in the context.
			oldKeyMatches, err := secbootTestProtectorKey(context.Background(), saveDisk.DevPath(), key, oldSaveKey)
			if err != nil {
				return err
			}
			// TODO: if the old key does not match, we are probably in a case a previous attempt
			// finished, but did not remove yet the old keys. In that case we should just continue
			// the cleaning instead. To do that we will need a way to identify the new key to the task.
			// For example we could store a digest of the plainkey's primary key, and if it matches, we could
			// just jump to after step 6.
			if oldKeyMatches {
				// We must have been restarted in the middle, before step 6.
				// The backed up keys are the correct ones, we need to cancel that previous
				// attempt and then go back to expected name.
				revertReprovisionAttempt()
			}
		}
	}

	var setupData *reprovisionSetupData
	cached := st.Cached(reprovisionSetupDataKey{})
	if cached == nil {
		return fmt.Errorf("missing reprovision context")
	} else {
		var ok bool
		setupData, ok = cached.(*reprovisionSetupData)
		if !ok {
			return fmt.Errorf("internal error: wrong data type for reprovisionSetupDataKey")
		}
	}

	if setupData.checkContext == nil {
		return fmt.Errorf("missing post install check context")
	}
	if setupData.recoveryKeyID == "" {
		return fmt.Errorf("missing recovery key")
	}

	// TODO: On Ubuntu Core, we should not need a recovery key
	recoveryKey, err := fdestateGetRecoveryKey(st, setupData.recoveryKeyID)
	if err != nil {
		return err
	}

	revertReprovisionAttemptOnError := true
	defer func() {
		if !revertReprovisionAttemptOnError {
			return
		}
		revertReprovisionAttempt()
	}()

	// Step 1. rename existing keyslots that we will overwrite

	for _, disk := range []string{dataDisk.DevPath(), saveDisk.DevPath()} {
		knownKeysBeforeRenames := map[string]bool{}
		platformKeyNames, err := secbootListContainerUnlockKeyNames(disk)
		if err != nil {
			return err
		}
		for _, k := range platformKeyNames {
			knownKeysBeforeRenames[k] = true
		}
		recoveryKeyNames, err := secbootListContainerRecoveryKeyNames(disk)
		if err != nil {
			return err
		}
		for _, k := range recoveryKeyNames {
			knownKeysBeforeRenames[k] = true
		}

		for _, rename := range renames {
			if err := secbootDeleteContainerKey(disk, rename.old); err != nil {
				// We do not expect it to exist, we should not fail on error.
				// For example due to previous run not cleaned up.
				// We know it is not a key that is still in use because we
				// checked snapd-reprovision-default previously.
				if knownKeysBeforeRenames[rename.old] {
					logger.Noticef("WARNING: cannot delete %s on %s: %v", rename.old, disk, err)
				} else {
					logger.Debugf("cannot delete %s on %s as expected: %v", rename.old, disk, err)
				}
			} else {
				// A previous run was not cleaned up, we need to log it.
				logger.Noticef("successfully deleted unexpected %s on %s", rename.old, disk)
			}
			if knownKeysBeforeRenames[rename.new] {
				if err := secbootRenameContainerKey(disk, rename.new, rename.old); err != nil {
					return err
				}
			}
		}
	}

	dataContainer, err := convertToBootstrappedContainer(dataDisk.DevPath())
	if err != nil {
		return err
	}

	saveContainer, err := convertToBootstrappedContainer(saveDisk.DevPath())
	if err != nil {
		return err
	}

	if err := dataContainer.AddRecoveryKey("default-recovery", recoveryKey); err != nil {
		return err
	}
	if err := saveContainer.AddRecoveryKey("default-recovery", recoveryKey); err != nil {
		return err
	}

	protectorKey, err := keysNewProtectorKey()
	if err != nil {
		return err
	}

	// Steps:
	//  2. Generate primary key
	//  3. Create plainkey protector key (and save to keyslot)
	plainKey, primaryKey, unlockPlainKey, err := keysCreateProtectedKey(protectorKey, nil)
	if err != nil {
		return err
	}

	if err := saveContainer.AddKey("default", unlockPlainKey); err != nil {
		return err
	}
	tokenWriter, err := saveContainer.GetTokenWriter("default")
	if err != nil {
		return err
	}
	if err := keysPlainKeyWrite(plainKey, tokenWriter); err != nil {
		return err
	}

	kernelInfo, err := snapstateKernelInfo(st, deviceCtx)
	if err != nil {
		return fmt.Errorf("cannot get kernel info: %v", err)
	}

	keyProtector, err := hookKeyProtectorFactory(m, kernelInfo)
	if err != nil && !errors.Is(err, secboot.ErrNoKeyProtector) {
		return err
	}

	// No volumes option, we reprovision without PIN or passphrase
	var volumesAuth *device.VolumesAuthOptions = nil

	errorDetails, err := secbootPreinstallCheckAction(setupData.checkContext, context.Background(), &secboot.PreinstallAction{Action: secboot.ActionNone})
	if err != nil {
		return err
	}

	if len(errorDetails) != 0 {
		// This should happen only if the client is trying
		// reprovision without fully fixing the postinstall
		// checks issues. Which is an error from the client.
		for _, e := range errorDetails {
			logger.Debugf("while attempting reprovision, postinstall check found an issue: %s", e.Message)
		}
		return fmt.Errorf("postinstall check found some issues")
	}

	checkResult, err := secbootCheckResult(setupData.checkContext)
	if err != nil {
		return err
	}

	if err := secbootSaveCheckResult(setupData.checkContext, device.PreinstallCheckResultUnder(dirs.SnapSaveDir)); err != nil {
		return err
	}

	// Steps:
	//   4. Reprovision the TPM
	//   5. Create new set of keyslots
	encryptionParams := boot.NewEncryptionSetup(dataContainer, saveContainer,
		primaryKey,
		volumesAuth,
		checkResult)

	err = bootMakeRunnableReprovision(
		deviceCtx.Model(),
		keyProtector,
		encryptionParams,
	)

	if err != nil {
		return fmt.Errorf("cannot make system runnable: %v", err)
	}

	// Step 7. Swap the state
	// TODO: Actually swap the state. And move it after step 6.
	var oldState any
	errGetState := st.Get("fde", &oldState)
	if errGetState != nil && !errors.Is(errGetState, state.ErrNoState) {
		return fmt.Errorf("internal error: cannot get the fde state %v", err)
	}
	st.Set("fde", nil)

	// Step 6. write the protector key
	if err := keysSaveProtectorKey(protectorKey, saveKeyPath); err != nil {
		if errGetState == nil {
			st.Set("fde", oldState)
		}
		return fmt.Errorf("cannot save the system-save key: %v", err)
	}
	// swapping the protector key is the sign we have finished
	revertReprovisionAttemptOnError = false

	// Steps:
	//   8. Erase nv counters associated to the old keys
	//   9. Remove the keys (plain key last)
	removedNvIndices := map[uint32]bool{}

	for _, disk := range []string{dataDisk.DevPath(), saveDisk.DevPath()} {
		recoveryKeyNames, err := secbootListContainerRecoveryKeyNames(disk)
		if err != nil {
			return err
		}
		for _, key := range recoveryKeyNames {
			if key == "default-recovery" {
				continue
			}
			if err := secbootDeleteContainerKey(disk, key); err != nil {
				return err
			}
		}
		platformKeyNames, err := secbootListContainerUnlockKeyNames(disk)
		if err != nil {
			return err
		}

		for _, key := range platformKeyNames {
			if key == "default" || key == "default-fallback" {
				continue
			}

			nv, err := secbootGetPCRHandleFromToken(disk, key)
			if err != nil {
				return err
			} else if (nv != 0) && !removedNvIndices[nv] {
				if err := secbootReleasePCRResourceHandle(nv); err != nil {
					return err
				} else {
					removedNvIndices[nv] = true
				}
			}

			if key == "snapd-reprovision-default" && disk == saveDisk.DevPath() {
				// Always the last one to be removed. This will be done after the loop.
				// This is a marker for completion.
				continue
			}

			if err := secbootDeleteContainerKey(disk, key); err != nil {
				return err
			}
		}
	}

	if err := secbootDeleteContainerKey(saveDisk.DevPath(), "snapd-reprovision-default"); err != nil {
		return err
	}

	t.SetStatus(state.DoneStatus)

	return nil
}
