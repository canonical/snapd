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

package backend_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/kernel/fde"
	fdeBackend "github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/testutil"
)

type sealSuite struct {
	testutil.BaseTest

	rootdir string
}

var _ = Suite(&sealSuite{})

func (s *sealSuite) SetUpTest(c *C) {
	s.rootdir = c.MkDir()
	dirs.SetRootDir(s.rootdir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })
}

func (s *sealSuite) TestSealKeyForBootChains(c *C) {
	for idx, tc := range []struct {
		sealErr                  error
		provisionErr             error
		factoryReset             bool
		pcrHandleOfKey           uint32
		pcrHandleOfKeyErr        error
		shimId                   string
		grubId                   string
		runGrubId                string
		expErr                   string
		expProvisionCalls        int
		expSealCalls             int
		expReleasePCRHandleCalls int
		expPCRHandleOfKeyCalls   int
		disableTokens            bool
	}{
		{
			sealErr: nil, expErr: "",
			expProvisionCalls: 1, expSealCalls: 2,
		}, {
			sealErr: nil, expErr: "",
			expProvisionCalls: 1, expSealCalls: 2, disableTokens: true,
		}, {
			sealErr: nil,
			// old boot assets
			shimId: "bootx64.efi", grubId: "grubx64.efi",
			expErr:            "",
			expProvisionCalls: 1, expSealCalls: 2,
		}, {
			sealErr: nil, factoryReset: true, pcrHandleOfKey: secboot.FallbackObjectPCRPolicyCounterHandle,
			expProvisionCalls: 1, expSealCalls: 2, expPCRHandleOfKeyCalls: 1, expReleasePCRHandleCalls: 1,
		}, {
			sealErr: nil, factoryReset: true, pcrHandleOfKey: secboot.FallbackObjectPCRPolicyCounterHandle,
			expProvisionCalls: 1, expSealCalls: 2, expPCRHandleOfKeyCalls: 1, expReleasePCRHandleCalls: 1, disableTokens: true,
		}, {
			sealErr: nil, factoryReset: true, pcrHandleOfKey: secboot.AltFallbackObjectPCRPolicyCounterHandle,
			expProvisionCalls: 1, expSealCalls: 2, expPCRHandleOfKeyCalls: 1, expReleasePCRHandleCalls: 1,
		}, {
			sealErr: nil, factoryReset: true, pcrHandleOfKeyErr: errors.New("PCR handle error"),
			expErr:                 "PCR handle error",
			expPCRHandleOfKeyCalls: 1,
		}, {
			sealErr: errors.New("seal error"), expErr: "cannot seal the encryption keys: seal error",
			expProvisionCalls: 1, expSealCalls: 1,
		}, {
			provisionErr: errors.New("provision error"), sealErr: errors.New("unexpected call"),
			expErr:            "provision error",
			expProvisionCalls: 1,
		},
	} {
		c.Logf("tc %v", idx)
		rootdir := c.MkDir()
		dirs.SetRootDir(rootdir)
		defer dirs.SetRootDir(s.rootdir)

		shimId := tc.shimId
		if shimId == "" {
			shimId = "ubuntu:shimx64.efi"
		}
		grubId := tc.grubId
		if grubId == "" {
			grubId = "ubuntu:grubx64.efi"
		}
		runGrubId := tc.runGrubId
		if runGrubId == "" {
			runGrubId = "grubx64.efi"
		}

		model := boottest.MakeMockUC20Model()

		// mock asset cache
		mockAssetsCache(c, rootdir, "grub", []string{
			fmt.Sprintf("%s-shim-hash-1", shimId),
			fmt.Sprintf("%s-grub-hash-1", grubId),
			fmt.Sprintf("%s-run-grub-hash-1", runGrubId),
		})

		// set encryption key
		myKey := secboot.CreateMockBootstrappedContainer()
		myKey2 := secboot.CreateMockBootstrappedContainer()

		provisionCalls := 0
		restore := fdeBackend.MockSecbootProvisionTPM(func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error {
			provisionCalls++
			c.Check(lockoutAuthFile, Equals, filepath.Join(boot.InstallHostFDESaveDir, "tpm-lockout-auth"))
			if tc.factoryReset {
				c.Check(mode, Equals, secboot.TPMPartialReprovision)
			} else {
				c.Check(mode, Equals, secboot.TPMProvisionFull)
			}
			return tc.provisionErr
		})
		defer restore()

		pcrHandleOfKeyCalls := 0
		restore = boot.MockSecbootPCRHandleOfSealedKey(func(p string) (uint32, error) {
			pcrHandleOfKeyCalls++
			c.Check(provisionCalls, Equals, 0)
			c.Check(p, Equals, filepath.Join(rootdir, "/run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"))
			return tc.pcrHandleOfKey, tc.pcrHandleOfKeyErr
		})
		defer restore()

		releasePCRHandleCalls := 0
		releaseResourceFunc := func(handles ...uint32) error {
			c.Check(tc.factoryReset, Equals, true)
			releasePCRHandleCalls++
			if tc.pcrHandleOfKey == secboot.FallbackObjectPCRPolicyCounterHandle {
				c.Check(handles, DeepEquals, []uint32{
					secboot.AltRunObjectPCRPolicyCounterHandle,
					secboot.AltFallbackObjectPCRPolicyCounterHandle,
				})
			} else {
				c.Check(handles, DeepEquals, []uint32{
					secboot.RunObjectPCRPolicyCounterHandle,
					secboot.FallbackObjectPCRPolicyCounterHandle,
				})
			}
			return nil
		}
		restore = boot.MockSecbootReleasePCRResourceHandles(releaseResourceFunc)
		defer restore()
		restore = fdeBackend.MockSecbootReleasePCRResourceHandles(releaseResourceFunc)
		defer restore()

		// set mock key sealing
		sealKeysCalls := 0
		restore = fdeBackend.MockSecbootSealKeys(func(keys []secboot.SealKeyRequest, params *secboot.SealKeysParams) ([]byte, error) {
			c.Assert(provisionCalls, Equals, 1, Commentf("TPM must have been provisioned before"))
			sealKeysCalls++
			switch sealKeysCalls {
			case 1:
				// the run object seals only the ubuntu-data key
				c.Check(params.TPMPolicyAuthKeyFile, Equals, filepath.Join(boot.InstallHostFDESaveDir, "tpm-policy-auth-key"))

				expectedSKR := secboot.SealKeyRequest{BootstrappedContainer: myKey, KeyName: "ubuntu-data", SlotName: "default"}
				if tc.disableTokens {
					expectedSKR.KeyFile = filepath.Join(rootdir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key")
				}
				c.Check(keys, DeepEquals, []secboot.SealKeyRequest{expectedSKR})
				if tc.pcrHandleOfKey == secboot.FallbackObjectPCRPolicyCounterHandle {
					c.Check(params.PCRPolicyCounterHandle, Equals, secboot.AltRunObjectPCRPolicyCounterHandle)
				} else {
					c.Check(params.PCRPolicyCounterHandle, Equals, secboot.RunObjectPCRPolicyCounterHandle)
				}
			case 2:
				// the fallback object seals the ubuntu-data and the ubuntu-save keys
				c.Check(params.TPMPolicyAuthKeyFile, Equals, "")

				expectedDataSKR := secboot.SealKeyRequest{BootstrappedContainer: myKey, KeyName: "ubuntu-data", SlotName: "default-fallback"}
				expectedSaveSKR := secboot.SealKeyRequest{BootstrappedContainer: myKey2, KeyName: "ubuntu-save", SlotName: "default-fallback"}
				if tc.disableTokens {
					expectedDataSKR.KeyFile = filepath.Join(rootdir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key")
					if tc.factoryReset {
						expectedSaveSKR.KeyFile = filepath.Join(rootdir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key.factory-reset")
					} else {
						expectedSaveSKR.KeyFile = filepath.Join(rootdir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key")
					}
				}
				c.Check(keys, DeepEquals, []secboot.SealKeyRequest{expectedDataSKR, expectedSaveSKR})
				if tc.pcrHandleOfKey == secboot.FallbackObjectPCRPolicyCounterHandle {
					c.Check(params.PCRPolicyCounterHandle, Equals, secboot.AltFallbackObjectPCRPolicyCounterHandle)
				} else {
					c.Check(params.PCRPolicyCounterHandle, Equals, secboot.FallbackObjectPCRPolicyCounterHandle)
				}
			default:
				c.Errorf("unexpected additional call to secboot.SealKeys (call # %d)", sealKeysCalls)
			}
			c.Assert(params.ModelParams, HasLen, 1)

			shim := bootloader.NewBootFile("", filepath.Join(rootdir, fmt.Sprintf("var/lib/snapd/boot-assets/grub/%s-shim-hash-1", shimId)), bootloader.RoleRecovery)
			grub := bootloader.NewBootFile("", filepath.Join(rootdir, fmt.Sprintf("var/lib/snapd/boot-assets/grub/%s-grub-hash-1", grubId)), bootloader.RoleRecovery)
			runGrub := bootloader.NewBootFile("", filepath.Join(rootdir, fmt.Sprintf("var/lib/snapd/boot-assets/grub/%s-run-grub-hash-1", runGrubId)), bootloader.RoleRunMode)
			kernel := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
			runKernel := bootloader.NewBootFile(filepath.Join(rootdir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode)

			switch sealKeysCalls {
			case 1:
				c.Assert(params.ModelParams[0].EFILoadChains, DeepEquals, []*secboot.LoadChain{
					secboot.NewLoadChain(shim,
						secboot.NewLoadChain(grub,
							secboot.NewLoadChain(kernel))),
					secboot.NewLoadChain(shim,
						secboot.NewLoadChain(grub,
							secboot.NewLoadChain(runGrub,
								secboot.NewLoadChain(runKernel)))),
				})
				c.Assert(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
					"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
					"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
					"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
				})
			case 2:
				c.Assert(params.ModelParams[0].EFILoadChains, DeepEquals, []*secboot.LoadChain{
					secboot.NewLoadChain(shim,
						secboot.NewLoadChain(grub,
							secboot.NewLoadChain(kernel))),
				})
				c.Assert(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
					"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
					"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				})
			default:
				c.Errorf("unexpected additional call to secboot.SealKeys (call # %d)", sealKeysCalls)
			}
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")

			return nil, tc.sealErr
		})
		defer restore()

		recoveryKernel := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
		runKernel := bootloader.NewBootFile(filepath.Join(rootdir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode)

		params := &boot.SealKeyForBootChainsParams{
			RunModeBootChains: []boot.BootChain{
				{
					BrandID:        model.BrandID(),
					Model:          model.Model(),
					Classic:        model.Classic(),
					Grade:          model.Grade(),
					ModelSignKeyID: model.SignKeyID(),

					AssetChain: []boot.BootAsset{
						{
							Role: bootloader.RoleRecovery,
							Name: shimId,
							Hashes: []string{
								"shim-hash-1",
							},
						},
						{
							Role: bootloader.RoleRecovery,
							Name: grubId,
							Hashes: []string{
								"grub-hash-1",
							},
						},
						{
							Role: bootloader.RoleRunMode,
							Name: runGrubId,
							Hashes: []string{
								"run-grub-hash-1",
							},
						},
					},

					Kernel:         "pc-kernel",
					KernelRevision: "500",
					KernelCmdlines: []string{
						"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
					},
					KernelBootFile: runKernel,
				},
			},
			RecoveryBootChainsForRunKey: nil,
			RecoveryBootChains: []boot.BootChain{
				{
					BrandID:        model.BrandID(),
					Model:          model.Model(),
					Classic:        model.Classic(),
					Grade:          model.Grade(),
					ModelSignKeyID: model.SignKeyID(),

					AssetChain: []boot.BootAsset{
						{
							Role: bootloader.RoleRecovery,
							Name: shimId,
							Hashes: []string{
								"shim-hash-1",
							},
						},
						{
							Role: bootloader.RoleRecovery,
							Name: grubId,
							Hashes: []string{
								"grub-hash-1",
							},
						},
					},

					Kernel:         "pc-kernel",
					KernelRevision: "1",
					KernelCmdlines: []string{
						"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
						"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
					},
					KernelBootFile: recoveryKernel,
				},
			},
			RoleToBlName: map[bootloader.Role]string{
				bootloader.RoleRecovery: "grub",
				bootloader.RoleRunMode:  "grub",
			},
			FactoryReset:           tc.factoryReset,
			InstallHostWritableDir: filepath.Join(boot.InstallUbuntuDataDir, "system-data"),
			UseTokens:              !tc.disableTokens,
		}
		err := boot.SealKeyForBootChains(device.SealingMethodTPM, myKey, myKey2, params)

		c.Check(pcrHandleOfKeyCalls, Equals, tc.expPCRHandleOfKeyCalls)
		c.Check(provisionCalls, Equals, tc.expProvisionCalls)
		c.Check(sealKeysCalls, Equals, tc.expSealCalls)
		c.Check(releasePCRHandleCalls, Equals, tc.expReleasePCRHandleCalls)
		if tc.expErr == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.expErr)
			continue
		}

		// verify the boot chains data file
		pbc, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), "boot-chains"))
		c.Assert(err, IsNil)
		c.Check(cnt, Equals, 0)
		c.Check(pbc, DeepEquals, boot.PredictableBootChains{
			boot.BootChain{
				BrandID:        "my-brand",
				Model:          "my-model-uc20",
				Grade:          "dangerous",
				ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
				AssetChain: []boot.BootAsset{
					{
						Role:   "recovery",
						Name:   shimId,
						Hashes: []string{"shim-hash-1"},
					},
					{
						Role:   "recovery",
						Name:   grubId,
						Hashes: []string{"grub-hash-1"},
					},
				},
				Kernel:         "pc-kernel",
				KernelRevision: "1",
				KernelCmdlines: []string{
					"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
					"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				},
			},
			boot.BootChain{
				BrandID:        "my-brand",
				Model:          "my-model-uc20",
				Grade:          "dangerous",
				ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
				AssetChain: []boot.BootAsset{
					{
						Role:   "recovery",
						Name:   shimId,
						Hashes: []string{"shim-hash-1"},
					},
					{
						Role:   "recovery",
						Name:   grubId,
						Hashes: []string{"grub-hash-1"},
					},
					{
						Role:   "run-mode",
						Name:   runGrubId,
						Hashes: []string{"run-grub-hash-1"},
					},
				},
				Kernel:         "pc-kernel",
				KernelRevision: "500",
				KernelCmdlines: []string{
					"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
				},
			},
		})

		// verify the recovery boot chains
		pbc, cnt, err = boot.ReadBootChains(filepath.Join(dirs.SnapFDEDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), "recovery-boot-chains"))
		c.Assert(err, IsNil)
		c.Check(cnt, Equals, 0)
		c.Check(pbc, DeepEquals, boot.PredictableBootChains{
			boot.BootChain{
				BrandID:        "my-brand",
				Model:          "my-model-uc20",
				Grade:          "dangerous",
				ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
				AssetChain: []boot.BootAsset{
					{
						Role:   "recovery",
						Name:   shimId,
						Hashes: []string{"shim-hash-1"},
					},
					{
						Role:   "recovery",
						Name:   grubId,
						Hashes: []string{"grub-hash-1"},
					},
				},
				Kernel:         "pc-kernel",
				KernelRevision: "1",
				KernelCmdlines: []string{
					"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
					"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				},
			},
		})

		// marker
		marker := filepath.Join(dirs.SnapFDEDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), "sealed-keys")
		c.Check(marker, testutil.FileEquals, "tpm")
	}
}

func (s *sealSuite) testSealToModeenvWithFdeHookHappy(c *C, useTokens bool) {
	model := boottest.MakeMockUC20Model()

	n := 0
	var runFDESetupHookReqs []*fde.SetupRequest
	restore := fdeBackend.MockRunFDESetupHook(func(req *fde.SetupRequest) ([]byte, error) {
		n++
		runFDESetupHookReqs = append(runFDESetupHookReqs, req)

		key := []byte(fmt.Sprintf("key-%v", strconv.Itoa(n)))
		return key, nil
	})
	defer restore()
	dataContainer := secboot.CreateMockBootstrappedContainer()
	saveContainer := secboot.CreateMockBootstrappedContainer()
	savedKeyFiles := make(map[string][]byte)
	savedTokens := make(map[string][]byte)
	restore = fdeBackend.MockSecbootSealKeysWithFDESetupHook(func(runHook fde.RunSetupHookFunc, skrs []secboot.SealKeyRequest, params *secboot.SealKeysWithFDESetupHookParams) error {
		c.Check(params.Model.Model(), Equals, model.Model())
		c.Check(params.Model.Model(), Equals, model.Model())
		for _, skr := range skrs {
			var expectedBootstrappedContainer secboot.BootstrappedContainer
			switch skr.KeyName {
			case "ubuntu-data":
				expectedBootstrappedContainer = dataContainer
			case "ubuntu-save":
				expectedBootstrappedContainer = saveContainer
			}
			c.Assert(skr.BootstrappedContainer, Equals, expectedBootstrappedContainer)
			out, err := runHook(&fde.SetupRequest{
				Key:     []byte{1, 2, 3, 4},
				KeyName: skr.KeyName,
			})
			c.Assert(err, IsNil)
			if len(skr.KeyFile) != 0 {
				savedKeyFiles[skr.KeyFile] = out
			} else {
				var container string
				if skr.BootstrappedContainer == dataContainer {
					container = "data"
				} else if skr.BootstrappedContainer == saveContainer {
					container = "save"
				}
				savedTokens[fmt.Sprintf("%s/%s", container, skr.SlotName)] = out
			}
		}
		return nil
	})
	defer restore()

	params := &boot.SealKeyForBootChainsParams{
		RunModeBootChains: []boot.BootChain{
			{
				BrandID:        model.BrandID(),
				Model:          model.Model(),
				Classic:        model.Classic(),
				Grade:          model.Grade(),
				ModelSignKeyID: model.SignKeyID(),
			},
		},
		RecoveryBootChainsForRunKey: nil,
		RecoveryBootChains: []boot.BootChain{
			{
				BrandID:        model.BrandID(),
				Model:          model.Model(),
				Classic:        model.Classic(),
				Grade:          model.Grade(),
				ModelSignKeyID: model.SignKeyID(),
			},
		},
		RoleToBlName:           nil,
		FactoryReset:           false,
		InstallHostWritableDir: filepath.Join(boot.InstallUbuntuDataDir, "system-data"),
		UseTokens:              useTokens,
	}
	err := boot.SealKeyForBootChains(device.SealingMethodFDESetupHook, dataContainer, saveContainer, params)
	c.Assert(err, IsNil)
	// check that runFDESetupHook was called the expected way
	c.Check(runFDESetupHookReqs, DeepEquals, []*fde.SetupRequest{
		{Key: []byte{1, 2, 3, 4}, KeyName: "ubuntu-data"},
		{Key: []byte{1, 2, 3, 4}, KeyName: "ubuntu-data"},
		{Key: []byte{1, 2, 3, 4}, KeyName: "ubuntu-save"},
	})

	if useTokens {
		c.Check(savedKeyFiles, HasLen, 0)
		for i, p := range []string{
			"data/default",
			"data/default-fallback",
			"save/default-fallback",
		} {
			mockedSealedKey := []byte(fmt.Sprintf("key-%v", strconv.Itoa(i+1)))
			c.Check(savedTokens[p], DeepEquals, mockedSealedKey)
		}
	} else {
		c.Check(savedTokens, HasLen, 0)
		// check that the sealed keys got written to the expected places
		for i, p := range []string{
			filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
			filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
			filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
		} {
			// Check for a valid platform handle, encrypted payload (base64)
			mockedSealedKey := []byte(fmt.Sprintf("key-%v", strconv.Itoa(i+1)))
			c.Check(savedKeyFiles[p], DeepEquals, mockedSealedKey)
		}
	}

	marker := filepath.Join(dirs.SnapFDEDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), "sealed-keys")
	c.Check(marker, testutil.FileEquals, "fde-setup-hook")
}

func (s *sealSuite) TestSealToModeenvWithFdeHookHappyFiles(c *C) {
	const useTokens = false
	s.testSealToModeenvWithFdeHookHappy(c, useTokens)
}

func (s *sealSuite) TestSealToModeenvWithFdeHookHappyTokens(c *C) {
	const useTokens = true
	s.testSealToModeenvWithFdeHookHappy(c, useTokens)
}

func (s *sealSuite) TestSealToModeenvWithFdeHookSad(c *C) {
	model := boottest.MakeMockUC20Model()

	restore := fdeBackend.MockSecbootSealKeysWithFDESetupHook(func(fde.RunSetupHookFunc, []secboot.SealKeyRequest, *secboot.SealKeysWithFDESetupHookParams) error {
		return fmt.Errorf("hook failed")
	})
	defer restore()

	key := secboot.CreateMockBootstrappedContainer()
	saveKey := secboot.CreateMockBootstrappedContainer()

	params := &boot.SealKeyForBootChainsParams{
		RunModeBootChains: []boot.BootChain{
			{
				BrandID:        model.BrandID(),
				Model:          model.Model(),
				Classic:        model.Classic(),
				Grade:          model.Grade(),
				ModelSignKeyID: model.SignKeyID(),
			},
		},
		RecoveryBootChainsForRunKey: nil,
		RecoveryBootChains: []boot.BootChain{
			{
				BrandID:        model.BrandID(),
				Model:          model.Model(),
				Classic:        model.Classic(),
				Grade:          model.Grade(),
				ModelSignKeyID: model.SignKeyID(),
			},
		},
		RoleToBlName:           nil,
		FactoryReset:           false,
		InstallHostWritableDir: filepath.Join(boot.InstallUbuntuDataDir, "system-data"),
	}
	err := boot.SealKeyForBootChains(device.SealingMethodFDESetupHook, key, saveKey, params)
	c.Assert(err, ErrorMatches, "hook failed")
	marker := filepath.Join(dirs.SnapFDEDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), "sealed-keys")
	c.Check(marker, testutil.FileAbsent)
}
