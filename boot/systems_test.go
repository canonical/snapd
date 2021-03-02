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
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
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

	uc20dev boot.Device

	runKernelBf      bootloader.BootFile
	recoveryKernelBf bootloader.BootFile
}

var _ = Suite(&systemsSuite{})

func (s *systemsSuite) mockTrustedBootloaderWithAssetAndChains(c *C, runKernelBf, recoveryKernelBf bootloader.BootFile) *bootloadertest.MockTrustedAssetsBootloader {
	mockAssetsCache(c, s.rootdir, "trusted", []string{
		"asset-asset-hash-1",
	})

	mtbl := bootloadertest.Mock("trusted", s.bootdir).WithTrustedAssets()
	mtbl.TrustedAssetsList = []string{"asset-1"}
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

	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error { return nil })
	s.AddCleanup(restore)

	s.uc20dev = boottest.MockUC20Device("", nil)

	// run kernel
	s.runKernelBf = bootloader.NewBootFile("/var/lib/snapd/snap/pc-kernel_500.snap",
		"kernel.efi", bootloader.RoleRunMode)
	// seed (recovery) kernel
	s.recoveryKernelBf = bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap",
		"kernel.efi", bootloader.RoleRecovery)
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

	modeenv := &boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825"},
		CurrentKernels:         []string{},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},
	}
	c.Assert(modeenv.WriteTo(""), IsNil)

	var readSeedSeenLabels []string
	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		// the mock bootloader can only mock a single recovery boot
		// chain, so pretend both seeds use the same kernel, but keep track of the labels
		readSeedSeenLabels = append(readSeedSeenLabels, label)
		kernelSnap := &seed.Snap{
			Path: "/var/lib/snapd/seed/snaps/pc-kernel_1.snap",
			SideInfo: &snap.SideInfo{
				RealName: "pc-kernel",
				Revision: snap.Revision{N: 1},
			},
		}
		return s.uc20dev.Model(), []*seed.Snap{kernelSnap}, nil
	})
	defer restore()

	resealCalls := 0
	restore = boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		// bootloader variables have already been modified
		c.Check(mtbl.SetBootVarsCalls, Equals, 1)
		return nil
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
	// run and recovery keys
	c.Check(resealCalls, Equals, 2)
	c.Check(readSeedSeenLabels, DeepEquals, []string{"20200825", "1234"})

	modeenvRead, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvRead.CurrentRecoverySystems, DeepEquals, []string{
		"20200825", "1234",
	})
}

func (s *systemsSuite) TestSetTryRecoverySystemSimple(c *C) {
	mtbl := bootloadertest.Mock("trusted", s.bootdir).WithTrustedAssets()
	bootloader.Force(mtbl)
	defer bootloader.Force(nil)

	modeenv := &boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825"},
	}
	c.Assert(modeenv.WriteTo(""), IsNil)

	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
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
	c.Check(modeenvRead.CurrentRecoverySystems, DeepEquals, []string{
		"20200825", "1234",
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

	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
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
	c.Check(modeenvRead.CurrentRecoverySystems, DeepEquals, []string{
		"20200825",
	})
}

func (s *systemsSuite) TestSetTryRecoverySystemCleanupOnErrorBeforeReseal(c *C) {
	mockAssetsCache(c, s.rootdir, "trusted", []string{
		"asset-asset-hash-1",
	})

	mtbl := s.mockTrustedBootloaderWithAssetAndChains(c, s.runKernelBf, s.recoveryKernelBf)
	defer bootloader.Force(nil)

	// system is encrypted
	s.stampSealedKeys(c, s.rootdir)

	modeenv := &boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825"},
		CurrentKernels:         []string{},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},
	}
	c.Assert(modeenv.WriteTo(""), IsNil)

	readSeedCalls := 0
	cleanupTriggered := false
	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		readSeedCalls++
		// this is the reseal cleanup path
		kernelSnap := &seed.Snap{
			Path: "/var/lib/snapd/seed/snaps/pc-kernel_1.snap",
			SideInfo: &snap.SideInfo{
				RealName: "pc-kernel",
				Revision: snap.Revision{N: 1},
			},
		}

		switch readSeedCalls {
		case 1:
			// called for the first system
			c.Assert(label, Equals, "20200825")
			c.Check(mtbl.SetBootVarsCalls, Equals, 1)
			return s.uc20dev.Model(), []*seed.Snap{kernelSnap}, nil
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
			// (cleanup) called for the first system
			c.Assert(label, Equals, "20200825")
			// boot variables already updated
			c.Check(mtbl.SetBootVarsCalls, Equals, 2)
			return s.uc20dev.Model(), []*seed.Snap{kernelSnap}, nil
		default:
			return nil, nil, fmt.Errorf("unexpected call %v", readSeedCalls)
		}
	})
	defer restore()

	resealCalls := 0
	restore = boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
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
	c.Check(readSeedCalls, Equals, 3)
	// called twice during cleanup for run and recovery keys
	c.Check(resealCalls, Equals, 2)

	modeenvRead, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// modeenv is back to normal
	c.Check(modeenvRead.CurrentRecoverySystems, DeepEquals, []string{
		"20200825",
	})
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

	modeenv := &boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825"},
		CurrentKernels:         []string{},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},
	}
	c.Assert(modeenv.WriteTo(""), IsNil)

	readSeedCalls := 0
	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		readSeedCalls++
		// this is the reseal cleanup path
		kernelSnap := &seed.Snap{
			Path: "/var/lib/snapd/seed/snaps/pc-kernel_1.snap",
			SideInfo: &snap.SideInfo{
				RealName: "pc-kernel",
				Revision: snap.Revision{N: 1},
			},
		}

		switch readSeedCalls {
		case 1:
			// called for the first system
			c.Assert(label, Equals, "20200825")
			c.Check(mtbl.SetBootVarsCalls, Equals, 1)
			return s.uc20dev.Model(), []*seed.Snap{kernelSnap}, nil
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
			return s.uc20dev.Model(), []*seed.Snap{kernelSnap}, nil
		case 3:
			// (cleanup) called for the first system
			c.Assert(label, Equals, "20200825")
			// boot variables already updated
			c.Check(mtbl.SetBootVarsCalls, Equals, 2)
			return s.uc20dev.Model(), []*seed.Snap{kernelSnap}, nil
		default:
			return nil, nil, fmt.Errorf("unexpected call %v", readSeedCalls)
		}
	})
	defer restore()

	resealCalls := 0
	restore = boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		switch resealCalls {
		case 1:
			return fmt.Errorf("reseal fails")
		case 2, 3:
			// reseal of run and recovery keys
			return nil
		default:
			return fmt.Errorf("unexpected call")

		}
	})
	defer restore()

	err := boot.SetTryRecoverySystem(s.uc20dev, "1234")
	c.Assert(err, ErrorMatches, "cannot reseal the encryption key: reseal fails")

	// failed after the call to read the 'try' system seed
	c.Check(readSeedCalls, Equals, 3)
	// called 3 times, once when mocked failure occurs, twice during cleanup
	// for run and recovery keys
	c.Check(resealCalls, Equals, 3)

	modeenvRead, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// modeenv is back to normal
	c.Check(modeenvRead.CurrentRecoverySystems, DeepEquals, []string{
		"20200825",
	})
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

	modeenv := &boot.Modeenv{
		Mode: "run",
		// keep this comment to make old gofmt happy
		CurrentRecoverySystems: []string{"20200825"},
		CurrentKernels:         []string{},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},
	}
	c.Assert(modeenv.WriteTo(""), IsNil)

	readSeedCalls := 0
	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		readSeedCalls++
		// this is the reseal cleanup path
		kernelSnap := &seed.Snap{
			Path: "/var/lib/snapd/seed/snaps/pc-kernel_1.snap",
			SideInfo: &snap.SideInfo{
				RealName: "pc-kernel",
				Revision: snap.Revision{N: 1},
			},
		}
		switch readSeedCalls {
		case 1:
			// called for the first system
			c.Assert(label, Equals, "20200825")
			return s.uc20dev.Model(), []*seed.Snap{kernelSnap}, nil
		case 2:
			// called for the 'try' system
			c.Assert(label, Equals, "1234")
			// still good
			return s.uc20dev.Model(), []*seed.Snap{kernelSnap}, nil
		case 3:
			// (cleanup) called for the first system only
			c.Assert(label, Equals, "20200825")
			return s.uc20dev.Model(), []*seed.Snap{kernelSnap}, nil
		default:
			return nil, nil, fmt.Errorf("unexpected call %v", readSeedCalls)
		}
	})
	defer restore()

	resealCalls := 0
	restore = boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		switch resealCalls {
		case 1:
			return fmt.Errorf("reseal fails")
		case 2, 3:
			// reseal of run and recovery keys
			return fmt.Errorf("reseal in cleanup fails too")
		default:
			return fmt.Errorf("unexpected call")

		}
	})
	defer restore()

	err := boot.SetTryRecoverySystem(s.uc20dev, "1234")
	c.Assert(err, ErrorMatches, `cannot reseal the encryption key: reseal fails \(cleanup failed: cannot reseal the encryption key: reseal in cleanup fails too\)`)

	// failed after the call to read the 'try' system seed
	c.Check(readSeedCalls, Equals, 3)
	// called twice, once when enabling the try system, once on cleanup
	c.Check(resealCalls, Equals, 2)

	modeenvRead, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	// modeenv is back to normal
	c.Check(modeenvRead.CurrentRecoverySystems, DeepEquals, []string{
		"20200825",
	})
	// bootloader variables have been cleared regardless of reseal failing
	vars, err := mtbl.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(vars, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
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
	c.Check(boot.IsInconsystemRecoverySystemState(err), Equals, true)
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
	c.Check(boot.IsInconsystemRecoverySystemState(err), Equals, true)
	c.Check(isTry, Equals, false)

	// errors out even if try recovery system label is unset
	err = s.bl.SetBootVars(map[string]string{
		"recovery_system_status": "no-label",
		"try_recovery_system":    "",
	})
	c.Assert(err, IsNil)
	isTry, err = boot.InitramfsIsTryingRecoverySystem("1234")
	c.Assert(err, ErrorMatches, `unexpected recovery system status "no-label"`)
	c.Check(boot.IsInconsystemRecoverySystemState(err), Equals, true)
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
