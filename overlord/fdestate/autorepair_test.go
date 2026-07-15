// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

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

package fdestate_test

import (
	"context"
	"fmt"
	"os"

	. "gopkg.in/check.v1"

	sb "github.com/snapcore/secboot"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type autoRepairSuite struct {
	fdeMgrSuite

	bootId string
}

var _ = Suite(&autoRepairSuite{})

func (s *autoRepairSuite) SetUpTest(c *C) {
	s.fdeMgrSuite.SetUpTest(c)

	s.bootId = "547730db-9e31-4c33-b418-1bce4e03f467"
	s.AddCleanup(fdestate.MockOsutilBootID(func() (string, error) {
		return s.bootId, nil
	}))
}

func (s *autoRepairSuite) mockPostInstallChecks(c *C) {
	recoveryBl := bootloadertest.Mock("recovery", "").WithTrustedAssets()
	recoveryBl.TrustedAssetsMap = map[string]string{
		"EFI/ubuntu/shim.efi": "ubuntu:shim",
		"EFI/ubuntu/grub.efi": "ubuntu:grub",
	}
	recoveryBl.KernelBootFileBuilder = func(kernelPath string) bootloader.BootFile {
		return bootloader.NewBootFile("some-kernel", "kernel.efi", bootloader.RoleRunMode)
	}
	recoveryBl.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "EFI/ubuntu/shim.efi", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "EFI/ubuntu/grub.efi", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "EFI/ubuntu/grub.efi", bootloader.RoleRunMode),
	}

	runBl := bootloadertest.Mock("run", "").WithExtractedRunKernelImage()
	runBl.SetEnabledKernel(&snap.Info{SuggestedName: "some-kernel", InstanceKey: "x1", SnapType: snap.TypeKernel})

	s.AddCleanup(fdestate.MockBootloaderFind(func(rootdir string, opts *bootloader.Options) (bootloader.Bootloader, error) {
		if opts.Role == bootloader.RoleRecovery {
			return recoveryBl, nil
		} else if opts.Role == bootloader.RoleRunMode {
			return runBl, nil
		} else {
			c.Errorf("unexpected")
			return nil, fmt.Errorf("unexpected")
		}
	}))

	s.AddCleanup(fdestate.MockBootReadModeenv(func(rootdir string) (*boot.Modeenv, error) {
		return &boot.Modeenv{
			CurrentTrustedBootAssets: map[string][]string{
				"ubuntu:grub": {
					"hash-grub-run",
				},
			},
			CurrentTrustedRecoveryBootAssets: map[string][]string{
				"ubuntu:shim": {
					"hash-shim-recovery",
				},
				"ubuntu:grub": {
					"hash-grub-recovery",
				},
			},
		}, nil
	}))

	s.AddCleanup(fdestate.MockSecbootPostinstallCheck(func(ctx context.Context, bootImageFiles []bootloader.BootFile) (*secboot.PreinstallCheckContext, []secboot.PreinstallErrorDetails, error) {
		return nil, nil, nil
	}))
}

func (s *autoRepairSuite) TestAttemptAutoRepairNeeded(c *C) {
	const onClassic = false
	s.startedManager(c, onClassic)

	s.st.Lock()
	defer s.st.Unlock()

	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	s.createUnlockedState(c, sb.ActivationSucceededWithPlatformKey)

	reprovisioned := 0
	defer fdestate.MockSecbootProvisionTPM(func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error {
		c.Check(mode, Equals, secboot.TPMPartialReprovision)
		reprovisioned += 1
		return nil
	})()

	defer fdestate.MockSecbootShouldAttemptRepair(func(as *secboot.ActivateState, lockoutResetErr error) secboot.RemedialActions {
		return secboot.RemedialActions{
			AttemptRepair: true,
		}
	})()

	s.mockBootAssetsStateForModeenv(c)

	resealed := 0
	defer fdestate.MockBackendResealKeyForBootChains(func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
		resealed += 1
		c.Check(params.Options.Force, Equals, true)
		return nil
	})()

	s.mockPostInstallChecks(c)

	const runPostInstallChecks = true
	err := fdestate.AttemptAutoRepairIfNeeded(s.st, nil, runPostInstallChecks)
	c.Assert(err, IsNil)

	c.Check(reprovisioned, Equals, 1)
	c.Check(resealed, Equals, 1)

	result, err := fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)

	c.Check(result.Result, Equals, fdestate.AutoRepairResult("success"))

	// Try again it should do nothing
	err = fdestate.AttemptAutoRepairIfNeeded(s.st, nil, runPostInstallChecks)
	c.Assert(err, IsNil)

	c.Check(reprovisioned, Equals, 1)
	c.Check(resealed, Equals, 1)

	// And keep the same result
	result, err = fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)

	c.Check(result.Result, Equals, fdestate.AutoRepairResult("success"))
}

func (s *autoRepairSuite) TestAttemptAutoRepairNotNeeded(c *C) {
	const onClassic = false
	s.startedManager(c, onClassic)

	s.st.Lock()
	defer s.st.Unlock()

	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	s.createUnlockedState(c, sb.ActivationSucceededWithPlatformKey)

	defer fdestate.MockSecbootProvisionTPM(func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error {
		c.Errorf("Unexpected call")
		return fmt.Errorf("Unexpected call")
	})()

	defer fdestate.MockSecbootShouldAttemptRepair(func(as *secboot.ActivateState, lockoutResetErr error) secboot.RemedialActions {
		return secboot.RemedialActions{}
	})()

	s.mockBootAssetsStateForModeenv(c)

	defer fdestate.MockBackendResealKeyForBootChains(func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
		c.Errorf("Unexpected call")
		return fmt.Errorf("Unexpected call")
	})()

	const runPostInstallChecks = true
	err := fdestate.AttemptAutoRepairIfNeeded(s.st, nil, runPostInstallChecks)
	c.Assert(err, IsNil)

	result, err := fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)

	c.Check(result.Result, Equals, fdestate.AutoRepairResult("not-attempted"))
}

func (s *autoRepairSuite) TestAttemptAutoRepairNeededBadReprovision(c *C) {
	const onClassic = false
	s.startedManager(c, onClassic)

	s.st.Lock()
	defer s.st.Unlock()

	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	s.createUnlockedState(c, sb.ActivationSucceededWithPlatformKey)

	reprovisioned := 0
	defer fdestate.MockSecbootProvisionTPM(func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error {
		reprovisioned += 1
		return fmt.Errorf("some error")
	})()

	defer fdestate.MockSecbootShouldAttemptRepair(func(as *secboot.ActivateState, lockoutResetErr error) secboot.RemedialActions {
		return secboot.RemedialActions{
			AttemptRepair: true,
		}
	})()

	s.mockBootAssetsStateForModeenv(c)

	defer fdestate.MockBackendResealKeyForBootChains(func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
		c.Errorf("Unexpected call")
		return fmt.Errorf("Unexpected call")
	})()

	s.mockPostInstallChecks(c)

	const runPostInstallChecks = true
	err := fdestate.AttemptAutoRepairIfNeeded(s.st, nil, runPostInstallChecks)
	c.Assert(err, IsNil)

	c.Check(reprovisioned, Equals, 1)

	result, err := fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)

	c.Check(result.Result, Equals, fdestate.AutoRepairResult("failed-platform-init"))
}

func (s *autoRepairSuite) TestAttemptAutoRepairErrorNoActivateState(c *C) {
	const onClassic = false
	s.startedManager(c, onClassic)

	s.st.Lock()
	defer s.st.Unlock()

	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	defer fdestate.MockBootLoadDiskUnlockState(func(name string) (*boot.DiskUnlockState, error) {
		c.Check(name, Equals, "unlocked.json")
		return &boot.DiskUnlockState{}, nil
	})()

	logbuf, restore := logger.MockLogger()
	defer restore()

	const runPostInstallChecks = true
	err := fdestate.AttemptAutoRepairIfNeeded(s.st, nil, runPostInstallChecks)
	c.Assert(err, IsNil)

	result, err := fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)

	c.Check(result.Result, Equals, fdestate.AutoRepairResult("not-attempted"))

	c.Check(logbuf.String(), testutil.Contains, `WARNING: the system booted with an old initrd without using activation API`)
}

func (s *autoRepairSuite) TestAttemptAutoRepairErrorNoActivateStateRecovery(c *C) {
	const onClassic = false
	s.startedManager(c, onClassic)

	s.st.Lock()
	defer s.st.Unlock()

	reprovisioned := 0
	defer fdestate.MockSecbootProvisionTPM(func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error {
		c.Check(mode, Equals, secboot.TPMPartialReprovision)
		reprovisioned += 1
		return nil
	})()

	defer fdestate.MockSecbootShouldAttemptRepair(func(as *secboot.ActivateState, lockoutResetErr error) secboot.RemedialActions {
		return secboot.RemedialActions{
			AttemptRepair: true,
		}
	})()

	s.mockBootAssetsStateForModeenv(c)

	resealed := 0
	defer fdestate.MockBackendResealKeyForBootChains(func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
		resealed += 1
		c.Check(params.Options.Force, Equals, true)
		return nil
	})()

	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	defer fdestate.MockBootLoadDiskUnlockState(func(name string) (*boot.DiskUnlockState, error) {
		c.Check(name, Equals, "unlocked.json")
		return &boot.DiskUnlockState{
			UbuntuData: boot.PartitionState{
				UnlockKey: boot.KeyRecovery,
			},
		}, nil
	})()

	s.mockPostInstallChecks(c)

	logbuf, restore := logger.MockLogger()
	defer restore()

	const runPostInstallChecks = true
	err := fdestate.AttemptAutoRepairIfNeeded(s.st, nil, runPostInstallChecks)
	c.Assert(err, IsNil)

	result, err := fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)

	c.Check(result.Result, Equals, fdestate.AutoRepairResult("success"))

	c.Check(logbuf.String(), testutil.Contains, `WARNING: the system booted with an old initrd without using activation API`)

	c.Check(reprovisioned, Equals, 1)
	c.Check(resealed, Equals, 1)
}

func (s *autoRepairSuite) TestAttemptAutoRepairErrorActivateState(c *C) {
	const onClassic = false
	s.startedManager(c, onClassic)

	s.st.Lock()
	defer s.st.Unlock()

	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	defer fdestate.MockBootLoadDiskUnlockState(func(name string) (*boot.DiskUnlockState, error) {
		c.Check(name, Equals, "unlocked.json")
		return nil, fmt.Errorf("cannot read state")
	})()

	logbuf, restore := logger.MockLogger()
	defer restore()

	const runPostInstallChecks = true
	err := fdestate.AttemptAutoRepairIfNeeded(s.st, nil, runPostInstallChecks)
	c.Assert(err, IsNil)

	result, err := fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)

	c.Check(result.Result, Equals, fdestate.AutoRepairResult("not-attempted"))

	c.Check(logbuf.String(), testutil.Contains, `WARNING: error while getting activation state: cannot read state`)
}

func (s *autoRepairSuite) TestAttemptAutoRepairErrorNoFileActivateState(c *C) {
	const onClassic = false
	s.startedManager(c, onClassic)

	s.st.Lock()
	defer s.st.Unlock()

	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	defer fdestate.MockBootLoadDiskUnlockState(func(name string) (*boot.DiskUnlockState, error) {
		c.Check(name, Equals, "unlocked.json")
		return nil, os.ErrNotExist
	})()

	logbuf, restore := logger.MockLogger()
	defer restore()

	const runPostInstallChecks = true
	err := fdestate.AttemptAutoRepairIfNeeded(s.st, nil, runPostInstallChecks)
	c.Assert(err, IsNil)

	result, err := fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)

	c.Check(result.Result, Equals, fdestate.AutoRepairResult("not-attempted"))

	c.Check(logbuf.String(), testutil.Contains, `WARNING: the system booted with an old initrd without unlocked status reporting`)
}

func (s *autoRepairSuite) TestAttemptAutoRepairNeededBadReseal(c *C) {
	const onClassic = false
	s.startedManager(c, onClassic)

	s.st.Lock()
	defer s.st.Unlock()

	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	s.createUnlockedState(c, sb.ActivationSucceededWithPlatformKey)

	reprovisioned := 0
	defer fdestate.MockSecbootProvisionTPM(func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error {
		c.Check(mode, Equals, secboot.TPMPartialReprovision)
		reprovisioned += 1
		return nil
	})()

	defer fdestate.MockSecbootShouldAttemptRepair(func(as *secboot.ActivateState, lockoutResetErr error) secboot.RemedialActions {
		return secboot.RemedialActions{
			AttemptRepair: true,
		}
	})()

	s.mockBootAssetsStateForModeenv(c)

	resealed := 0
	defer fdestate.MockBackendResealKeyForBootChains(func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
		resealed += 1
		c.Check(params.Options.Force, Equals, true)
		return fmt.Errorf("some error")
	})()

	s.mockPostInstallChecks(c)

	const runPostInstallChecks = true
	err := fdestate.AttemptAutoRepairIfNeeded(s.st, nil, runPostInstallChecks)
	c.Assert(err, IsNil)

	c.Check(reprovisioned, Equals, 1)
	c.Check(resealed, Equals, 1)

	result, err := fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)

	c.Check(result.Result, Equals, fdestate.AutoRepairResult("failed-keyslots"))
}

func (s *autoRepairSuite) TestIgnoreOldAutoRepairResult(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	err := fdestate.SetRepairAttemptResult(s.st, &fdestate.RepairState{Result: fdestate.AutoRepairFailedPlatformInit})
	c.Assert(err, IsNil)

	result, err := fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)
	c.Check(result.Result, Equals, fdestate.AutoRepairResult("failed-platform-init"))

	s.bootId = "e6784d27-c31b-4d1f-be44-582702fd6250"
	result, err = fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)
	c.Check(result, IsNil)

	err = fdestate.SetRepairAttemptResult(s.st, &fdestate.RepairState{Result: fdestate.AutoRepairFailedPlatformInit})
	c.Assert(err, IsNil)

	result, err = fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)
	c.Check(result.Result, Equals, fdestate.AutoRepairResult("failed-platform-init"))
}

func (s *autoRepairSuite) TestAttemptAutoRepairFailedPostinstallChecks(c *C) {
	const onClassic = false
	s.startedManager(c, onClassic)

	s.st.Lock()
	defer s.st.Unlock()

	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	s.createUnlockedState(c, sb.ActivationSucceededWithPlatformKey)

	defer fdestate.MockSecbootProvisionTPM(func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error {
		c.Errorf("unexpected call")
		return fmt.Errorf("unexpected call")
	})()

	defer fdestate.MockSecbootShouldAttemptRepair(func(as *secboot.ActivateState, lockoutResetErr error) secboot.RemedialActions {
		return secboot.RemedialActions{
			AttemptRepair: true,
		}
	})()

	s.mockBootAssetsStateForModeenv(c)

	defer fdestate.MockBackendResealKeyForBootChains(func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
		c.Errorf("unexpected call")
		return fmt.Errorf("unexpected call")
	})()

	s.mockPostInstallChecks(c)
	defer fdestate.MockSecbootPostinstallCheck(func(ctx context.Context, bootImageFiles []bootloader.BootFile) (*secboot.PreinstallCheckContext, []secboot.PreinstallErrorDetails, error) {
		return nil, nil, fmt.Errorf("some error")
	})()

	logbuf, restore := logger.MockLogger()
	defer restore()

	const runPostInstallChecks = true
	err := fdestate.AttemptAutoRepairIfNeeded(s.st, nil, runPostInstallChecks)
	c.Assert(err, IsNil)

	result, err := fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)

	c.Check(result.Result, Equals, fdestate.AutoRepairResult("failed-encryption-support"))

	c.Check(logbuf.String(), testutil.Contains, `WARNING: could not auto repair keyslots due to failed platform initialization: some error`)
}

func (s *autoRepairSuite) TestAttemptAutoRepairFailedPostinstallChecksWithDetails(c *C) {
	const onClassic = false
	s.startedManager(c, onClassic)

	s.st.Lock()
	defer s.st.Unlock()

	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	s.createUnlockedState(c, sb.ActivationSucceededWithPlatformKey)

	defer fdestate.MockSecbootProvisionTPM(func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error {
		c.Errorf("unexpected call")
		return fmt.Errorf("unexpected call")
	})()

	defer fdestate.MockSecbootShouldAttemptRepair(func(as *secboot.ActivateState, lockoutResetErr error) secboot.RemedialActions {
		return secboot.RemedialActions{
			AttemptRepair: true,
		}
	})()

	s.mockBootAssetsStateForModeenv(c)

	defer fdestate.MockBackendResealKeyForBootChains(func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
		c.Errorf("unexpected call")
		return fmt.Errorf("unexpected call")
	})()

	s.mockPostInstallChecks(c)
	defer fdestate.MockSecbootPostinstallCheck(func(ctx context.Context, bootImageFiles []bootloader.BootFile) (*secboot.PreinstallCheckContext, []secboot.PreinstallErrorDetails, error) {
		var details = []secboot.PreinstallErrorDetails{
			{
				Kind:    "kind-1",
				Message: "error-1",
			},
			{
				Kind:    "kind-2",
				Message: "error-2",
			},
		}

		return nil, details, nil
	})()

	logbuf, restore := logger.MockLogger()
	defer restore()

	const runPostInstallChecks = true
	err := fdestate.AttemptAutoRepairIfNeeded(s.st, nil, runPostInstallChecks)
	c.Assert(err, IsNil)

	result, err := fdestate.GetRepairAttemptResult(s.st)
	c.Assert(err, IsNil)

	c.Check(result.Result, Equals, fdestate.AutoRepairResult("failed-encryption-support"))

	c.Check(logbuf.String(), testutil.Contains, "WARNING: could not auto repair keyslots due to failed platform initialization:\n- error-1\n- error-2\n")
}
