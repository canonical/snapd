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
	"crypto"
	"errors"
	"fmt"
	"os"
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

	secbootProvisionTPM        = secboot.ProvisionTPM
	secbootShouldAttemptRepair = secboot.ShouldAttemptRepair
	secbootPostinstallCheck    = secboot.PostinstallCheck

	osutilBootID = osutil.BootID

	bootGetRunBootChain = boot.GetRunBootChain
	bootReadModeenv     = boot.ReadModeenv
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
	Recommendations []RecommendedRemedialAction `json:"recommendations,omitempty"`
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

func autoRepair(st *state.State, runPostInstallChecks bool) (AutoRepairResult, error) {
	method, err := device.SealedKeysMethod(dirs.GlobalRootDir)
	if err != nil {
		return AutoRepairNotAttempted, err
	}

	switch method {
	case device.SealingMethodFDESetupHook:
	case device.SealingMethodTPM, device.SealingMethodLegacyTPM:
		if runPostInstallChecks {
			modeenv, err := bootReadModeenv(dirs.GlobalRootDir)
			if err != nil {
				return AutoRepairNotAttempted, err
			}

			images, err := bootGetRunBootChain(modeenv)
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
		// First we check that unlocked primary keys are matching.
		//  * secboot *does* unlock with unmatching primary key if the keyslot uses a protector key from the data disk.
		//  * When primary keys do not match, we will always need reprovision.
		//  * Unfortunately, the activation state alone cannot be used to decide whether this is a case of auto repair, or whether reprovision is required.
		// The most likely scenario in which this can happen is a hard reset in the middle of reprovision. So we do need to restart the
		// reprovision process.
		disks, err := GetEncryptedContainers(st)
		if err != nil {
			return err
		}
		var salt []byte
		var digest []byte
		primaryKeysMatch := true
		for i, disk := range disks {
			if i == 0 {
				var err error
				salt, digest, err = secbootGetPrimaryKeyDigest(disk.DevPath(), crypto.Hash(defaultHashAlg))
				if err != nil {
					if errors.Is(err, secboot.ErrKernelKeyNotFound) {
						break
					}
					return err
				}
			} else {
				matches, err := secbootVerifyPrimaryKeyDigest(disk.DevPath(), crypto.Hash(defaultHashAlg), salt, digest)
				if err != nil {
					if errors.Is(err, secboot.ErrKernelKeyNotFound) {
						break
					}
					return err
				}
				if !matches {
					primaryKeysMatch = false
				}
			}
		}
		if !primaryKeysMatch {
			logger.Noticef("WARNING: the primary keys of unlocked devices are not matching. Reprovision is required.")
			setRepairAttemptResult(st, &repairState{
				Result:          AutoRepairNotAttempted,
				Recommendations: []RecommendedRemedialAction{RecommendedRemedialActionRequireReprovision},
			})
			return nil
		}

		remedialActions := secbootShouldAttemptRepair(s, lockoutResetErr)
		if !remedialActions.AttemptRepair {
			var recommendations []RecommendedRemedialAction

			if remedialActions.RequireReprovision {
				recommendations = append(recommendations, RecommendedRemedialActionRequireReprovision)
			}
			if remedialActions.PermitManual {
				recommendations = append(recommendations, RecommendedRemedialActionPermitManual)
			}
			if remedialActions.RequirePlatformReset {
				recommendations = append(recommendations, RecommendedRemedialActionRequirePlatformReset)
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
