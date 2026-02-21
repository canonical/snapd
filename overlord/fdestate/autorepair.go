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

package fdestate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
)

var (
	bootloaderFind = bootloader.Find

	bootReadModeenv = boot.ReadModeenv

	secbootProvisionTPM        = secboot.ProvisionTPM
	secbootShouldAttemptRepair = secboot.ShouldAttemptRepair
	secbootPreinstallCheck     = secboot.PreinstallCheck

	osutilBootID = osutil.BootID
)

type AutoRepairResult string

const (
	AutoRepairNotInitialized          AutoRepairResult = "not-initialized"
	AutoRepairNotAttempted            AutoRepairResult = "not-attempted"
	AutoRepairFailedPlatformInit      AutoRepairResult = "failed-platform-init"
	AutoRepairFailedKeyslots          AutoRepairResult = "failed-keyslots"
	AutoRepairFailedEncryptionSupport AutoRepairResult = "failed-encryption-support"
	AutoRepairSuccess                 AutoRepairResult = "success"
)

type repairState struct {
	Result AutoRepairResult `json:"result"`
}

type repairStateForBoot struct {
	BootID string       `json:"boot-id"`
	State  *repairState `json:"state"`
}

const fdeRepairStateKey = "fde-repair-state"

func setRepairAttemptResult(st *state.State, rs *repairState) error {
	bootId, err := osutilBootID()
	if err != nil {
		return err
	}
	st.Set(fdeRepairStateKey, &repairStateForBoot{
		BootID: bootId,
		State:  rs,
	})
	return nil
}

func getRepairAttemptResult(st *state.State) (*repairState, error) {
	var rs repairStateForBoot
	if err := st.Get(fdeRepairStateKey, &rs); err != nil {
		if errors.Is(err, state.ErrNoState) {
			return nil, nil
		} else {
			return nil, err
		}
	}

	bootId, err := osutilBootID()
	if err != nil {
		return nil, err
	}

	if rs.BootID != bootId {
		st.Set(fdeRepairStateKey, nil)
		return nil, nil
	}

	return rs.State, nil
}

func getBootChain() ([]bootloader.BootFile, error) {
	modeenv, err := bootReadModeenv(dirs.GlobalRootDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read modeenv: %w", err)
	}

	rbl, err := bootloaderFind(boot.InitramfsUbuntuSeedDir, &bootloader.Options{
		Role: bootloader.RoleRecovery,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot find recovery bootloader: %w", err)
	}

	tbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		return nil, fmt.Errorf("internal error: recovery bootloader does not support trusted assets")
	}

	bl, err := bootloaderFind(boot.InitramfsUbuntuBootDir, &bootloader.Options{
		Role:        bootloader.RoleRunMode,
		NoSlashBoot: true,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot find run bootloader: %w", err)
	}

	ebl, ok := bl.(bootloader.ExtractedRunKernelImageBootloader)
	if !ok {
		return nil, fmt.Errorf("internal error: run bootloader does not support kernel extraction")
	}

	info, err := ebl.TryKernel()
	if err != nil {
		if err == bootloader.ErrNoTryKernelRef {
			info, err = ebl.Kernel()
		}
		if err != nil {
			return nil, err
		}
	}

	trustedAssets, err := tbl.TrustedAssets()
	if err != nil {
		return nil, err
	}

	kernelPath := info.MountFile()

	runModeBootChains, err := tbl.BootChains(bl, kernelPath)
	if err != nil {
		return nil, err
	}

	for _, runModeBootChain := range runModeBootChains {
		var chain []bootloader.BootFile

		if len(runModeBootChain) == 0 {
			return nil, fmt.Errorf("internal error: no file in boot chain")
		}

		ignoreChain := false
		for _, bf := range runModeBootChain[:len(runModeBootChain)-1] {
			path := bf.Path
			name, ok := trustedAssets[path]
			if !ok {
				return nil, fmt.Errorf("internal error: unknown trusted asset %s from boot chain", path)
			}
			var hashes []string
			if bf.Role == bootloader.RoleRecovery {
				hashes, ok = modeenv.CurrentTrustedRecoveryBootAssets[name]
			} else {
				hashes, ok = modeenv.CurrentTrustedBootAssets[name]
			}
			if !ok {
				ignoreChain = true
				break
			}

			hash := hashes[len(hashes)-1]
			p := filepath.Join(dirs.SnapBootAssetsDir, bl.Name(), fmt.Sprintf("%s-%s", name, hash))
			chain = append(chain, bootloader.NewBootFile("", p, bf.Role))
		}
		if !ignoreChain {
			return append(chain, runModeBootChain[len(runModeBootChain)-1]), nil
		}
	}

	return nil, fmt.Errorf("cannot find the active boot chain")
}

func autoRepair(st *state.State) (AutoRepairResult, error) {
	method, err := device.SealedKeysMethod(dirs.GlobalRootDir)
	if err != nil {
		return AutoRepairNotAttempted, err
	}

	switch method {
	case device.SealingMethodFDESetupHook:
	case device.SealingMethodTPM, device.SealingMethodLegacyTPM:
		images, err := getBootChain()
		if err != nil {
			return AutoRepairNotAttempted, err
		}
		const postInstall = true
		_, _, err = secbootPreinstallCheck(context.Background(), postInstall, images)

		if err != nil {
			logger.Noticef("WARNING: could not auto repair keyslots due to failed platform initialization: %v", err)
			return AutoRepairFailedPlatformInit, nil
		}

		lockoutAuthFile := device.TpmLockoutAuthUnder(boot.InstallHostFDESaveDir)
		if err := secbootProvisionTPM(secboot.TPMPartialReprovision, lockoutAuthFile); err != nil {
			logger.Noticef("WARNING: could not repair platform: %v", err)
			return AutoRepairFailedPlatformInit, nil
		}
	default:
		return AutoRepairNotAttempted, fmt.Errorf("unknown key sealing method: %q", method)
	}

	mgr := fdeMgr(st)
	wrapped := &unlockedStateManager{
		FDEManager: mgr,
		unlocker:   st.Unlocker(),
	}
	err = boot.WithBootChains(func(bc boot.BootChains) error {
		params := boot.ResealKeyForBootChainsParams{
			BootChains: bc,
			Options:    boot.ResealKeyToModeenvOptions{Force: true},
		}
		return backendResealKeyForBootChains(wrapped, method, dirs.GlobalRootDir, &params)
	}, method)

	if err != nil {
		logger.Noticef("WARNING: could not auto repair keyslots: %v", err)
		return AutoRepairFailedKeyslots, nil
	}

	return AutoRepairSuccess, nil
}

// AttemptAutoRepairIfNeeded looks at the activation state and status
// of lockout reset and may attempt to repair keyslots.
func AttemptAutoRepairIfNeeded(st *state.State, lockoutResetErr error) error {
	if lockoutResetErr != nil {
		// FIXME: we need to either try repair in some cases and save the
		// error for the status API
		return lockoutResetErr
	}

	previousResult, err := getRepairAttemptResult(st)
	if err != nil {
		return err
	}
	if previousResult != nil {
		return nil
	}

	s, err := getActivateState(st)

	if err == errNoActivateState {
		logger.Noticef("WARNING: the system booted with an old initrd without using activation API")
		unlockedState, err := bootLoadDiskUnlockState("unlocked.json")
		if err != nil {
			// errNoActivateState means the file must exist
			return err
		}
		if unlockedState.UbuntuData.UnlockKey != "recovery" && unlockedState.UbuntuSave.UnlockKey != "recovery" {
			setRepairAttemptResult(st, &repairState{Result: AutoRepairNotAttempted})
			return nil
		}
	} else if os.IsNotExist(err) {
		logger.Noticef("WARNING: the system booted with an old initrd without unlocked status reporting")
		setRepairAttemptResult(st, &repairState{Result: AutoRepairNotAttempted})
		return nil
	} else if err != nil {
		logger.Noticef("WARNING: error while getting activation state: %v", err)
		setRepairAttemptResult(st, &repairState{Result: AutoRepairNotAttempted})
		return nil
	} else {
		if !secbootShouldAttemptRepair(s) {
			setRepairAttemptResult(st, &repairState{Result: AutoRepairNotAttempted})
			return nil
		}
	}

	result, err := autoRepair(st)
	if err != nil {
		return err
	}
	setRepairAttemptResult(st, &repairState{Result: result})

	return nil
}
