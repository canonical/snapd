// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package boot_test

import (
	"fmt"
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

type baseSystemsSuite struct {
	baseBootenvSuite
}

func (s *baseSystemsSuite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)
	c.Assert(os.MkdirAll(boot.InitramfsUbuntuBootDir, 0755), IsNil)
	c.Assert(os.MkdirAll(boot.InitramfsUbuntuSeedDir, 0755), IsNil)
}

type systemsSuite struct {
	baseSystemsSuite

	uc20dev snap.Device

	runKernelBf      bootloader.BootFile
	recoveryKernelBf bootloader.BootFile
	seedKernelSnap   *seed.Snap
	seedGadgetSnap   *seed.Snap
}

var _ = Suite(&systemsSuite{})

func (s *systemsSuite) mockTrustedBootloaderWithAssetAndChains(c *C, runKernelBf, recoveryKernelBf bootloader.BootFile) *bootloadertest.MockTrustedAssetsBootloader {
	mockAssetsCache(c, s.rootdir, "trusted", []string{
		"asset-asset-hash-1",
	})

	mtbl := bootloadertest.Mock("trusted", s.bootdir).WithTrustedAssets()
	mtbl.TrustedAssetsMap = map[string]string{"asset": "asset"}
	mtbl.StaticCommandLine = "static cmdline"
	mtbl.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "asset", bootloader.RoleRunMode),
		runKernelBf,
	}
	mtbl.RecoveryBootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "asset", bootloader.RoleRecovery),
		recoveryKernelBf,
	}
	bootloader.Force(mtbl)
	return mtbl
}

func (s *systemsSuite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)

	restore := boot.MockResealKeyForBootChains(func(method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
		return nil
	})
	s.AddCleanup(restore)

	s.uc20dev = boottest.MockUC20Device("", nil)

	// run kernel
	s.runKernelBf = bootloader.NewBootFile("/var/lib/snapd/snap/pc-kernel_500.snap",
		"kernel.efi", bootloader.RoleRunMode)
	// seed (recovery) kernel
	s.recoveryKernelBf = bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap",
		"kernel.efi", bootloader.RoleRecovery)

	s.seedKernelSnap = mockKernelSeedSnap(snap.R(1))
	s.seedGadgetSnap = mockGadgetSeedSnap(c, nil)
}

func (s *systemsSuite) TestSetTryRecoverySystemEncrypted(c *C) {
	mockAssetsCache(c, s.rootdir, "trusted", []string{
		"asset-asset-hash-1",
	})

	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	bootloader.Force(mtbl)
	defer bootloader.Force(nil)

	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)

	model := s.uc20dev.Model()

	modeenv := &boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825"},
		GoodRecoverySystems:    []string{"20200825"},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},
		CurrentKernels: []string{"pc-kernel_500.snap"},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	c.Assert(modeenv.WriteTo(""), IsNil)

	var readSeedSeenLabels []string
	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		// the mock bootloader can only mock a single recovery boot
		// chain, so pretend both seeds use the same kernel, but keep track of the labels
		readSeedSeenLabels = append(readSeedSeenLabels, label)
		return model, []*seed.Snap{s.seedKernelSnap, s.seedGadgetSnap}, nil
	})
	defer restore()

	resealCalls := 0
	restore = boot.MockResealKeyForBootChains(func(method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
		resealCalls++
		// bootloader variables have already been modified
		c.Check(mtbl.SetBootVarsCalls, Equals, 1)
		switch resealCalls {
		case 1:
			c.Assert(params.RunModeBootChains, HasLen, 1)
			bootChain := params.RunModeBootChains[0]
			c.Check(bootChain.KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=run static cmdline",
			})

			c.Assert(params.RecoveryBootChainsForRunKey, HasLen, 2)
			recoveryRunBootChain := params.RecoveryBootChainsForRunKey[0]
			c.Check(recoveryRunBootChain.KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 static cmdline",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 static cmdline",
			})
			tryRecoveryRunBootChain := params.RecoveryBootChainsForRunKey[1]
			c.Check(tryRecoveryRunBootChain.KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=1234 static cmdline",
			})

			c.Assert(params.RecoveryBootChains, HasLen, 1)
			recoveryBootChain := params.RecoveryBootChains[0]
			c.Check(recoveryBootChain.KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 static cmdline",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 static cmdline",
			})
			return nil
		default:
			c.Errorf("unexpected call to ResealKeyForBootChains with count %v", resealCalls)
			return fmt.Errorf("unexpected call")
		}
	})
	defer restore()

	err := boot.SetTryRecoverySystem(s.uc20dev, "1234")
	c.Assert(err, IsNil)

	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"try_recovery_system":    "1234",
		"recovery_system_status": "try",
	})
	c.Check(resealCalls, Equals, 1)
	c.Check(readSeedSeenLabels, DeepEquals, []string{
		"20200825", "1234", // current recovery systems for run key
		"20200825", // good recovery systems for recovery keys
	})

	modeenvRead, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvRead.DeepEqual(&boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825", "1234"},
		GoodRecoverySystems:    []string{"20200825"},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},
		CurrentKernels: []string{"pc-kernel_500.snap"},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}), Equals, true)
}

func (s *systemsSuite) TestSetTryRecoverySystemRemodelEncrypted(c *C) {
	mockAssetsCache(c, s.rootdir, "trusted", []string{
		"asset-asset-hash-1",
	})

	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	bootloader.Force(mtbl)
	defer bootloader.Force(nil)

	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)

	model := s.uc20dev.Model()
	newModel := boottest.MakeMockUC20Model(map[string]interface{}{
		"model": "my-new-model",
	})

	modeenv := &boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825"},
		GoodRecoverySystems:    []string{"20200825"},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},
		CurrentKernels: []string{"pc-kernel_500.snap"},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	c.Assert(modeenv.WriteTo(""), IsNil)

	var readSeedSeenLabels []string
	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		// the mock bootloader can only mock a single recovery boot
		// chain, so pretend both seeds use the same kernel, but keep track of the labels
		readSeedSeenLabels = append(readSeedSeenLabels, label)
		systemModel := model
		if label == "1234" {
			systemModel = newModel
		}
		return systemModel, []*seed.Snap{s.seedKernelSnap, s.seedGadgetSnap}, nil
	})
	defer restore()

	resealCalls := 0
	restore = boot.MockResealKeyForBootChains(func(method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
		resealCalls++
		// bootloader variables have already been modified
		c.Check(mtbl.SetBootVarsCalls, Equals, 1)
		switch resealCalls {
		case 1:
			c.Assert(params.RunModeBootChains, HasLen, 2)
			bootChain := params.RunModeBootChains[0]
			c.Check(bootChain.KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=run static cmdline",
			})
			tryBootChain := params.RunModeBootChains[1]
			c.Check(tryBootChain.KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=run static cmdline",
			})

			c.Assert(params.RecoveryBootChainsForRunKey, HasLen, 2)
			recoveryRunBootChain := params.RecoveryBootChainsForRunKey[0]
			c.Check(recoveryRunBootChain.KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 static cmdline",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 static cmdline",
			})
			tryRecoveryRunBootChain := params.RecoveryBootChainsForRunKey[1]
			c.Check(tryRecoveryRunBootChain.KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=1234 static cmdline",
			})

			c.Assert(params.RecoveryBootChains, HasLen, 1)
			recoveryBootChain := params.RecoveryBootChains[0]
			c.Check(recoveryBootChain.KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 static cmdline",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 static cmdline",
			})
		default:
			c.Errorf("unexpected call to ResealKeyForBootChains with count %v", resealCalls)
			return fmt.Errorf("unexpected call")
		}
		return nil
	})
	defer restore()

	// a remodel will pass the new device
	newUC20Device := boottest.MockUC20Device("run", newModel)
	err := boot.SetTryRecoverySystem(newUC20Device, "1234")
	c.Assert(err, IsNil)

	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"try_recovery_system":    "1234",
		"recovery_system_status": "try",
	})
	// run and recovery keys
	c.Check(resealCalls, Equals, 1)
	c.Check(readSeedSeenLabels, DeepEquals, []string{
		"20200825", "1234", // current recovery systems for run key and current model from modeenv
		"20200825", "1234", // current recovery systems for run key and try model from modeenv
		"20200825", // good recovery systems for recovery keys
	})

	modeenvRead, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvRead, testutil.JsonEquals, boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825", "1234"},
		GoodRecoverySystems:    []string{"20200825"},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},
		CurrentKernels: []string{"pc-kernel_500.snap"},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),

		TryModel:          newModel.Model(),
		TryBrandID:        newModel.BrandID(),
		TryGrade:          string(newModel.Grade()),
		TryModelSignKeyID: newModel.SignKeyID(),
	})
}

func (s *systemsSuite) TestSetTryRecoverySystemSimple(c *C) {
	mtbl := bootloadertest.Mock("trusted", s.bootdir).WithTrustedAssets()
	bootloader.Force(mtbl)
	defer bootloader.Force(nil)

	model := s.uc20dev.Model()
	modeenv := &boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825"},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	c.Assert(modeenv.WriteTo(""), IsNil)

	restore := boot.MockResealKeyForBootChains(func(method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
		return fmt.Errorf("unexpected call")
	})
	s.AddCleanup(restore)

	err := boot.SetTryRecoverySystem(s.uc20dev, "1234")
	c.Assert(err, IsNil)

	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"try_recovery_system":    "1234",
		"recovery_system_status": "try",
	})

	modeenvRead, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvRead, testutil.JsonEquals, boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825", "1234"},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	})
}

func (s *systemsSuite) TestSetTryRecoverySystemSetBootVarsErr(c *C) {
	mtbl := bootloadertest.Mock("trusted", s.bootdir).WithTrustedAssets()
	bootloader.Force(mtbl)
	defer bootloader.Force(nil)

	modeenv := &boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825"},
	}
	c.Assert(modeenv.WriteTo(""), IsNil)

	restore := boot.MockResealKeyForBootChains(func(method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
		return fmt.Errorf("unexpected call")
	})
	s.AddCleanup(restore)

	mtbl.BootVars = map[string]string{
		"try_recovery_system":    "mock",
		"recovery_system_status": "mock",
	}
	mtbl.SetErrFunc = func() error {
		switch mtbl.SetBootVarsCalls {
		case 1:
			return fmt.Errorf("set boot vars fails")
		case 2:
			// called during cleanup
			return nil
		default:
			return fmt.Errorf("unexpected call")
		}
	}

	err := boot.SetTryRecoverySystem(s.uc20dev, "1234")
	c.Assert(err, ErrorMatches, "set boot vars fails")

	// cleared
	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})

	modeenvRead, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// modeenv is unchanged
	c.Check(modeenvRead.DeepEqual(modeenv), Equals, true)
}

func (s *systemsSuite) TestSetTryRecoverySystemCleanupOnErrorBeforeReseal(c *C) {
	mockAssetsCache(c, s.rootdir, "trusted", []string{
		"asset-asset-hash-1",
	})

	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	defer bootloader.Force(nil)

	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)

	model := s.uc20dev.Model()

	modeenv := &boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825"},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	c.Assert(modeenv.WriteTo(""), IsNil)

	readSeedCalls := 0
	cleanupTriggered := false
	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		readSeedCalls++
		// this is the reseal cleanup path
		switch readSeedCalls {
		case 1:
			// called for the first system
			c.Assert(label, Equals, "20200825")
			c.Check(mtbl.SetBootVarsCalls, Equals, 1)
			return model, []*seed.Snap{s.seedKernelSnap, s.seedGadgetSnap}, nil
		case 2:
			// called for the 'try' system
			c.Assert(label, Equals, "1234")
			// modeenv is updated first
			modeenvRead, err := boot.ReadModeenv("")
			c.Assert(err, IsNil)
			c.Check(modeenvRead.CurrentRecoverySystems, DeepEquals, []string{
				"20200825", "1234",
			})
			c.Check(mtbl.SetBootVarsCalls, Equals, 1)
			// we are triggering the cleanup by returning an error now
			cleanupTriggered = true
			return nil, nil, fmt.Errorf("seed read essential fails")
		case 3:
			// (cleanup) recovery boot chains for run key, called
			// for the first system only
			fallthrough
		case 4:
			// (cleanup) recovery boot chains for recovery keys
			c.Assert(label, Equals, "20200825")
			// boot variables already updated
			c.Check(mtbl.SetBootVarsCalls, Equals, 2)
			return model, []*seed.Snap{s.seedKernelSnap, s.seedGadgetSnap}, nil
		default:
			return nil, nil, fmt.Errorf("unexpected call %v", readSeedCalls)
		}
	})
	defer restore()

	resealCalls := 0
	restore = boot.MockResealKeyForBootChains(func(method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
		resealCalls++
		if cleanupTriggered {
			return nil
		}
		return fmt.Errorf("unexpected call")
	})
	defer restore()

	err := boot.SetTryRecoverySystem(s.uc20dev, "1234")
	c.Assert(err, ErrorMatches, ".*: seed read essential fails")

	// failed after the call to read the 'try' system seed
	c.Check(readSeedCalls, Equals, 4)
	// called twice during cleanup for run and recovery keys
	c.Check(resealCalls, Equals, 1)

	modeenvRead, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// modeenv is unchanged
	c.Check(modeenvRead.DeepEqual(modeenv), Equals, true)
	// bootloader variables have been cleared
	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
}

func (s *systemsSuite) TestSetTryRecoverySystemCleanupOnErrorAfterReseal(c *C) {
	mockAssetsCache(c, s.rootdir, "trusted", []string{
		"asset-asset-hash-1",
	})

	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	bootloader.Force(mtbl)
	defer bootloader.Force(nil)

	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)

	model := s.uc20dev.Model()

	modeenv := &boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825"},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	c.Assert(modeenv.WriteTo(""), IsNil)

	readSeedCalls := 0
	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		readSeedCalls++
		// this is the reseal cleanup path

		switch readSeedCalls {
		case 1:
			// called for the first system
			c.Assert(label, Equals, "20200825")
			c.Check(mtbl.SetBootVarsCalls, Equals, 1)
			return model, []*seed.Snap{s.seedKernelSnap, s.seedGadgetSnap}, nil
		case 2:
			// called for the 'try' system
			c.Assert(label, Equals, "1234")
			// modeenv is updated first
			modeenvRead, err := boot.ReadModeenv("")
			c.Assert(err, IsNil)
			c.Check(modeenvRead.CurrentRecoverySystems, DeepEquals, []string{
				"20200825", "1234",
			})
			c.Check(mtbl.SetBootVarsCalls, Equals, 1)
			// still good
			return model, []*seed.Snap{s.seedKernelSnap, s.seedGadgetSnap}, nil
		case 3:
			// recovery boot chains for a good recovery system
			c.Check(mtbl.SetBootVarsCalls, Equals, 1)
			fallthrough
		case 4:
			// (cleanup) recovery boot chains for run key, called
			// for the first system only
			fallthrough
		case 5:
			// (cleanup) recovery boot chains for recovery keys
			c.Assert(label, Equals, "20200825")
			// boot variables already updated
			if readSeedCalls >= 4 {
				c.Check(mtbl.SetBootVarsCalls, Equals, 2)
			}
			return model, []*seed.Snap{s.seedKernelSnap, s.seedGadgetSnap}, nil
		default:
			return nil, nil, fmt.Errorf("unexpected call %v", readSeedCalls)
		}
	})
	defer restore()

	resealCalls := 0
	restore = boot.MockResealKeyForBootChains(func(method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
		resealCalls++
		switch resealCalls {
		case 1:
			return fmt.Errorf("reseal fails")
		case 2:
			return nil
		default:
			return fmt.Errorf("unexpected call")

		}
	})
	defer restore()

	err := boot.SetTryRecoverySystem(s.uc20dev, "1234")
	c.Assert(err, ErrorMatches, "reseal fails")

	// failed after the call to read the 'try' system seed
	c.Check(readSeedCalls, Equals, 5)
	// called 3 times, once when mocked failure occurs, twice during cleanup
	// for run and recovery keys
	c.Check(resealCalls, Equals, 2)

	modeenvRead, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// modeenv is unchanged
	c.Check(modeenvRead.DeepEqual(modeenv), Equals, true)
	// bootloader variables have been cleared
	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
}

func (s *systemsSuite) TestSetTryRecoverySystemCleanupError(c *C) {
	mockAssetsCache(c, s.rootdir, "trusted", []string{
		"asset-asset-hash-1",
	})

	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	bootloader.Force(mtbl)
	defer bootloader.Force(nil)

	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)

	model := s.uc20dev.Model()
	modeenv := &boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825"},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	c.Assert(modeenv.WriteTo(""), IsNil)

	readSeedCalls := 0
	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		readSeedCalls++
		// this is the reseal cleanup path
		c.Logf("call %v label %v", readSeedCalls, label)
		switch readSeedCalls {
		case 1:
			// called for the first system
			c.Assert(label, Equals, "20200825")
			return s.uc20dev.Model(), []*seed.Snap{s.seedKernelSnap, s.seedGadgetSnap}, nil
		case 2:
			// called for the 'try' system
			c.Assert(label, Equals, "1234")
			// still good
			return s.uc20dev.Model(), []*seed.Snap{s.seedKernelSnap, s.seedGadgetSnap}, nil
		case 3:
			// recovery boot chains for a good recovery system
			fallthrough
		case 4:
			// (cleanup) recovery boot chains for run key, called
			// for the first system only
			fallthrough
		case 5:
			// (cleanup) recovery boot chains for recovery keys
			c.Check(label, Equals, "20200825")
			return s.uc20dev.Model(), []*seed.Snap{s.seedKernelSnap, s.seedGadgetSnap}, nil
		default:
			return nil, nil, fmt.Errorf("unexpected call %v", readSeedCalls)
		}
	})
	defer restore()

	resealCalls := 0
	restore = boot.MockResealKeyForBootChains(func(method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
		resealCalls++
		switch resealCalls {
		case 1:
			return fmt.Errorf("reseal fails")
		case 2:
			return fmt.Errorf("reseal in cleanup fails too")
		default:
			return fmt.Errorf("unexpected call")

		}
	})
	defer restore()

	err := boot.SetTryRecoverySystem(s.uc20dev, "1234")
	c.Assert(err, ErrorMatches, `reseal fails \(cleanup failed: reseal in cleanup fails too\)`)

	// failed after the call to read the 'try' system seed
	c.Check(readSeedCalls, Equals, 5)
	// called twice, once when enabling the try system, once on cleanup
	c.Check(resealCalls, Equals, 2)

	modeenvRead, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// modeenv is unchanged
	c.Check(modeenvRead.DeepEqual(modeenv), Equals, true)
	// bootloader variables have been cleared regardless of reseal failing
	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
}

func (s *systemsSuite) testInspectRecoverySystemOutcomeHappy(c *C, mtbl *bootloadertest.MockTrustedAssetsBootloader, expectedOutcome boot.TryRecoverySystemOutcome, expectedErr string) {
	bootloader.Force(mtbl)
	defer bootloader.Force(nil)

	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)

	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		return nil, nil, fmt.Errorf("unexpected call")
	})
	defer restore()

	restore = boot.MockResealKeyForBootChains(func(method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
		return fmt.Errorf("unexpected call")
	})
	defer restore()

	outcome, label, err := boot.InspectTryRecoverySystemOutcome(s.uc20dev)
	if expectedErr == "" {
		c.Assert(err, IsNil)
	} else {
		c.Assert(err, ErrorMatches, expectedErr)
	}
	c.Check(outcome, Equals, expectedOutcome)
	switch outcome {
	case boot.TryRecoverySystemOutcomeSuccess, boot.TryRecoverySystemOutcomeFailure:
		c.Check(label, Equals, "1234")
	default:
		c.Check(label, Equals, "")
	}
}

func (s *systemsSuite) TestInspectRecoverySystemOutcomeHappySuccess(c *C) {
	triedVars := map[string]string{
		"recovery_system_status": "tried",
		"try_recovery_system":    "1234",
	}
	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	err := mtbl.SetBootVars(triedVars)
	c.Assert(err, IsNil)

	m := boot.Modeenv{
		Mode: boot.ModeRun,
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"29112019", "1234"},
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)

	s.testInspectRecoverySystemOutcomeHappy(c, mtbl, boot.TryRecoverySystemOutcomeSuccess, "")

	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, triedVars)
}

func (s *systemsSuite) TestInspectRecoverySystemOutcomeFailureMissingSystemInModeenv(c *C) {
	triedVars := map[string]string{
		"recovery_system_status": "tried",
		"try_recovery_system":    "1234",
	}
	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	err := mtbl.SetBootVars(triedVars)
	c.Assert(err, IsNil)

	m := boot.Modeenv{
		Mode: boot.ModeRun,
		// we don't have the tried recovery system in the modeenv
		CurrentRecoverySystems: []string{"29112019"},
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)

	s.testInspectRecoverySystemOutcomeHappy(c, mtbl, boot.TryRecoverySystemOutcomeFailure, `recovery system "1234" was tried, but is not present in the modeenv CurrentRecoverySystems`)

	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, triedVars)
}

func (s *systemsSuite) TestInspectRecoverySystemOutcomeHappyFailure(c *C) {
	tryVars := map[string]string{
		"recovery_system_status": "try",
		"try_recovery_system":    "1234",
	}
	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	err := mtbl.SetBootVars(tryVars)
	c.Assert(err, IsNil)
	s.testInspectRecoverySystemOutcomeHappy(c, mtbl, boot.TryRecoverySystemOutcomeFailure, "")

	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, tryVars)
}

func (s *systemsSuite) TestInspectRecoverySystemOutcomeNotTried(c *C) {
	notTriedVars := map[string]string{
		"recovery_system_status": "",
		"try_recovery_system":    "",
	}
	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	err := mtbl.SetBootVars(notTriedVars)
	c.Assert(err, IsNil)
	s.testInspectRecoverySystemOutcomeHappy(c, mtbl, boot.TryRecoverySystemOutcomeNoneTried, "")
}

func (s *systemsSuite) TestInspectRecoverySystemOutcomeInconsistentBogusStatus(c *C) {
	badVars := map[string]string{
		"recovery_system_status": "foo",
		"try_recovery_system":    "1234",
	}
	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	err := mtbl.SetBootVars(badVars)
	c.Assert(err, IsNil)
	s.testInspectRecoverySystemOutcomeHappy(c, mtbl, boot.TryRecoverySystemOutcomeInconsistent, `unexpected recovery system status "foo"`)
	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, badVars)
}

func (s *systemsSuite) TestInspectRecoverySystemOutcomeInconsistentBadLabel(c *C) {
	badVars := map[string]string{
		"recovery_system_status": "tried",
		"try_recovery_system":    "",
	}
	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	err := mtbl.SetBootVars(badVars)
	c.Assert(err, IsNil)
	s.testInspectRecoverySystemOutcomeHappy(c, mtbl, boot.TryRecoverySystemOutcomeInconsistent, `try recovery system is unset but status is "tried"`)
	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, badVars)
}

func (s *systemsSuite) TestInspectRecoverySystemOutcomeInconsistentUnexpectedLabel(c *C) {
	badVars := map[string]string{
		"recovery_system_status": "",
		"try_recovery_system":    "1234",
	}
	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	err := mtbl.SetBootVars(badVars)
	c.Assert(err, IsNil)
	s.testInspectRecoverySystemOutcomeHappy(c, mtbl, boot.TryRecoverySystemOutcomeInconsistent, `unexpected recovery system status ""`)
	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, badVars)
}

type clearRecoverySystemTestCase struct {
	systemLabel string
	tryModel    *asserts.Model
	resealErr   error
	expectedErr string
}

func (s *systemsSuite) testClearRecoverySystem(c *C, mtbl *bootloadertest.MockTrustedAssetsBootloader, tc clearRecoverySystemTestCase) {
	mockAssetsCache(c, s.rootdir, "trusted", []string{
		"asset-asset-hash-1",
	})

	bootloader.Force(mtbl)
	defer bootloader.Force(nil)

	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)

	model := s.uc20dev.Model()

	modeenv := &boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825"},
		GoodRecoverySystems:    []string{"20200825"},
		CurrentKernels:         []string{},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	if tc.systemLabel != "" {
		modeenv.CurrentRecoverySystems = append(modeenv.CurrentRecoverySystems, tc.systemLabel)
	}
	if tc.tryModel != nil {
		modeenv.TryModel = tc.tryModel.Model()
		modeenv.TryBrandID = tc.tryModel.BrandID()
		modeenv.TryGrade = string(tc.tryModel.Grade())
		modeenv.TryModelSignKeyID = tc.tryModel.SignKeyID()
	}
	c.Assert(modeenv.WriteTo(""), IsNil)

	var readSeedSeenLabels []string
	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		// the mock bootloader can only mock a single recovery boot
		// chain, so pretend both seeds use the same kernel, but keep track of the labels
		readSeedSeenLabels = append(readSeedSeenLabels, label)
		return model, []*seed.Snap{s.seedKernelSnap, s.seedGadgetSnap}, nil
	})
	defer restore()

	resealCalls := 0
	restore = boot.MockResealKeyForBootChains(func(method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
		resealCalls++
		switch resealCalls {
		case 1:
			c.Assert(params.RecoveryBootChainsForRunKey, HasLen, 1)
			recoveryRunBootChain := params.RecoveryBootChainsForRunKey[0]
			c.Check(recoveryRunBootChain.KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 static cmdline",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 static cmdline",
			})

			c.Assert(params.RecoveryBootChains, HasLen, 1)
			recoveryBootChain := params.RecoveryBootChains[0]
			c.Check(recoveryBootChain.KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 static cmdline",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 static cmdline",
			})
			return tc.resealErr
		default:
			c.Errorf("unexpected call to ResealKeyForBootChains with count %v", resealCalls)
			return fmt.Errorf("unexpected call")
		}
	})
	defer restore()

	err := boot.ClearTryRecoverySystem(s.uc20dev, tc.systemLabel)
	if tc.expectedErr == "" {
		c.Assert(err, IsNil)
	} else {
		c.Assert(err, ErrorMatches, tc.expectedErr)
	}

	// only one seed system accessed
	c.Check(readSeedSeenLabels, DeepEquals, []string{"20200825", "20200825"})
	c.Check(resealCalls, Equals, 1)

	modeenvRead, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// modeenv systems list has one entry only
	c.Check(modeenvRead, testutil.JsonEquals, boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825"},
		GoodRecoverySystems:    []string{"20200825"},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),

		// try model if set, has been cleared
	})
}

func (s *systemsSuite) TestClearRecoverySystemHappy(c *C) {
	setVars := map[string]string{
		"recovery_system_status": "try",
		"try_recovery_system":    "1234",
	}
	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	err := mtbl.SetBootVars(setVars)
	c.Assert(err, IsNil)

	s.testClearRecoverySystem(c, mtbl, clearRecoverySystemTestCase{systemLabel: "1234"})
	// bootloader variables have been cleared
	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
}

func (s *systemsSuite) TestClearRecoverySystemTriedHappy(c *C) {
	setVars := map[string]string{
		"recovery_system_status": "tried",
		"try_recovery_system":    "1234",
	}
	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	err := mtbl.SetBootVars(setVars)
	c.Assert(err, IsNil)

	s.testClearRecoverySystem(c, mtbl, clearRecoverySystemTestCase{systemLabel: "1234"})
	// bootloader variables have been cleared
	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
}

func (s *systemsSuite) TestClearRecoverySystemInconsistentStateHappy(c *C) {
	setVars := map[string]string{
		"recovery_system_status": "foo",
		"try_recovery_system":    "",
	}
	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	err := mtbl.SetBootVars(setVars)
	c.Assert(err, IsNil)

	s.testClearRecoverySystem(c, mtbl, clearRecoverySystemTestCase{systemLabel: "1234"})
	// bootloader variables have been cleared
	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
}

func (s *systemsSuite) TestClearRecoverySystemInconsistentNoLabelHappy(c *C) {
	setVars := map[string]string{
		"recovery_system_status": "this-will-be-gone",
		"try_recovery_system":    "this-too",
	}
	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	err := mtbl.SetBootVars(setVars)
	c.Assert(err, IsNil)

	// clear without passing the system label, just clears the relevant boot
	// variables
	const noLabel = ""
	s.testClearRecoverySystem(c, mtbl, clearRecoverySystemTestCase{systemLabel: noLabel})
	// bootloader variables have been cleared
	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
}

func (s *systemsSuite) TestClearRecoverySystemRemodelHappy(c *C) {
	setVars := map[string]string{
		"recovery_system_status": "try",
		"try_recovery_system":    "1234",
	}
	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	err := mtbl.SetBootVars(setVars)
	c.Assert(err, IsNil)

	s.testClearRecoverySystem(c, mtbl, clearRecoverySystemTestCase{
		systemLabel: "1234",
		tryModel: boottest.MakeMockUC20Model(map[string]interface{}{
			"tryModelodel": "my-new-model",
		}),
	})
	// bootloader variables have been cleared
	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
}

func (s *systemsSuite) TestClearRecoverySystemResealFails(c *C) {
	setVars := map[string]string{
		"recovery_system_status": "try",
		"try_recovery_system":    "1234",
	}
	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	err := mtbl.SetBootVars(setVars)
	c.Assert(err, IsNil)

	s.testClearRecoverySystem(c, mtbl, clearRecoverySystemTestCase{
		systemLabel: "1234",
		resealErr:   fmt.Errorf("reseal fails"),
		expectedErr: "reseal fails",
	})
	// bootloader variables have been cleared
	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	// variables were cleared
	c.Check(vars, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
}

func (s *systemsSuite) TestClearRecoverySystemSetBootVarsFails(c *C) {
	setVars := map[string]string{
		"recovery_system_status": "try",
		"try_recovery_system":    "1234",
	}
	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	err := mtbl.SetBootVars(setVars)
	c.Assert(err, IsNil)
	mtbl.SetErr = fmt.Errorf("set boot vars fails")

	s.testClearRecoverySystem(c, mtbl, clearRecoverySystemTestCase{
		systemLabel: "1234",
		expectedErr: "set boot vars fails",
	})
}

func (s *systemsSuite) TestClearRecoverySystemReboot(c *C) {
	setVars := map[string]string{
		"recovery_system_status": "try",
		"try_recovery_system":    "1234",
	}
	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	err := mtbl.SetBootVars(setVars)
	c.Assert(err, IsNil)

	mockAssetsCache(c, s.rootdir, "trusted", []string{
		"asset-asset-hash-1",
	})

	bootloader.Force(mtbl)
	defer bootloader.Force(nil)

	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)

	model := s.uc20dev.Model()

	modeenv := &boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825", "1234"},
		CurrentKernels:         []string{},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	c.Assert(modeenv.WriteTo(""), IsNil)

	var readSeedSeenLabels []string
	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		// the mock bootloader can only mock a single recovery boot
		// chain, so pretend both seeds use the same kernel, but keep track of the labels
		readSeedSeenLabels = append(readSeedSeenLabels, label)
		return model, []*seed.Snap{s.seedKernelSnap, s.seedGadgetSnap}, nil
	})
	defer restore()

	resealCalls := 0
	restore = boot.MockResealKeyForBootChains(func(method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
		resealCalls++
		switch resealCalls {
		case 1:
			panic("reseal panic")
		case 2:
			c.Assert(params.RecoveryBootChains, HasLen, 1)
			recoveryBootChain := params.RecoveryBootChains[0]
			c.Check(recoveryBootChain.KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 static cmdline",
			})
			return nil
		default:
			c.Errorf("unexpected call to ResealKeyForBootChains with count %v", resealCalls)
			return fmt.Errorf("unexpected call")

		}
	})
	defer restore()

	checkGoodState := func() {
		// modeenv was already written
		modeenvRead, err := boot.ReadModeenv("")
		c.Assert(err, IsNil)
		// modeenv systems list has one entry only
		c.Check(modeenvRead.CurrentRecoverySystems, DeepEquals, []string{
			"20200825",
		})
		// bootloader variables have been cleared already
		vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
		c.Assert(err, IsNil)
		// variables were cleared
		c.Check(vars, DeepEquals, map[string]string{
			"try_recovery_system":    "",
			"recovery_system_status": "",
		})
	}

	c.Assert(func() {
		boot.ClearTryRecoverySystem(s.uc20dev, "1234")
	}, PanicMatches, "reseal panic")
	// only one seed system accessed
	c.Check(readSeedSeenLabels, DeepEquals, []string{"20200825", "20200825"})
	// panicked on run key
	c.Check(resealCalls, Equals, 1)
	checkGoodState()

	mtbl.SetErrFunc = func() error {
		panic("set boot vars panic")
	}
	c.Assert(func() {
		boot.ClearTryRecoverySystem(s.uc20dev, "1234")
	}, PanicMatches, "set boot vars panic")
	// we did not reach resealing yet
	c.Check(resealCalls, Equals, 1)
	checkGoodState()

	mtbl.SetErrFunc = nil
	err = boot.ClearTryRecoverySystem(s.uc20dev, "1234")
	c.Assert(err, IsNil)
	checkGoodState()
}

type recoverySystemGoodTestCase struct {
	systemLabelAddToCurrent bool
	systemLabelAddToGood    bool
	triedSystems            []string

	resealRecoveryKeyErr              error
	resealRecoveryKeyDuringCleanupErr error
	resealCalls                       int
	expectedErr                       string

	readSeedSystems            []string
	expectedCurrentSystemsList []string
	expectedGoodSystemsList    []string
}

func (s *systemsSuite) testPromoteTriedRecoverySystem(c *C, systemLabel string, tc recoverySystemGoodTestCase) {
	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	mockAssetsCache(c, s.rootdir, "trusted", []string{
		"asset-asset-hash-1",
	})

	bootloader.Force(mtbl)
	defer bootloader.Force(nil)

	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)

	model := s.uc20dev.Model()

	modeenv := &boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825"},
		GoodRecoverySystems:    []string{"20200825"},
		CurrentKernels:         []string{},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	if tc.systemLabelAddToCurrent {
		modeenv.CurrentRecoverySystems = append(modeenv.CurrentRecoverySystems, systemLabel)
	}
	if tc.systemLabelAddToGood {
		modeenv.GoodRecoverySystems = append(modeenv.GoodRecoverySystems, systemLabel)
	}

	c.Assert(modeenv.WriteTo(""), IsNil)

	var readSeedSeenLabels []string
	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		// the mock bootloader can only mock a single recovery boot
		// chain, so pretend both seeds use the same kernel, but keep track of the labels
		readSeedSeenLabels = append(readSeedSeenLabels, label)
		return model, []*seed.Snap{s.seedKernelSnap, s.seedGadgetSnap}, nil
	})
	defer restore()

	resealCalls := 0
	restore = boot.MockResealKeyForBootChains(func(method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
		resealCalls++
		switch resealCalls {
		case 1:
			c.Assert(params.RecoveryBootChainsForRunKey, HasLen, 2)
			recoveryRunBootChain := params.RecoveryBootChainsForRunKey[0]
			c.Check(recoveryRunBootChain.KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 static cmdline",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 static cmdline",
			})
			tryRecoveryRunBootChain := params.RecoveryBootChainsForRunKey[1]
			c.Check(tryRecoveryRunBootChain.KernelCmdlines, DeepEquals, []string{
				fmt.Sprintf("snapd_recovery_mode=recover snapd_recovery_system=%s static cmdline", systemLabel),
				fmt.Sprintf("snapd_recovery_mode=factory-reset snapd_recovery_system=%s static cmdline", systemLabel),
			})

			c.Assert(params.RecoveryBootChains, HasLen, 2)
			recoveryBootChain := params.RecoveryBootChains[0]
			c.Check(recoveryBootChain.KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 static cmdline",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 static cmdline",
			})
			tryRecoveryBootChain := params.RecoveryBootChains[1]
			c.Check(tryRecoveryBootChain.KernelCmdlines, DeepEquals, []string{
				fmt.Sprintf("snapd_recovery_mode=recover snapd_recovery_system=%s static cmdline", systemLabel),
				fmt.Sprintf("snapd_recovery_mode=factory-reset snapd_recovery_system=%s static cmdline", systemLabel),
			})

			return tc.resealRecoveryKeyErr
		case 2:
			// run key boot chain is unchanged, so only recovery key boot chain is resealed
			if tc.resealRecoveryKeyErr == nil {
				c.Errorf("unexpected call to ResealKeyForBootChains with count %v", resealCalls)
				return fmt.Errorf("unexpected call")
			}
			c.Assert(params.RecoveryBootChainsForRunKey, HasLen, 1)
			recoveryRunBootChain := params.RecoveryBootChainsForRunKey[0]
			c.Check(recoveryRunBootChain.KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 static cmdline",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 static cmdline",
			})

			c.Assert(params.RecoveryBootChains, HasLen, 1)
			recoveryBootChain := params.RecoveryBootChains[0]
			c.Check(recoveryBootChain.KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 static cmdline",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 static cmdline",
			})
			return tc.resealRecoveryKeyDuringCleanupErr
		default:
			c.Errorf("unexpected call to ResealKeyForBootChains with count %v", resealCalls)
			return fmt.Errorf("unexpected call")
		}
	})
	defer restore()

	err := boot.PromoteTriedRecoverySystem(s.uc20dev, systemLabel, tc.triedSystems)
	if tc.expectedErr == "" {
		c.Assert(err, IsNil)
	} else {
		c.Assert(err, ErrorMatches, tc.expectedErr)
	}
	c.Check(readSeedSeenLabels, DeepEquals, tc.readSeedSystems)
	c.Check(resealCalls, Equals, tc.resealCalls)

	modeenvRead, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvRead.GoodRecoverySystems, DeepEquals, tc.expectedGoodSystemsList)
	c.Check(modeenvRead.CurrentRecoverySystems, DeepEquals, tc.expectedCurrentSystemsList)
}

func (s *systemsSuite) TestPromoteTriedRecoverySystemHappy(c *C) {
	s.testPromoteTriedRecoverySystem(c, "1234", recoverySystemGoodTestCase{
		triedSystems: []string{"1234"},

		resealCalls: 1,

		readSeedSystems: []string{
			// run key
			"20200825", "1234",
			// recovery keys
			"20200825", "1234",
		},

		expectedCurrentSystemsList: []string{"20200825", "1234"},
		expectedGoodSystemsList:    []string{"20200825", "1234"},
	})
}

func (s *systemsSuite) TestPromoteTriedRecoverySystemInCurrent(c *C) {
	s.testPromoteTriedRecoverySystem(c, "1234", recoverySystemGoodTestCase{
		triedSystems:            []string{"1234"},
		systemLabelAddToCurrent: true,
		resealCalls:             1,

		readSeedSystems: []string{
			// run key
			"20200825", "1234",
			// recovery keys
			"20200825", "1234",
		},
		expectedCurrentSystemsList: []string{"20200825", "1234"},
		expectedGoodSystemsList:    []string{"20200825", "1234"},
	})
}

func (s *systemsSuite) TestPromoteTriedRecoverySystemPresentEverywhere(c *C) {
	s.testPromoteTriedRecoverySystem(c, "1234", recoverySystemGoodTestCase{
		triedSystems:            []string{"1234"},
		systemLabelAddToCurrent: true,
		systemLabelAddToGood:    true,

		resealCalls: 1,

		readSeedSystems: []string{
			// run key
			"20200825", "1234",
			// recovery keys
			"20200825", "1234",
		},
		expectedCurrentSystemsList: []string{"20200825", "1234"},
		expectedGoodSystemsList:    []string{"20200825", "1234"},
	})
}

func (s *systemsSuite) TestPromoteTriedRecoverySystemResealFails(c *C) {
	s.testPromoteTriedRecoverySystem(c, "1234", recoverySystemGoodTestCase{
		triedSystems:         []string{"1234"},
		resealRecoveryKeyErr: fmt.Errorf("recovery key reseal mock failure"),
		// no failure during cleanup
		resealRecoveryKeyDuringCleanupErr: nil,

		resealCalls: 2,

		expectedErr: `recovery key reseal mock failure`,

		readSeedSystems: []string{
			// run key
			"20200825", "1234",
			// recovery keys
			"20200825", "1234",
			// cleanup run key reseal (the seed system is still in
			// current-recovery-systems)
			"20200825",
			// cleanup recovery keys
			"20200825",
		},
		expectedCurrentSystemsList: []string{"20200825"},
		expectedGoodSystemsList:    []string{"20200825"},
	})
}

func (s *systemsSuite) TestPromoteTriedRecoverySystemResealUndoFails(c *C) {
	s.testPromoteTriedRecoverySystem(c, "1234", recoverySystemGoodTestCase{
		triedSystems:                      []string{"1234"},
		resealRecoveryKeyErr:              fmt.Errorf("recovery key reseal mock failure"),
		resealRecoveryKeyDuringCleanupErr: fmt.Errorf("recovery key reseal mock fail in cleanup"),

		resealCalls: 2,

		expectedErr: `recovery key reseal mock failure \(cleanup failed: recovery key reseal mock fail in cleanup\)`,

		readSeedSystems: []string{
			// run key
			"20200825", "1234",
			// recovery keys
			"20200825", "1234",
			// cleanup run key reseal (the seed system is still in
			// current-recovery-systems)
			// cleanup run key
			"20200825",
			// cleanup recovery keys
			"20200825",
		},
		expectedCurrentSystemsList: []string{"20200825"},
		expectedGoodSystemsList:    []string{"20200825"},
	})
}

func (s *systemsSuite) TestPromoteTriedRecoverySystemNotTried(c *C) {
	s.testPromoteTriedRecoverySystem(c, "1234", recoverySystemGoodTestCase{
		triedSystems: []string{"not-here"},

		expectedErr: `system has not been successfully tried`,

		expectedCurrentSystemsList: []string{"20200825"},
		expectedGoodSystemsList:    []string{"20200825"},
	})

	// also works if tried systems list is nil
	s.testPromoteTriedRecoverySystem(c, "1234", recoverySystemGoodTestCase{
		triedSystems: nil,

		expectedErr: `system has not been successfully tried`,

		expectedCurrentSystemsList: []string{"20200825"},
		expectedGoodSystemsList:    []string{"20200825"},
	})
}

type recoverySystemDropTestCase struct {
	systemLabelAddToCurrent bool
	systemLabelAddToGood    bool

	resealRecoveryKeyErr error
	resealCalls          int
	expectedErr          string

	expectedCurrentSystemsList []string
	expectedGoodSystemsList    []string
}

func (s *systemsSuite) testDropRecoverySystem(c *C, systemLabel string, tc recoverySystemDropTestCase) {
	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	mockAssetsCache(c, s.rootdir, "trusted", []string{
		"asset-asset-hash-1",
	})

	bootloader.Force(mtbl)
	defer bootloader.Force(nil)

	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)

	model := s.uc20dev.Model()

	modeenv := &boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825"},
		GoodRecoverySystems:    []string{"20200825"},
		CurrentKernels:         []string{},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	if tc.systemLabelAddToCurrent {
		modeenv.CurrentRecoverySystems = append(modeenv.CurrentRecoverySystems, systemLabel)
	}
	if tc.systemLabelAddToGood {
		modeenv.GoodRecoverySystems = append(modeenv.GoodRecoverySystems, systemLabel)
	}

	c.Assert(modeenv.WriteTo(""), IsNil)

	var readSeedSeenLabels []string
	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		// the mock bootloader can only mock a single recovery boot
		// chain, so pretend both seeds use the same kernel, but keep track of the labels
		readSeedSeenLabels = append(readSeedSeenLabels, label)
		return model, []*seed.Snap{s.seedKernelSnap, s.seedGadgetSnap}, nil
	})
	defer restore()

	resealCalls := 0
	restore = boot.MockResealKeyForBootChains(func(method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
		resealCalls++
		switch resealCalls {
		case 1:
			c.Assert(params.RecoveryBootChainsForRunKey, HasLen, 1)
			recoveryRunBootChain := params.RecoveryBootChainsForRunKey[0]
			c.Check(recoveryRunBootChain.KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 static cmdline",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 static cmdline",
			})

			c.Assert(params.RecoveryBootChains, HasLen, 1)
			recoveryBootChain := params.RecoveryBootChains[0]
			c.Check(recoveryBootChain.KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 static cmdline",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 static cmdline",
			})
			return tc.resealRecoveryKeyErr
		default:
			c.Errorf("unexpected call to ResealKeyForBootChains with count %v", resealCalls)
			return fmt.Errorf("unexpected call")
		}
	})
	defer restore()

	err := boot.DropRecoverySystem(s.uc20dev, systemLabel)
	if tc.expectedErr == "" {
		c.Assert(err, IsNil)
	} else {
		c.Assert(err, ErrorMatches, tc.expectedErr)
	}
	c.Check(readSeedSeenLabels, DeepEquals, []string{"20200825", "20200825"})
	c.Check(resealCalls, Equals, tc.resealCalls)

	modeenvRead, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// current is unchanged
	c.Check(modeenvRead.GoodRecoverySystems, DeepEquals, tc.expectedCurrentSystemsList)
	c.Check(modeenvRead.CurrentRecoverySystems, DeepEquals, tc.expectedGoodSystemsList)
}

func (s *systemsSuite) TestDropRecoverySystemHappy(c *C) {
	s.testDropRecoverySystem(c, "1234", recoverySystemDropTestCase{
		systemLabelAddToCurrent: true,
		systemLabelAddToGood:    true,
		resealCalls:             1,

		expectedGoodSystemsList:    []string{"20200825"},
		expectedCurrentSystemsList: []string{"20200825"},
	})
}

func (s *systemsSuite) TestDropRecoverySystemAlreadyGoneFromBoth(c *C) {
	s.testDropRecoverySystem(c, "1234", recoverySystemDropTestCase{
		systemLabelAddToCurrent: false,
		systemLabelAddToGood:    false,
		resealCalls:             1,

		expectedGoodSystemsList:    []string{"20200825"},
		expectedCurrentSystemsList: []string{"20200825"},
	})
}

func (s *systemsSuite) TestDropRecoverySystemAlreadyGoneOne(c *C) {
	s.testDropRecoverySystem(c, "1234", recoverySystemDropTestCase{
		systemLabelAddToCurrent: true,
		systemLabelAddToGood:    false,
		resealCalls:             1,

		expectedGoodSystemsList:    []string{"20200825"},
		expectedCurrentSystemsList: []string{"20200825"},
	})
}

func (s *systemsSuite) TestDropRecoverySystemResealErr(c *C) {
	s.testDropRecoverySystem(c, "1234", recoverySystemDropTestCase{
		systemLabelAddToCurrent: true,
		systemLabelAddToGood:    false,
		resealCalls:             1,
		resealRecoveryKeyErr:    fmt.Errorf("mocked error"),
		expectedErr:             `mocked error`,

		expectedGoodSystemsList:    []string{"20200825"},
		expectedCurrentSystemsList: []string{"20200825"},
	})
}

func (s *systemsSuite) TestMarkRecoveryCapableSystemHappy(c *C) {
	rbl := bootloadertest.Mock("recovery", c.MkDir()).RecoveryAware()
	bootloader.Force(rbl)

	err := boot.MarkRecoveryCapableSystem("1234")
	c.Assert(err, IsNil)
	vars, err := rbl.GetBootVars("snapd_good_recovery_systems")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"snapd_good_recovery_systems": "1234",
	})
	// try the same system again
	err = boot.MarkRecoveryCapableSystem("1234")
	c.Assert(err, IsNil)
	vars, err = rbl.GetBootVars("snapd_good_recovery_systems")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		// still a single entry
		"snapd_good_recovery_systems": "1234",
	})

	// try something new
	err = boot.MarkRecoveryCapableSystem("4567")
	c.Assert(err, IsNil)
	vars, err = rbl.GetBootVars("snapd_good_recovery_systems")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		// entry added
		"snapd_good_recovery_systems": "1234,4567",
	})

	// try adding the old one again
	err = boot.MarkRecoveryCapableSystem("1234")
	c.Assert(err, IsNil)
	vars, err = rbl.GetBootVars("snapd_good_recovery_systems")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		// system got moved to the end of the list
		"snapd_good_recovery_systems": "4567,1234",
	})

	// and the new one again
	err = boot.MarkRecoveryCapableSystem("4567")
	c.Assert(err, IsNil)
	vars, err = rbl.GetBootVars("snapd_good_recovery_systems")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		// and it became the last entry
		"snapd_good_recovery_systems": "1234,4567",
	})
}

func (s *systemsSuite) TestMarkRecoveryCapableSystemAlwaysLast(c *C) {
	rbl := bootloadertest.Mock("recovery", c.MkDir()).RecoveryAware()
	bootloader.Force(rbl)

	err := rbl.SetBootVars(map[string]string{
		"snapd_good_recovery_systems": "1234,2222",
	})
	c.Assert(err, IsNil)

	err = boot.MarkRecoveryCapableSystem("1234")
	c.Assert(err, IsNil)
	vars, err := rbl.GetBootVars("snapd_good_recovery_systems")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"snapd_good_recovery_systems": "2222,1234",
	})

	err = rbl.SetBootVars(map[string]string{
		"snapd_good_recovery_systems": "1111,1234,2222",
	})
	c.Assert(err, IsNil)
	err = boot.MarkRecoveryCapableSystem("1234")
	c.Assert(err, IsNil)
	vars, err = rbl.GetBootVars("snapd_good_recovery_systems")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"snapd_good_recovery_systems": "1111,2222,1234",
	})

	err = rbl.SetBootVars(map[string]string{
		"snapd_good_recovery_systems": "1111,2222",
	})
	c.Assert(err, IsNil)
	err = boot.MarkRecoveryCapableSystem("1234")
	c.Assert(err, IsNil)
	vars, err = rbl.GetBootVars("snapd_good_recovery_systems")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"snapd_good_recovery_systems": "1111,2222,1234",
	})
}

func (s *systemsSuite) TestMarkRecoveryCapableSystemErr(c *C) {
	rbl := bootloadertest.Mock("recovery", c.MkDir()).RecoveryAware()
	bootloader.Force(rbl)

	err := boot.MarkRecoveryCapableSystem("1234")
	c.Assert(err, IsNil)
	vars, err := rbl.GetBootVars("snapd_good_recovery_systems")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"snapd_good_recovery_systems": "1234",
	})

	rbl.SetErr = fmt.Errorf("mocked error")
	err = boot.MarkRecoveryCapableSystem("4567")
	c.Assert(err, ErrorMatches, "mocked error")
	vars, err = rbl.GetBootVars("snapd_good_recovery_systems")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		// mocked error is returned after variable is set
		"snapd_good_recovery_systems": "1234,4567",
	})

	// but mocked panic happens earlier
	rbl.SetMockToPanic("SetBootVars")
	c.Assert(func() { boot.MarkRecoveryCapableSystem("9999") },
		PanicMatches, "mocked reboot panic in SetBootVars")
	vars, err = rbl.GetBootVars("snapd_good_recovery_systems")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"snapd_good_recovery_systems": "1234,4567",
	})

}

func (s *systemsSuite) TestMarkRecoveryCapableSystemNonRecoveryAware(c *C) {
	bl := bootloadertest.Mock("recovery", c.MkDir())
	bootloader.Force(bl)

	err := boot.MarkRecoveryCapableSystem("1234")
	c.Assert(err, IsNil)
	c.Check(bl.SetBootVarsCalls, Equals, 0)
}

type initramfsMarkTryRecoverySystemSuite struct {
	baseSystemsSuite

	bl *bootloadertest.MockBootloader
}

var _ = Suite(&initramfsMarkTryRecoverySystemSuite{})

func (s *initramfsMarkTryRecoverySystemSuite) SetUpTest(c *C) {
	s.baseSystemsSuite.SetUpTest(c)

	s.bl = bootloadertest.Mock("bootloader", s.bootdir)
	bootloader.Force(s.bl)
	s.AddCleanup(func() { bootloader.Force(nil) })
}

func (s *initramfsMarkTryRecoverySystemSuite) testMarkRecoverySystemForRun(c *C, outcome boot.TryRecoverySystemOutcome, expectingStatus string) {
	err := s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "try",
		"try_recovery_system":    "1234",
	})
	c.Assert(err, IsNil)
	err = boot.EnsureNextBootToRunModeWithTryRecoverySystemOutcome(outcome)
	c.Assert(err, IsNil)

	expectedVars := map[string]string{
		"snapd_recovery_mode":   "run",
		"snapd_recovery_system": "",

		"recovery_system_status": expectingStatus,
		"try_recovery_system":    "1234",
	}

	vars, err := s.bl.GetBootVars("snapd_recovery_mode", "snapd_recovery_system",
		"recovery_system_status", "try_recovery_system")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, expectedVars)

	err = s.bl.SetBootVars(map[string]string{
		// the status is overwritten, even if it's completely bogus
		"recovery_system_status": "foobar",
		"try_recovery_system":    "1234",
	})
	c.Assert(err, IsNil)

	err = boot.EnsureNextBootToRunModeWithTryRecoverySystemOutcome(outcome)
	c.Assert(err, IsNil)

	vars, err = s.bl.GetBootVars("snapd_recovery_mode", "snapd_recovery_system",
		"recovery_system_status", "try_recovery_system")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, expectedVars)
}

func (s *initramfsMarkTryRecoverySystemSuite) TestMarkTryRecoverySystemSuccess(c *C) {
	s.testMarkRecoverySystemForRun(c, boot.TryRecoverySystemOutcomeSuccess, "tried")
}

func (s *initramfsMarkTryRecoverySystemSuite) TestMarkRecoverySystemFailure(c *C) {
	s.testMarkRecoverySystemForRun(c, boot.TryRecoverySystemOutcomeFailure, "try")
}

func (s *initramfsMarkTryRecoverySystemSuite) TestMarkRecoverySystemBogus(c *C) {
	s.testMarkRecoverySystemForRun(c, boot.TryRecoverySystemOutcomeInconsistent, "")
}

func (s *initramfsMarkTryRecoverySystemSuite) TestMarkRecoverySystemErr(c *C) {
	s.bl.SetErr = fmt.Errorf("set fails")
	err := boot.EnsureNextBootToRunModeWithTryRecoverySystemOutcome(boot.TryRecoverySystemOutcomeSuccess)
	c.Assert(err, ErrorMatches, "set fails")
	err = boot.EnsureNextBootToRunModeWithTryRecoverySystemOutcome(boot.TryRecoverySystemOutcomeFailure)
	c.Assert(err, ErrorMatches, "set fails")
	err = boot.EnsureNextBootToRunModeWithTryRecoverySystemOutcome(boot.TryRecoverySystemOutcomeInconsistent)
	c.Assert(err, ErrorMatches, "set fails")
}

func (s *initramfsMarkTryRecoverySystemSuite) TestTryingRecoverySystemUnset(c *C) {
	err := s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "try",
		// system is unset
		"try_recovery_system": "",
	})
	c.Assert(err, IsNil)
	isTry, err := boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, ErrorMatches, `try recovery system is unset but status is "try"`)
	c.Check(boot.IsInconsistentRecoverySystemState(err), Equals, true)
	c.Check(isTry, Equals, false)
}

func (s *initramfsMarkTryRecoverySystemSuite) TestTryingRecoverySystemBogus(c *C) {
	err := s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "foobar",
		"try_recovery_system":    "1234",
	})
	c.Assert(err, IsNil)
	isTry, err := boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, ErrorMatches, `unexpected recovery system status "foobar"`)
	c.Check(boot.IsInconsistentRecoverySystemState(err), Equals, true)
	c.Check(isTry, Equals, false)

	// errors out even if try recovery system label is unset
	err = s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "no-label",
		"try_recovery_system":    "",
	})
	c.Assert(err, IsNil)
	isTry, err = boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, ErrorMatches, `unexpected recovery system status "no-label"`)
	c.Check(boot.IsInconsistentRecoverySystemState(err), Equals, true)
	c.Check(isTry, Equals, false)
}

func (s *initramfsMarkTryRecoverySystemSuite) TestTryingRecoverySystemNoTryingStatus(c *C) {
	err := s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "",
		"try_recovery_system":    "",
	})
	c.Assert(err, IsNil)
	isTry, err := boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, IsNil)
	c.Check(isTry, Equals, false)

	err = s.bl.SetBootVars(map[string]string{
		// status is checked first
		"recovery_system_status": "",
		"try_recovery_system":    "1234",
	})
	c.Assert(err, IsNil)
	isTry, err = boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, IsNil)
	c.Check(isTry, Equals, false)
}

func (s *initramfsMarkTryRecoverySystemSuite) TestTryingRecoverySystemSameSystem(c *C) {
	// the usual scenario
	err := s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "try",
		"try_recovery_system":    "1234",
	})
	c.Assert(err, IsNil)
	isTry, err := boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, IsNil)
	c.Check(isTry, Equals, true)

	// pretend the system has already been tried
	err = s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "tried",
		"try_recovery_system":    "1234",
	})
	c.Assert(err, IsNil)
	isTry, err = boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, IsNil)
	c.Check(isTry, Equals, true)
}

func (s *initramfsMarkTryRecoverySystemSuite) TestRecoverySystemSuccessDifferent(c *C) {
	// other system
	err := s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "try",
		"try_recovery_system":    "9999",
	})
	c.Assert(err, IsNil)
	isTry, err := boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, IsNil)
	c.Check(isTry, Equals, false)

	// same when the other system has already been tried
	err = s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "tried",
		"try_recovery_system":    "9999",
	})
	c.Assert(err, IsNil)
	isTry, err = boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, IsNil)
	c.Check(isTry, Equals, false)
}

func (s *systemsSuite) TestUnmarkRecoveryCapableSystemHappy(c *C) {
	rbl := bootloadertest.Mock("recovery", c.MkDir()).RecoveryAware()
	bootloader.Force(rbl)

	// mark system
	err := boot.MarkRecoveryCapableSystem("1234")
	c.Assert(err, IsNil)
	vars, err := rbl.GetBootVars("snapd_good_recovery_systems")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"snapd_good_recovery_systems": "1234",
	})

	// mark system
	err = boot.MarkRecoveryCapableSystem("4567")
	c.Assert(err, IsNil)
	vars, err = rbl.GetBootVars("snapd_good_recovery_systems")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"snapd_good_recovery_systems": "1234,4567",
	})

	// unmark system that is not present, function is idempotent
	err = boot.UnmarkRecoveryCapableSystem("not-here")
	c.Assert(err, IsNil)
	vars, err = rbl.GetBootVars("snapd_good_recovery_systems")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"snapd_good_recovery_systems": "1234,4567",
	})

	// unmark system
	err = boot.UnmarkRecoveryCapableSystem("1234")
	c.Assert(err, IsNil)
	vars, err = rbl.GetBootVars("snapd_good_recovery_systems")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"snapd_good_recovery_systems": "4567",
	})

	// unmark system
	err = boot.UnmarkRecoveryCapableSystem("4567")
	c.Assert(err, IsNil)
	vars, err = rbl.GetBootVars("snapd_good_recovery_systems")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"snapd_good_recovery_systems": "",
	})
}
