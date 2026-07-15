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
	"strings"
	"time"

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
	secbootPostinstallCheck    = secboot.PostinstallCheck

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

type RecommendedRemedialAction string

const (
	RecommendedRemedialActionPermitManual         RecommendedRemedialAction = "permit-manual"
	RecommendedRemedialActionRequireReprovision   RecommendedRemedialAction = "require-reprovision"
	RecommendedRemedialActionRequirePlatformReset RecommendedRemedialAction = "require-platform-reset"
)

const (
	postInstallCheckTimeout = 2 * time.Minute
)

type repairState struct {
	Result          AutoRepairResult            `json:"result"`
	Recommendations []RecommendedRemedialAction `json:"recommendations",omitempty`
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

// GetRunBootChain returns the boot chain expected to be used
// for a normal "run" mode boot.
//
// The image files in the bootchain will either point a file in a snap
// or to a file in the trusted boot asset cache. They will not
// point to the effective path where the read from, though they
// are expected to be the same, unless boot partition were compromised.
func GetRunBootChain() ([]bootloader.BootFile, error) {
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

	// runModeBootChains is all possible run boot chains, but only one should exist (there
	// are legacy boot chains before we registered UEFI boot entries).
	// The "BootFile"s for the gadget part points to identifier names instead of real path, so we
	// need to resolve those. To resolve those we need to cross check with the modeenv, and then
	// find the file in the cache. The last one, is the kernel and should be pointing to the right place.
	for _, runModeBootChain := range runModeBootChains {
		var chain []bootloader.BootFile

		if len(runModeBootChain) == 0 {
			// That is not possible for a boot chain to be size 0, because that would mean there is no
			// kernel. We should not ignore this, there are bigger problems.
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

			// In theory we should only have one hash here. Multiple would be when we are trying
			// a boot chain, and this should have been cleaned. It should be safe to take the last one (newest).
			if len(hashes) > 1 {
				logger.Noticef("WARNING: multiple hashes for a trusted boot file were found.")
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

func autoRepair(st *state.State, runPostInstallChecks bool) (AutoRepairResult, error) {
	method, err := device.SealedKeysMethod(dirs.GlobalRootDir)
	if err != nil {
		return AutoRepairNotAttempted, err
	}

	switch method {
	case device.SealingMethodFDESetupHook:
	case device.SealingMethodTPM, device.SealingMethodLegacyTPM:
		if runPostInstallChecks {
			images, err := GetRunBootChain()
			if err != nil {
				return AutoRepairNotAttempted, err
			}

			ctx, cancel := context.WithTimeout(context.Background(), postInstallCheckTimeout)
			defer cancel()

			if _, details, err := secbootPostinstallCheck(ctx, images); len(details) > 0 || err != nil {
				if err != nil {
					logger.Noticef("WARNING: could not auto repair keyslots due to failed platform initialization: %v", err)
				} else {
					var messages []string
					for _, detail := range details {
						messages = append(messages, fmt.Sprintf("- %s", detail.Message))
					}
					logger.Noticef("WARNING: could not auto repair keyslots due to failed platform initialization:\n%s", strings.Join(messages, "\n"))
				}
				return AutoRepairFailedEncryptionSupport, nil
			}
		}

		lockoutAuthFile := device.TpmLockoutAuthUnder(boot.InstallHostFDESaveDir)
		// TODO: possibly we do not need to rotate the authorization keys for a repair...
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
// of lockout reset and may attempt to repair keyslots. If the
// auto-repair attempted has already occurred during the current boot,
// this will do nothing.
func AttemptAutoRepairIfNeeded(st *state.State, lockoutResetErr error, runPostInstallChecks bool) error {
	// let's get the result from previous attempt during the
	// current boot
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
			return lockoutResetErr
		}
	} else if os.IsNotExist(err) {
		logger.Noticef("WARNING: the system booted with an old initrd without unlocked status reporting")
		setRepairAttemptResult(st, &repairState{Result: AutoRepairNotAttempted})
		return lockoutResetErr
	} else if err != nil {
		logger.Noticef("WARNING: error while getting activation state: %v", err)
		setRepairAttemptResult(st, &repairState{Result: AutoRepairNotAttempted})
		return lockoutResetErr
	} else {
		remedialActions := secbootShouldAttemptRepair(s, lockoutResetErr)
		if !remedialActions.AttemptRepair {
			var recommendations []RecommendedRemedialAction

			if remedialActions.RequireReprovision {
				recommendations = append(recommendations, RecommendedRemedialActionRequireReprovision)
			}
			if remedialActions.PermitManual {
				recommendations = append(recommendations, RecommendedRemedialActionPermitManual)
			}

			setRepairAttemptResult(st, &repairState{
				Result:          AutoRepairNotAttempted,
				Recommendations: recommendations,
			})
			return nil
		}
	}

	result, err := autoRepair(st, runPostInstallChecks)
	if err != nil {
		return err
	}

	var recommendations []RecommendedRemedialAction
	if result != AutoRepairSuccess {
		recommendations = append(recommendations, RecommendedRemedialActionRequireReprovision)
	}
	setRepairAttemptResult(st, &repairState{
		Result:          result,
		Recommendations: recommendations,
	})

	return nil
}
