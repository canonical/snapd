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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
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

type mockFDEKeyProtector struct {
	hookFunc fde.RunSetupHookFunc
	keyName  string
}

func (m *mockFDEKeyProtector) ProtectKey(r io.Reader, cleartext, aad []byte) (ciphertext []byte, handle []byte, err error) {
	keyParams := &fde.InitialSetupParams{
		Key:     cleartext,
		KeyName: m.keyName,
	}
	res, err := fde.InitialSetup(m.hookFunc, keyParams)
	if err != nil {
		return nil, nil, err
	}

	return res.EncryptedKey, nil, nil
}

type mockFDEKeyFactory struct {
	hookFunc fde.RunSetupHookFunc
}

func (m *mockFDEKeyFactory) ForKeyName(name string) secboot.KeyProtector {
	return &mockFDEKeyProtector{
		hookFunc: m.hookFunc,
		keyName:  name,
	}
}

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
		sealErr           error
		provisionErr      error
		factoryReset      bool
		shimId            string
		grubId            string
		runGrubId         string
		expErr            string
		expProvisionCalls int
		expSealCalls      int
		disableTokens     bool
		withVolumesAuth   bool
		onCore            bool
	}{
		{
			sealErr: nil, expErr: "",
			expProvisionCalls: 1, expSealCalls: 2,
		},
		{
			sealErr: nil, expErr: "",
			expProvisionCalls: 1, expSealCalls: 2, onCore: true,
		}, {
			sealErr: nil, expErr: "",
			expProvisionCalls: 1, expSealCalls: 2, withVolumesAuth: true,
		}, {
			sealErr: nil, expErr: "",
			expProvisionCalls: 1, expSealCalls: 2, disableTokens: true,
		}, {
			sealErr: nil, expErr: "",
			expProvisionCalls: 1, expSealCalls: 2, disableTokens: true, withVolumesAuth: true,
		}, {
			sealErr: nil,
			// old boot assets
			shimId: "bootx64.efi", grubId: "grubx64.efi",
			expErr:            "",
			expProvisionCalls: 1, expSealCalls: 2,
		}, {
			sealErr: nil, factoryReset: true,
			expProvisionCalls: 1, expSealCalls: 2,
		}, {
			sealErr: nil, factoryReset: true,
			expProvisionCalls: 1, expSealCalls: 2, withVolumesAuth: true,
		}, {
			sealErr: nil, factoryReset: true,
			expProvisionCalls: 1, expSealCalls: 2, disableTokens: true,
		}, {
			sealErr: nil, factoryReset: true,
			expProvisionCalls: 1, expSealCalls: 2,
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

		var model *asserts.Model
		var modelName string
		if tc.onCore {
			model = boottest.MakeMockUC20Model()
			modelName = "my-model-uc20"
		} else {
			model = boottest.MakeMockClassicWithModesModel()
			modelName = "my-model-classic-modes"
		}

		// mock asset cache
		mockAssetsCache(c, rootdir, "grub", []string{
			fmt.Sprintf("%s-shim-hash-1", shimId),
			fmt.Sprintf("%s-grub-hash-1", grubId),
			fmt.Sprintf("%s-run-grub-hash-1", runGrubId),
		})

		// set encryption key
		myKey := secboot.CreateMockBootstrappedContainer()
		myKey2 := secboot.CreateMockBootstrappedContainer()

		var volumesAuth *device.VolumesAuthOptions
		if tc.withVolumesAuth {
			volumesAuth = &device.VolumesAuthOptions{Mode: device.AuthModePassphrase, Passphrase: "test"}
		}

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

		restore = fdeBackend.MockSsecbootFindFreeHandle(func() (uint32, error) {
			return 42, nil
		})
		defer restore()

		// set mock key sealing
		sealKeysCalls := 0
		restore = fdeBackend.MockSecbootSealKeys(func(keys []secboot.SealKeyRequest, params *secboot.SealKeysParams) ([]byte, error) {
			c.Check(params.AllowInsufficientDmaProtection, Equals, tc.onCore)
			c.Assert(provisionCalls, Equals, 1, Commentf("TPM must have been provisioned before"))
			c.Check(params.PCRPolicyCounterHandle, Equals, uint32(42))
			sealKeysCalls++
			switch sealKeysCalls {
			case 1:
				// the run object seals only the ubuntu-data key
				if tc.disableTokens {
					c.Check(params.TPMPolicyAuthKeyFile, Equals, filepath.Join(boot.InstallHostFDESaveDir, "tpm-policy-auth-key"))
				} else {
					c.Check(params.TPMPolicyAuthKeyFile, Equals, "")
				}

				expectedSKR := secboot.SealKeyRequest{BootstrappedContainer: myKey, KeyName: "ubuntu-data", SlotName: "default", BootModes: []string{"run", "recover"}}
				if tc.disableTokens {
					expectedSKR.KeyFile = filepath.Join(rootdir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key")
				}
				c.Check(keys, DeepEquals, []secboot.SealKeyRequest{expectedSKR})
				c.Check(params.KeyRole, Equals, "run+recover")
			case 2:
				// the fallback object seals the ubuntu-data and the ubuntu-save keys
				c.Check(params.TPMPolicyAuthKeyFile, Equals, "")

				expectedDataSKR := secboot.SealKeyRequest{BootstrappedContainer: myKey, KeyName: "ubuntu-data", SlotName: "default-fallback", BootModes: []string{"recover"}}
				expectedSaveSKR := secboot.SealKeyRequest{BootstrappedContainer: myKey2, KeyName: "ubuntu-save", SlotName: "default-fallback", BootModes: []string{"recover", "factory-reset"}}
				if tc.disableTokens {
					expectedDataSKR.KeyFile = filepath.Join(rootdir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key")
					if tc.factoryReset {
						expectedSaveSKR.KeyFile = filepath.Join(rootdir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key.factory-reset")
					} else {
						expectedSaveSKR.KeyFile = filepath.Join(rootdir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key")
					}
				}
				c.Check(keys, DeepEquals, []secboot.SealKeyRequest{expectedDataSKR, expectedSaveSKR})
				c.Check(params.KeyRole, Equals, "recover")
			default:
				c.Errorf("unexpected additional call to secboot.SealKeys (call # %d)", sealKeysCalls)
			}
			c.Assert(params.VolumesAuth, Equals, volumesAuth)
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
			c.Assert(params.ModelParams[0].Model.Model(), Equals, modelName)

			return nil, tc.sealErr
		})
		defer restore()

		recoveryKernel := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
		runKernel := bootloader.NewBootFile(filepath.Join(rootdir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode)

		bootChains := boot.BootChains{
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
		}
		params := &boot.SealKeyForBootChainsParams{
			BootChains:             bootChains,
			FactoryReset:           tc.factoryReset,
			InstallHostWritableDir: filepath.Join(boot.InstallUbuntuDataDir, "system-data"),
			UseTokens:              !tc.disableTokens,
		}
		err := boot.SealKeyForBootChains(device.SealingMethodTPM, myKey, myKey2, nil, volumesAuth, nil, params)

		c.Check(provisionCalls, Equals, tc.expProvisionCalls)
		c.Check(sealKeysCalls, Equals, tc.expSealCalls)
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
				Model:          modelName,
				Classic:        !tc.onCore,
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
				Model:          modelName,
				Classic:        !tc.onCore,
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
				Model:          modelName,
				Classic:        !tc.onCore,
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

	mockFactory := &mockFDEKeyFactory{
		hookFunc: func(req *fde.SetupRequest) ([]byte, error) {
			n++
			runFDESetupHookReqs = append(runFDESetupHookReqs, req)

			output, err := json.Marshal(fde.InitialSetupResult{
				EncryptedKey: []byte(fmt.Sprintf("key-%v", strconv.Itoa(n))),
			})
			if err != nil {
				return nil, err
			}
			return output, nil
		},
	}
	dataContainer := secboot.CreateMockBootstrappedContainer()
	saveContainer := secboot.CreateMockBootstrappedContainer()
	savedKeyFiles := make(map[string][]byte)
	savedTokens := make(map[string][]byte)
	restore := fdeBackend.MockSecbootSealKeysWithProtector(func(kf secboot.KeyProtectorFactory, keys []secboot.SealKeyRequest, params *secboot.SealKeysWithFDESetupHookParams) error {
		c.Check(kf, Equals, mockFactory)
		c.Check(params.Model.Model(), Equals, model.Model())
		c.Check(params.Model.Model(), Equals, model.Model())
		if useTokens {
			c.Check(params.AuxKeyFile, Equals, "")
		} else {
			c.Check(params.AuxKeyFile, Equals, filepath.Join(boot.InstallHostFDESaveDir, "aux-key"))
		}
		c.Check(params.PrimaryKey, DeepEquals, []byte{1, 2, 3, 4})
		for _, skr := range keys {
			var expectedBootstrappedContainer secboot.BootstrappedContainer
			switch skr.KeyName {
			case "ubuntu-data":
				expectedBootstrappedContainer = dataContainer
			case "ubuntu-save":
				expectedBootstrappedContainer = saveContainer
			}
			c.Assert(skr.BootstrappedContainer, Equals, expectedBootstrappedContainer)

			p := kf.ForKeyName(skr.KeyName)
			out, _, err := p.ProtectKey(nil, []byte{1, 2, 3, 4}, nil)
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

	bootChains := boot.BootChains{
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
		RoleToBlName: nil,
	}
	params := &boot.SealKeyForBootChainsParams{
		BootChains:             bootChains,
		FactoryReset:           false,
		InstallHostWritableDir: filepath.Join(boot.InstallUbuntuDataDir, "system-data"),
		UseTokens:              useTokens,
		PrimaryKey:             []byte{1, 2, 3, 4},
		KeyProtectorFactory:    mockFactory,
	}
	err := boot.SealKeyForBootChains(device.SealingMethodFDESetupHook, dataContainer, saveContainer, nil, nil, nil, params)
	c.Assert(err, IsNil)
	// check that runFDESetupHook was called the expected way
	c.Check(runFDESetupHookReqs, DeepEquals, []*fde.SetupRequest{
		{Op: "initial-setup", Key: []byte{1, 2, 3, 4}, KeyName: "ubuntu-data"},
		{Op: "initial-setup", Key: []byte{1, 2, 3, 4}, KeyName: "ubuntu-data"},
		{Op: "initial-setup", Key: []byte{1, 2, 3, 4}, KeyName: "ubuntu-save"},
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

	restore := fdeBackend.MockSecbootSealKeysWithProtector(func(secboot.KeyProtectorFactory, []secboot.SealKeyRequest, *secboot.SealKeysWithFDESetupHookParams) error {
		return fmt.Errorf("hook failed")
	})
	defer restore()

	key := secboot.CreateMockBootstrappedContainer()
	saveKey := secboot.CreateMockBootstrappedContainer()

	bootChains := boot.BootChains{
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
		RoleToBlName: nil,
	}
	params := &boot.SealKeyForBootChainsParams{
		BootChains:             bootChains,
		FactoryReset:           false,
		InstallHostWritableDir: filepath.Join(boot.InstallUbuntuDataDir, "system-data"),
	}
	err := boot.SealKeyForBootChains(device.SealingMethodFDESetupHook, key, saveKey, nil, nil, nil, params)
	c.Assert(err, ErrorMatches, "hook failed")
	marker := filepath.Join(dirs.SnapFDEDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), "sealed-keys")
	c.Check(marker, testutil.FileAbsent)
}
