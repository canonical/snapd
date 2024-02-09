// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch/archtest"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/assets"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

type sealSuite struct {
	testutil.BaseTest
}

var _ = Suite(&sealSuite{})

func (s *sealSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })
	s.AddCleanup(archtest.MockArchitecture("amd64"))
	snippets := []assets.ForEditions{
		{FirstEdition: 1, Snippet: []byte("console=ttyS0 console=tty1 panic=-1")},
	}
	s.AddCleanup(assets.MockSnippetsForEdition("grub.cfg:static-cmdline", snippets))
	s.AddCleanup(assets.MockSnippetsForEdition("grub-recovery.cfg:static-cmdline", snippets))
}

func mockKernelSeedSnap(rev snap.Revision) *seed.Snap {
	return mockNamedKernelSeedSnap(rev, "pc-kernel")
}

func mockNamedKernelSeedSnap(rev snap.Revision, name string) *seed.Snap {
	revAsString := rev.String()
	if rev.Unset() {
		revAsString = "unset"
	}
	return &seed.Snap{
		Path: fmt.Sprintf("/var/lib/snapd/seed/snaps/%v_%v.snap", name, revAsString),
		SideInfo: &snap.SideInfo{
			RealName: name,
			Revision: rev,
		},
		EssentialType: snap.TypeKernel,
	}
}

func mockGadgetSeedSnap(c *C, files [][]string) *seed.Snap {
	mockGadgetYaml := `
volumes:
  volumename:
    bootloader: grub
`

	hasGadgetYaml := false
	for _, entry := range files {
		if entry[0] == "meta/gadget.yaml" {
			hasGadgetYaml = true
		}
	}
	if !hasGadgetYaml {
		files = append(files, []string{"meta/gadget.yaml", mockGadgetYaml})
	}

	gadgetSnapFile := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, files)
	return &seed.Snap{
		Path: gadgetSnapFile,
		SideInfo: &snap.SideInfo{
			RealName: "gadget",
			Revision: snap.R(1),
		},
		EssentialType: snap.TypeGadget,
	}
}

func (s *sealSuite) TestSealKeyToModeenv(c *C) {
	defer boot.MockModeenvLocked()()

	for idx, tc := range []struct {
		sealErr                  error
		provisionErr             error
		factoryReset             bool
		pcrHandleOfKey           uint32
		pcrHandleOfKeyErr        error
		expErr                   string
		expProvisionCalls        int
		expSealCalls             int
		expReleasePCRHandleCalls int
		expPCRHandleOfKeyCalls   int
	}{
		{
			sealErr: nil, expErr: "",
			expProvisionCalls: 1, expSealCalls: 2,
		}, {
			sealErr: nil, factoryReset: true, pcrHandleOfKey: secboot.FallbackObjectPCRPolicyCounterHandle,
			expProvisionCalls: 1, expSealCalls: 2, expPCRHandleOfKeyCalls: 1, expReleasePCRHandleCalls: 1,
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
		defer dirs.SetRootDir("")

		err := createMockGrubCfg(filepath.Join(rootdir, "run/mnt/ubuntu-seed"))
		c.Assert(err, IsNil)

		err = createMockGrubCfg(filepath.Join(rootdir, "run/mnt/ubuntu-boot"))
		c.Assert(err, IsNil)

		model := boottest.MakeMockUC20Model()

		modeenv := &boot.Modeenv{
			RecoverySystem: "20200825",
			CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1"},
				"bootx64.efi": []string{"shim-hash-1"},
			},

			CurrentTrustedBootAssets: boot.BootAssetsMap{
				"grubx64.efi": []string{"run-grub-hash-1"},
			},

			CurrentKernels: []string{"pc-kernel_500.snap"},

			CurrentKernelCommandLines: boot.BootCommandLines{
				"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
			},
			Model:          model.Model(),
			BrandID:        model.BrandID(),
			Grade:          string(model.Grade()),
			ModelSignKeyID: model.SignKeyID(),
		}

		// mock asset cache
		mockAssetsCache(c, rootdir, "grub", []string{
			"bootx64.efi-shim-hash-1",
			"grubx64.efi-grub-hash-1",
			"grubx64.efi-run-grub-hash-1",
		})

		// set encryption key
		myKey := keys.EncryptionKey{}
		myKey2 := keys.EncryptionKey{}
		for i := range myKey {
			myKey[i] = byte(i)
			myKey2[i] = byte(128 + i)
		}

		// set a mock recovery kernel
		readSystemEssentialCalls := 0
		restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
			readSystemEssentialCalls++
			return model, []*seed.Snap{mockKernelSeedSnap(snap.R(1)), mockGadgetSeedSnap(c, nil)}, nil
		})
		defer restore()

		provisionCalls := 0
		restore = boot.MockSecbootProvisionTPM(func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error {
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
		restore = boot.MockSecbootReleasePCRResourceHandles(func(handles ...uint32) error {
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
		})
		defer restore()

		// set mock key sealing
		sealKeysCalls := 0
		restore = boot.MockSecbootSealKeys(func(keys []secboot.SealKeyRequest, params *secboot.SealKeysParams) error {
			c.Assert(provisionCalls, Equals, 1, Commentf("TPM must have been provisioned before"))
			sealKeysCalls++
			switch sealKeysCalls {
			case 1:
				// the run object seals only the ubuntu-data key
				c.Check(params.TPMPolicyAuthKeyFile, Equals, filepath.Join(boot.InstallHostFDESaveDir, "tpm-policy-auth-key"))

				dataKeyFile := filepath.Join(rootdir, "/run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key")
				c.Check(keys, DeepEquals, []secboot.SealKeyRequest{{Key: myKey, KeyName: "ubuntu-data", KeyFile: dataKeyFile}})
				if tc.pcrHandleOfKey == secboot.FallbackObjectPCRPolicyCounterHandle {
					c.Check(params.PCRPolicyCounterHandle, Equals, secboot.AltRunObjectPCRPolicyCounterHandle)
				} else {
					c.Check(params.PCRPolicyCounterHandle, Equals, secboot.RunObjectPCRPolicyCounterHandle)
				}
			case 2:
				// the fallback object seals the ubuntu-data and the ubuntu-save keys
				c.Check(params.TPMPolicyAuthKeyFile, Equals, "")

				dataKeyFile := filepath.Join(rootdir, "/run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key")
				saveKeyFile := filepath.Join(rootdir, "/run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key")
				if tc.factoryReset {
					// during factory reset we use a different key location
					saveKeyFile = filepath.Join(rootdir, "/run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key.factory-reset")
				}
				c.Check(keys, DeepEquals, []secboot.SealKeyRequest{{Key: myKey, KeyName: "ubuntu-data", KeyFile: dataKeyFile}, {Key: myKey2, KeyName: "ubuntu-save", KeyFile: saveKeyFile}})
				if tc.pcrHandleOfKey == secboot.FallbackObjectPCRPolicyCounterHandle {
					c.Check(params.PCRPolicyCounterHandle, Equals, secboot.AltFallbackObjectPCRPolicyCounterHandle)
				} else {
					c.Check(params.PCRPolicyCounterHandle, Equals, secboot.FallbackObjectPCRPolicyCounterHandle)
				}
			default:
				c.Errorf("unexpected additional call to secboot.SealKeys (call # %d)", sealKeysCalls)
			}
			c.Assert(params.ModelParams, HasLen, 1)
			for _, d := range []string{boot.InitramfsSeedEncryptionKeyDir, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/device/fde")} {
				ex, isdir, _ := osutil.DirExists(d)
				c.Check(ex && isdir, Equals, true, Commentf("location %q does not exist or is not a directory", d))
			}

			shim := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-1"), bootloader.RoleRecovery)
			grub := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash-1"), bootloader.RoleRecovery)
			runGrub := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash-1"), bootloader.RoleRunMode)
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

			return tc.sealErr
		})
		defer restore()

		u := mockUnlocker{}
		err = boot.SealKeyToModeenv(myKey, myKey2, model, modeenv, boot.MockSealKeyToModeenvFlags{
			FactoryReset:  tc.factoryReset,
			StateUnlocker: u.unlocker,
		})
		c.Check(u.unlocked, Equals, 1)
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
						Name:   "bootx64.efi",
						Hashes: []string{"shim-hash-1"},
					},
					{
						Role:   "recovery",
						Name:   "grubx64.efi",
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
						Name:   "bootx64.efi",
						Hashes: []string{"shim-hash-1"},
					},
					{
						Role:   "recovery",
						Name:   "grubx64.efi",
						Hashes: []string{"grub-hash-1"},
					},
					{
						Role:   "run-mode",
						Name:   "grubx64.efi",
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
						Name:   "bootx64.efi",
						Hashes: []string{"shim-hash-1"},
					},
					{
						Role:   "recovery",
						Name:   "grubx64.efi",
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

type mockUnlocker struct {
	unlocked int
}

func (u *mockUnlocker) unlocker() func() {
	return func() {
		u.unlocked += 1
	}
}

// TODO:UC20: also test fallback reseal
func (s *sealSuite) TestResealKeyToModeenvWithSystemFallback(c *C) {
	var prevPbc boot.PredictableBootChains
	var prevRecoveryPbc boot.PredictableBootChains

	defer boot.MockModeenvLocked()()

	for idx, tc := range []struct {
		sealedKeys       bool
		reuseRunPbc      bool
		reuseRecoveryPbc bool
		resealErr        error
		err              string
	}{
		{sealedKeys: false, resealErr: nil, err: ""},
		{sealedKeys: true, resealErr: nil, err: ""},
		{sealedKeys: true, resealErr: errors.New("reseal error"), err: "cannot reseal the encryption key: reseal error"},
		{reuseRunPbc: true, reuseRecoveryPbc: true, sealedKeys: true, resealErr: nil, err: ""},
		// recovery boot chain is unchanged
		{reuseRunPbc: false, reuseRecoveryPbc: true, sealedKeys: true, resealErr: nil, err: ""},
		// run boot chain is unchanged
		{reuseRunPbc: true, reuseRecoveryPbc: false, sealedKeys: true, resealErr: nil, err: ""},
	} {
		c.Logf("tc: %v", idx)
		rootdir := c.MkDir()
		dirs.SetRootDir(rootdir)
		defer dirs.SetRootDir("")

		if tc.sealedKeys {
			c.Assert(os.MkdirAll(dirs.SnapFDEDir, 0755), IsNil)
			err := os.WriteFile(filepath.Join(dirs.SnapFDEDir, "sealed-keys"), nil, 0644)
			c.Assert(err, IsNil)

		}

		err := createMockGrubCfg(filepath.Join(rootdir, "run/mnt/ubuntu-seed"))
		c.Assert(err, IsNil)

		err = createMockGrubCfg(filepath.Join(rootdir, "run/mnt/ubuntu-boot"))
		c.Assert(err, IsNil)

		model := boottest.MakeMockUC20Model()

		modeenv := &boot.Modeenv{
			CurrentRecoverySystems: []string{"20200825"},
			CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1"},
				"bootx64.efi": []string{"shim-hash-1", "shim-hash-2"},
			},

			CurrentTrustedBootAssets: boot.BootAssetsMap{
				"grubx64.efi": []string{"run-grub-hash-1", "run-grub-hash-2"},
			},

			CurrentKernels: []string{"pc-kernel_500.snap", "pc-kernel_600.snap"},

			CurrentKernelCommandLines: boot.BootCommandLines{
				"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
			},
			Model:          model.Model(),
			BrandID:        model.BrandID(),
			Grade:          string(model.Grade()),
			ModelSignKeyID: model.SignKeyID(),
		}

		if tc.reuseRunPbc {
			err := boot.WriteBootChains(prevPbc, filepath.Join(dirs.SnapFDEDir, "boot-chains"), 9)
			c.Assert(err, IsNil)
		}
		if tc.reuseRecoveryPbc {
			err = boot.WriteBootChains(prevRecoveryPbc, filepath.Join(dirs.SnapFDEDir, "recovery-boot-chains"), 9)
			c.Assert(err, IsNil)
		}

		// mock asset cache
		mockAssetsCache(c, rootdir, "grub", []string{
			"bootx64.efi-shim-hash-1",
			"bootx64.efi-shim-hash-2",
			"grubx64.efi-grub-hash-1",
			"grubx64.efi-run-grub-hash-1",
			"grubx64.efi-run-grub-hash-2",
		})

		// set a mock recovery kernel
		readSystemEssentialCalls := 0
		restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
			readSystemEssentialCalls++
			return model, []*seed.Snap{mockKernelSeedSnap(snap.R(1)), mockGadgetSeedSnap(c, nil)}, nil
		})
		defer restore()

		// set mock key resealing
		resealKeysCalls := 0
		restore = boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
			c.Check(params.TPMPolicyAuthKeyFile, Equals, filepath.Join(dirs.SnapSaveDir, "device/fde", "tpm-policy-auth-key"))

			resealKeysCalls++
			c.Assert(params.ModelParams, HasLen, 1)

			// shared parameters
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")

			// recovery parameters
			shim := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-1"), bootloader.RoleRecovery)
			shim2 := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-2"), bootloader.RoleRecovery)
			grub := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash-1"), bootloader.RoleRecovery)
			kernel := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
			// run mode parameters
			runGrub := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash-1"), bootloader.RoleRunMode)
			runGrub2 := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash-2"), bootloader.RoleRunMode)
			runKernel := bootloader.NewBootFile(filepath.Join(rootdir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode)
			runKernel2 := bootloader.NewBootFile(filepath.Join(rootdir, "var/lib/snapd/snaps/pc-kernel_600.snap"), "kernel.efi", bootloader.RoleRunMode)

			checkRunParams := func() {
				c.Check(params.KeyFiles, DeepEquals, []string{
					filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
				})
				c.Check(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
					"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
					"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
				})
				// load chains
				c.Assert(params.ModelParams[0].EFILoadChains, HasLen, 6)
				// recovery load chains
				c.Assert(params.ModelParams[0].EFILoadChains[:2], DeepEquals, []*secboot.LoadChain{
					secboot.NewLoadChain(shim,
						secboot.NewLoadChain(grub,
							secboot.NewLoadChain(kernel))),
					secboot.NewLoadChain(shim2,
						secboot.NewLoadChain(grub,
							secboot.NewLoadChain(kernel))),
				})
				// run load chains
				c.Assert(params.ModelParams[0].EFILoadChains[2:4], DeepEquals, []*secboot.LoadChain{
					secboot.NewLoadChain(shim,
						secboot.NewLoadChain(grub,
							secboot.NewLoadChain(runGrub,
								secboot.NewLoadChain(runKernel)),
							secboot.NewLoadChain(runGrub2,
								secboot.NewLoadChain(runKernel)),
						)),
					secboot.NewLoadChain(shim2,
						secboot.NewLoadChain(grub,
							secboot.NewLoadChain(runGrub,
								secboot.NewLoadChain(runKernel)),
							secboot.NewLoadChain(runGrub2,
								secboot.NewLoadChain(runKernel)),
						)),
				})

				c.Assert(params.ModelParams[0].EFILoadChains[4:], DeepEquals, []*secboot.LoadChain{
					secboot.NewLoadChain(shim,
						secboot.NewLoadChain(grub,
							secboot.NewLoadChain(runGrub,
								secboot.NewLoadChain(runKernel2)),
							secboot.NewLoadChain(runGrub2,
								secboot.NewLoadChain(runKernel2)),
						)),
					secboot.NewLoadChain(shim2,
						secboot.NewLoadChain(grub,
							secboot.NewLoadChain(runGrub,
								secboot.NewLoadChain(runKernel2)),
							secboot.NewLoadChain(runGrub2,
								secboot.NewLoadChain(runKernel2)),
						)),
				})
			}

			checkRecoveryParams := func() {
				c.Check(params.KeyFiles, DeepEquals, []string{
					filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
					filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
				})
				c.Check(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
					"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				})
				// load chains
				c.Assert(params.ModelParams[0].EFILoadChains, HasLen, 2)
				// recovery load chains
				c.Assert(params.ModelParams[0].EFILoadChains[:2], DeepEquals, []*secboot.LoadChain{
					secboot.NewLoadChain(shim,
						secboot.NewLoadChain(grub,
							secboot.NewLoadChain(kernel))),
					secboot.NewLoadChain(shim2,
						secboot.NewLoadChain(grub,
							secboot.NewLoadChain(kernel))),
				})
			}

			switch resealKeysCalls {
			case 1:
				if !tc.reuseRunPbc {
					checkRunParams()
				} else if !tc.reuseRecoveryPbc {
					checkRecoveryParams()
				} else {
					c.Errorf("unexpected call to secboot.ResealKeys (call # %d)", resealKeysCalls)
				}
			case 2:
				if !tc.reuseRecoveryPbc {
					checkRecoveryParams()
				} else {
					c.Errorf("unexpected call to secboot.ResealKeys (call # %d)", resealKeysCalls)
				}
			default:
				c.Errorf("unexpected additional call to secboot.ResealKeys (call # %d)", resealKeysCalls)
			}

			return tc.resealErr
		})
		defer restore()

		u := mockUnlocker{}

		// here we don't have unasserted kernels so just set
		// expectReseal to false as it doesn't matter;
		// the behavior with unasserted kernel is tested in
		// boot_test.go specific tests
		options := &boot.ResealToModeenvOptions{
			ExpectReseal: false,
			Force:        false,
		}
		err = boot.ResealKeyToModeenv(rootdir, modeenv, options, u.unlocker)
		if !tc.sealedKeys || (tc.reuseRunPbc && tc.reuseRecoveryPbc) {
			// did nothing
			c.Assert(err, IsNil)
			c.Assert(resealKeysCalls, Equals, 0)
			continue
		}
		c.Check(u.unlocked, Equals, 1)
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}
		if tc.resealErr != nil {
			// mocked error is returned on first reseal
			c.Assert(resealKeysCalls, Equals, 1)
		} else if !tc.reuseRecoveryPbc && !tc.reuseRunPbc {
			// none of the boot chains is reused, so 2 reseals are
			// observed
			c.Assert(resealKeysCalls, Equals, 2)
		} else {
			// one of the boot chains is reused, only one reseal
			c.Assert(resealKeysCalls, Equals, 1)
		}
		if tc.err != "" {
			continue
		}

		// verify the boot chains data file
		pbc, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDir, "boot-chains"))
		c.Assert(err, IsNil)
		if tc.reuseRunPbc {
			c.Assert(cnt, Equals, 9)
		} else {
			c.Assert(cnt, Equals, 1)
		}
		c.Check(pbc, DeepEquals, boot.PredictableBootChains{
			boot.BootChain{
				BrandID:        "my-brand",
				Model:          "my-model-uc20",
				Grade:          "dangerous",
				ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
				AssetChain: []boot.BootAsset{
					{
						Role:   "recovery",
						Name:   "bootx64.efi",
						Hashes: []string{"shim-hash-1", "shim-hash-2"},
					},
					{
						Role:   "recovery",
						Name:   "grubx64.efi",
						Hashes: []string{"grub-hash-1"},
					},
				},
				Kernel:         "pc-kernel",
				KernelRevision: "1",
				KernelCmdlines: []string{
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
						Name:   "bootx64.efi",
						Hashes: []string{"shim-hash-1", "shim-hash-2"},
					},
					{
						Role:   "recovery",
						Name:   "grubx64.efi",
						Hashes: []string{"grub-hash-1"},
					},
					{
						Role:   "run-mode",
						Name:   "grubx64.efi",
						Hashes: []string{"run-grub-hash-1", "run-grub-hash-2"},
					},
				},
				Kernel:         "pc-kernel",
				KernelRevision: "500",
				KernelCmdlines: []string{
					"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
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
						Name:   "bootx64.efi",
						Hashes: []string{"shim-hash-1", "shim-hash-2"},
					},
					{
						Role:   "recovery",
						Name:   "grubx64.efi",
						Hashes: []string{"grub-hash-1"},
					},
					{
						Role:   "run-mode",
						Name:   "grubx64.efi",
						Hashes: []string{"run-grub-hash-1", "run-grub-hash-2"},
					},
				},
				Kernel:         "pc-kernel",
				KernelRevision: "600",
				KernelCmdlines: []string{
					"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
				},
			},
		})
		prevPbc = pbc
		recoveryPbc, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDir, "recovery-boot-chains"))
		c.Assert(err, IsNil)
		if tc.reuseRecoveryPbc {
			c.Check(cnt, Equals, 9)
		} else {
			c.Check(cnt, Equals, 1)
		}
		prevRecoveryPbc = recoveryPbc
	}
}

func (s *sealSuite) TestResealKeyToModeenvRecoveryKeysForGoodSystemsOnly(c *C) {
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)
	defer dirs.SetRootDir("")

	c.Assert(os.MkdirAll(dirs.SnapFDEDir, 0755), IsNil)
	err := os.WriteFile(filepath.Join(dirs.SnapFDEDir, "sealed-keys"), nil, 0644)
	c.Assert(err, IsNil)

	err = createMockGrubCfg(filepath.Join(rootdir, "run/mnt/ubuntu-seed"))
	c.Assert(err, IsNil)

	err = createMockGrubCfg(filepath.Join(rootdir, "run/mnt/ubuntu-boot"))
	c.Assert(err, IsNil)

	model := boottest.MakeMockUC20Model()

	modeenv := &boot.Modeenv{
		// where 1234 is being tried
		CurrentRecoverySystems: []string{"20200825", "1234"},
		// 20200825 has known to be good
		GoodRecoverySystems: []string{"20200825"},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"grubx64.efi": []string{"grub-hash"},
			"bootx64.efi": []string{"shim-hash"},
		},

		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"grubx64.efi": []string{"run-grub-hash"},
		},

		CurrentKernels: []string{"pc-kernel_500.snap"},

		CurrentKernelCommandLines: boot.BootCommandLines{
			"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		},
		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}

	// mock asset cache
	mockAssetsCache(c, rootdir, "grub", []string{
		"bootx64.efi-shim-hash",
		"grubx64.efi-grub-hash",
		"grubx64.efi-run-grub-hash",
	})

	// set a mock recovery kernel
	readSystemEssentialCalls := 0
	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		readSystemEssentialCalls++
		kernelRev := 1
		if label == "1234" {
			kernelRev = 999
		}
		return model, []*seed.Snap{mockKernelSeedSnap(snap.R(kernelRev)), mockGadgetSeedSnap(c, nil)}, nil
	})
	defer restore()

	defer boot.MockModeenvLocked()()

	// set mock key resealing
	resealKeysCalls := 0
	restore = boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		c.Check(params.TPMPolicyAuthKeyFile, Equals, filepath.Join(dirs.SnapSaveDir, "device/fde", "tpm-policy-auth-key"))

		resealKeysCalls++
		c.Assert(params.ModelParams, HasLen, 1)

		// shared parameters
		c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")
		c.Logf("got:")
		for _, ch := range params.ModelParams[0].EFILoadChains {
			printChain(c, ch, "-")
		}
		switch resealKeysCalls {
		case 1: // run key
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
			})
			c.Assert(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=recover snapd_recovery_system=1234 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
			})
			// load chains
			c.Assert(params.ModelParams[0].EFILoadChains, HasLen, 3)
		case 2: // recovery keys
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
			})
			c.Assert(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
			})
			// load chains
			c.Assert(params.ModelParams[0].EFILoadChains, HasLen, 1)
		default:
			c.Errorf("unexpected additional call to secboot.ResealKeys (call # %d)", resealKeysCalls)
		}

		// recovery parameters
		shim := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash"), bootloader.RoleRecovery)
		grub := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash"), bootloader.RoleRecovery)
		kernelGoodRecovery := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
		// kernel from a tried recovery system
		kernelTriedRecovery := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_999.snap", "kernel.efi", bootloader.RoleRecovery)
		// run mode parameters
		runGrub := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash"), bootloader.RoleRunMode)
		runKernel := bootloader.NewBootFile(filepath.Join(rootdir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode)

		switch resealKeysCalls {
		case 1: // run load chain
			c.Assert(params.ModelParams[0].EFILoadChains, DeepEquals, []*secboot.LoadChain{
				secboot.NewLoadChain(shim,
					secboot.NewLoadChain(grub,
						secboot.NewLoadChain(kernelGoodRecovery),
					)),
				secboot.NewLoadChain(shim,
					secboot.NewLoadChain(grub,
						secboot.NewLoadChain(kernelTriedRecovery),
					)),
				secboot.NewLoadChain(shim,
					secboot.NewLoadChain(grub,
						secboot.NewLoadChain(runGrub,
							secboot.NewLoadChain(runKernel)),
					)),
			})
		case 2: // recovery load chains
			c.Assert(params.ModelParams[0].EFILoadChains, DeepEquals, []*secboot.LoadChain{
				secboot.NewLoadChain(shim,
					secboot.NewLoadChain(grub,
						secboot.NewLoadChain(kernelGoodRecovery),
					)),
			})
		}

		return nil
	})
	defer restore()

	// here we don't have unasserted kernels so just set
	// expectReseal to false as it doesn't matter;
	// the behavior with unasserted kernel is tested in
	// boot_test.go specific tests
	options := &boot.ResealToModeenvOptions{
		ExpectReseal: false,
		Force:        false,
	}
	err = boot.ResealKeyToModeenv(rootdir, modeenv, options, nil)
	c.Assert(err, IsNil)
	c.Assert(resealKeysCalls, Equals, 2)

	// verify the boot chains data file for run key
	runPbc, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDir, "boot-chains"))
	c.Assert(err, IsNil)
	c.Assert(cnt, Equals, 1)
	c.Check(runPbc, DeepEquals, boot.PredictableBootChains{
		boot.BootChain{
			BrandID:        "my-brand",
			Model:          "my-model-uc20",
			Grade:          "dangerous",
			ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
			AssetChain: []boot.BootAsset{
				{
					Role:   "recovery",
					Name:   "bootx64.efi",
					Hashes: []string{"shim-hash"},
				},
				{
					Role:   "recovery",
					Name:   "grubx64.efi",
					Hashes: []string{"grub-hash"},
				},
			},
			Kernel:         "pc-kernel",
			KernelRevision: "1",
			KernelCmdlines: []string{
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
			},
		},
		// includes the tried system
		boot.BootChain{
			BrandID:        "my-brand",
			Model:          "my-model-uc20",
			Grade:          "dangerous",
			ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
			AssetChain: []boot.BootAsset{
				{
					Role:   "recovery",
					Name:   "bootx64.efi",
					Hashes: []string{"shim-hash"},
				},
				{
					Role:   "recovery",
					Name:   "grubx64.efi",
					Hashes: []string{"grub-hash"},
				},
			},
			Kernel:         "pc-kernel",
			KernelRevision: "999",
			KernelCmdlines: []string{
				// but only the recover mode
				"snapd_recovery_mode=recover snapd_recovery_system=1234 console=ttyS0 console=tty1 panic=-1",
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
					Name:   "bootx64.efi",
					Hashes: []string{"shim-hash"},
				},
				{
					Role:   "recovery",
					Name:   "grubx64.efi",
					Hashes: []string{"grub-hash"},
				},
				{
					Role:   "run-mode",
					Name:   "grubx64.efi",
					Hashes: []string{"run-grub-hash"},
				},
			},
			Kernel:         "pc-kernel",
			KernelRevision: "500",
			KernelCmdlines: []string{
				"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
			},
		},
	})
	// recovery boot chains
	recoveryPbc, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDir, "recovery-boot-chains"))
	c.Assert(err, IsNil)
	c.Assert(cnt, Equals, 1)
	c.Check(recoveryPbc, DeepEquals, boot.PredictableBootChains{
		// only one entry for a recovery system that is known to be good
		boot.BootChain{
			BrandID:        "my-brand",
			Model:          "my-model-uc20",
			Grade:          "dangerous",
			ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
			AssetChain: []boot.BootAsset{
				{
					Role:   "recovery",
					Name:   "bootx64.efi",
					Hashes: []string{"shim-hash"},
				},
				{
					Role:   "recovery",
					Name:   "grubx64.efi",
					Hashes: []string{"grub-hash"},
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
}

func (s *sealSuite) TestResealKeyToModeenvFallbackCmdline(c *C) {
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)
	defer dirs.SetRootDir("")

	model := boottest.MakeMockUC20Model()

	c.Assert(os.MkdirAll(dirs.SnapFDEDir, 0755), IsNil)
	err := os.WriteFile(filepath.Join(dirs.SnapFDEDir, "sealed-keys"), nil, 0644)
	c.Assert(err, IsNil)

	modeenv := &boot.Modeenv{
		CurrentRecoverySystems: []string{"20200825"},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": []string{"asset-hash-1"},
		},

		CurrentKernels: []string{"pc-kernel_500.snap"},

		// as if it is unset yet
		CurrentKernelCommandLines: nil,

		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}

	err = boot.WriteBootChains(nil, filepath.Join(dirs.SnapFDEDir, "boot-chains"), 9)
	c.Assert(err, IsNil)
	// mock asset cache
	mockAssetsCache(c, rootdir, "trusted", []string{
		"asset-asset-hash-1",
	})

	// match one of current kernels
	runKernelBf := bootloader.NewBootFile("/var/lib/snapd/snap/pc-kernel_500.snap", "kernel.efi", bootloader.RoleRunMode)
	// match the seed kernel
	recoveryKernelBf := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)

	bootdir := c.MkDir()
	mtbl := bootloadertest.Mock("trusted", bootdir).WithTrustedAssets()
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
	defer bootloader.Force(nil)

	// set a mock recovery kernel
	readSystemEssentialCalls := 0
	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		readSystemEssentialCalls++
		return model, []*seed.Snap{mockKernelSeedSnap(snap.R(1)), mockGadgetSeedSnap(c, nil)}, nil
	})
	defer restore()

	defer boot.MockModeenvLocked()()

	// set mock key resealing
	resealKeysCalls := 0
	restore = boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealKeysCalls++
		c.Assert(params.ModelParams, HasLen, 1)
		c.Logf("reseal: %+v", params)
		switch resealKeysCalls {
		case 1:
			c.Assert(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 static cmdline",
				"snapd_recovery_mode=run static cmdline",
			})
		case 2:
			c.Assert(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 static cmdline",
			})
		default:
			c.Fatalf("unexpected number of reseal calls, %v", params)
		}
		return nil
	})
	defer restore()

	options := &boot.ResealToModeenvOptions{
		ExpectReseal: false,
		Force:        false,
	}
	err = boot.ResealKeyToModeenv(rootdir, modeenv, options, nil)
	c.Assert(err, IsNil)
	c.Assert(resealKeysCalls, Equals, 2)

	// verify the boot chains data file
	pbc, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDir, "boot-chains"))
	c.Assert(err, IsNil)
	c.Assert(cnt, Equals, 10)
	c.Check(pbc, DeepEquals, boot.PredictableBootChains{
		boot.BootChain{
			BrandID:        "my-brand",
			Model:          "my-model-uc20",
			Grade:          "dangerous",
			ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
			AssetChain: []boot.BootAsset{
				{
					Role:   "recovery",
					Name:   "asset",
					Hashes: []string{"asset-hash-1"},
				},
			},
			Kernel:         "pc-kernel",
			KernelRevision: "1",
			KernelCmdlines: []string{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 static cmdline",
			},
		},
		boot.BootChain{
			BrandID:        "my-brand",
			Model:          "my-model-uc20",
			Grade:          "dangerous",
			ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
			AssetChain: []boot.BootAsset{
				{
					Role:   "run-mode",
					Name:   "asset",
					Hashes: []string{"asset-hash-1"},
				},
			},
			Kernel:         "pc-kernel",
			KernelRevision: "500",
			KernelCmdlines: []string{
				"snapd_recovery_mode=run static cmdline",
			},
		},
	})
}

func (s *sealSuite) TestRecoveryBootChainsForSystems(c *C) {
	for _, tc := range []struct {
		desc                    string
		assetsMap               boot.BootAssetsMap
		recoverySystems         []string
		modesForSystems         map[string][]string
		undefinedKernel         bool
		gadgetFilesForSystem    map[string][][]string
		expectedAssets          []boot.BootAsset
		expectedKernelRevs      []int
		expectedBootChainsCount int
		// in the order of boot chains
		expectedCmdlines [][]string
		err              string
	}{
		{
			desc:            "transition sequences",
			recoverySystems: []string{"20200825"},
			modesForSystems: map[string][]string{"20200825": {boot.ModeRecover, boot.ModeFactoryReset}},
			assetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1", "grub-hash-2"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			expectedAssets: []boot.BootAsset{
				{Role: bootloader.RoleRecovery, Name: "bootx64.efi", Hashes: []string{"shim-hash-1"}},
				{Role: bootloader.RoleRecovery, Name: "grubx64.efi", Hashes: []string{"grub-hash-1", "grub-hash-2"}},
			},
			expectedKernelRevs: []int{1},
			expectedCmdlines: [][]string{{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
			}},
		},
		{
			desc:            "two systems",
			recoverySystems: []string{"20200825", "20200831"},
			modesForSystems: map[string][]string{
				"20200825": {boot.ModeRecover, boot.ModeFactoryReset},
				"20200831": {boot.ModeRecover, boot.ModeFactoryReset},
			},
			assetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1", "grub-hash-2"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			expectedAssets: []boot.BootAsset{
				{Role: bootloader.RoleRecovery, Name: "bootx64.efi", Hashes: []string{"shim-hash-1"}},
				{Role: bootloader.RoleRecovery, Name: "grubx64.efi", Hashes: []string{"grub-hash-1", "grub-hash-2"}},
			},
			expectedKernelRevs: []int{1, 3},
			expectedCmdlines: [][]string{{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
			}, {
				"snapd_recovery_mode=recover snapd_recovery_system=20200831 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200831 console=ttyS0 console=tty1 panic=-1",
			}},
		},
		{
			desc:            "non transition sequence",
			recoverySystems: []string{"20200825"},
			modesForSystems: map[string][]string{"20200825": {boot.ModeRecover, boot.ModeFactoryReset}},
			assetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			expectedAssets: []boot.BootAsset{
				{Role: bootloader.RoleRecovery, Name: "bootx64.efi", Hashes: []string{"shim-hash-1"}},
				{Role: bootloader.RoleRecovery, Name: "grubx64.efi", Hashes: []string{"grub-hash-1"}},
			},
			expectedKernelRevs: []int{1},
			expectedCmdlines: [][]string{{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
			}},
		},
		{
			desc:            "two systems with command lines",
			recoverySystems: []string{"20200825", "20200831"},
			modesForSystems: map[string][]string{
				"20200825": {boot.ModeRecover, boot.ModeFactoryReset},
				"20200831": {boot.ModeRecover, boot.ModeFactoryReset},
			},
			assetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1", "grub-hash-2"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			expectedAssets: []boot.BootAsset{
				{Role: bootloader.RoleRecovery, Name: "bootx64.efi", Hashes: []string{"shim-hash-1"}},
				{Role: bootloader.RoleRecovery, Name: "grubx64.efi", Hashes: []string{"grub-hash-1", "grub-hash-2"}},
			},
			gadgetFilesForSystem: map[string][][]string{
				"20200825": {
					{"cmdline.extra", "extra for 20200825"},
				},
				"20200831": {
					// TODO: make it a cmdline.full
					{"cmdline.extra", "some-extra-for-20200831"},
				},
			},
			expectedKernelRevs: []int{1, 3},
			expectedCmdlines: [][]string{{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1 extra for 20200825",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1 extra for 20200825",
			}, {
				"snapd_recovery_mode=recover snapd_recovery_system=20200831 console=ttyS0 console=tty1 panic=-1 some-extra-for-20200831",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200831 console=ttyS0 console=tty1 panic=-1 some-extra-for-20200831",
			}},
		},
		{
			desc:            "three systems, one with different model",
			recoverySystems: []string{"20200825", "20200831", "off-model"},
			modesForSystems: map[string][]string{
				"20200825":  {boot.ModeRecover, boot.ModeFactoryReset},
				"20200831":  {boot.ModeRecover, boot.ModeFactoryReset},
				"off-model": {boot.ModeRecover, boot.ModeFactoryReset},
			},
			assetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1", "grub-hash-2"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			expectedAssets: []boot.BootAsset{
				{Role: bootloader.RoleRecovery, Name: "bootx64.efi", Hashes: []string{"shim-hash-1"}},
				{Role: bootloader.RoleRecovery, Name: "grubx64.efi", Hashes: []string{"grub-hash-1", "grub-hash-2"}},
			},
			expectedKernelRevs: []int{1, 3},
			expectedCmdlines: [][]string{{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
			}, {
				"snapd_recovery_mode=recover snapd_recovery_system=20200831 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200831 console=ttyS0 console=tty1 panic=-1",
			}},
			expectedBootChainsCount: 2,
		},
		{
			desc:            "two systems, one with different modes",
			recoverySystems: []string{"20200825", "20200831"},
			modesForSystems: map[string][]string{
				"20200825": {boot.ModeRecover, boot.ModeFactoryReset},
				"20200831": {boot.ModeRecover},
			},
			assetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1", "grub-hash-2"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			expectedAssets: []boot.BootAsset{
				{Role: bootloader.RoleRecovery, Name: "bootx64.efi", Hashes: []string{"shim-hash-1"}},
				{Role: bootloader.RoleRecovery, Name: "grubx64.efi", Hashes: []string{"grub-hash-1", "grub-hash-2"}},
			},
			expectedKernelRevs: []int{1, 3},
			expectedCmdlines: [][]string{{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
			}, {
				"snapd_recovery_mode=recover snapd_recovery_system=20200831 console=ttyS0 console=tty1 panic=-1",
			}},
			expectedBootChainsCount: 2,
		},
		{
			desc:            "invalid recovery system label",
			recoverySystems: []string{"0"},
			modesForSystems: map[string][]string{"0": {boot.ModeRecover}},
			err:             `cannot read system "0" seed: invalid system seed`,
		},
		{
			desc:            "missing modes for a system",
			recoverySystems: []string{"20200825"},
			modesForSystems: map[string][]string{"other": {boot.ModeRecover}},
			err:             `internal error: no modes for system "20200825"`,
		},
	} {
		c.Logf("tc: %q", tc.desc)
		rootdir := c.MkDir()
		dirs.SetRootDir(rootdir)
		defer dirs.SetRootDir("")

		model := boottest.MakeMockUC20Model()

		// set recovery kernel
		restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
			systemModel := model
			kernelRev := 1
			switch label {
			case "20200825":
				// nothing special
			case "20200831":
				kernelRev = 3
			case "off-model":
				systemModel = boottest.MakeMockUC20Model(map[string]interface{}{
					"model": "model-mismatch-uc20",
				})
			default:
				return nil, nil, fmt.Errorf("invalid system seed")
			}
			return systemModel, []*seed.Snap{mockKernelSeedSnap(snap.R(kernelRev)), mockGadgetSeedSnap(c, tc.gadgetFilesForSystem[label])}, nil
		})
		defer restore()

		grubDir := filepath.Join(rootdir, "run/mnt/ubuntu-seed")
		err := createMockGrubCfg(grubDir)
		c.Assert(err, IsNil)

		bl, err := bootloader.Find(grubDir, &bootloader.Options{Role: bootloader.RoleRecovery})
		c.Assert(err, IsNil)
		tbl, ok := bl.(bootloader.TrustedAssetsBootloader)
		c.Assert(ok, Equals, true)

		modeenv := &boot.Modeenv{
			CurrentTrustedRecoveryBootAssets: tc.assetsMap,

			BrandID:        model.BrandID(),
			Model:          model.Model(),
			ModelSignKeyID: model.SignKeyID(),
			Grade:          string(model.Grade()),
		}

		includeTryModel := false
		bc, err := boot.RecoveryBootChainsForSystems(tc.recoverySystems, tc.modesForSystems, tbl, modeenv, includeTryModel, dirs.SnapSeedDir)
		if tc.err == "" {
			c.Assert(err, IsNil)
			if tc.expectedBootChainsCount == 0 {
				// usually there is a boot chain for each recovery system
				c.Assert(bc, HasLen, len(tc.recoverySystems))
			} else {
				c.Assert(bc, HasLen, tc.expectedBootChainsCount)
			}
			c.Assert(tc.expectedCmdlines, HasLen, len(bc), Commentf("broken test, expected command lines must be of the same length as recovery systems and recovery boot chains"))
			for i, chain := range bc {
				c.Assert(chain.AssetChain, DeepEquals, tc.expectedAssets)
				c.Assert(chain.Kernel, Equals, "pc-kernel")
				expectedKernelRev := tc.expectedKernelRevs[i]
				c.Assert(chain.KernelRevision, Equals, fmt.Sprintf("%d", expectedKernelRev))
				c.Assert(chain.KernelBootFile(), DeepEquals, bootloader.BootFile{
					Snap: fmt.Sprintf("/var/lib/snapd/seed/snaps/pc-kernel_%d.snap", expectedKernelRev),
					Path: "kernel.efi",
					Role: bootloader.RoleRecovery,
				})
				c.Assert(chain.KernelCmdlines, DeepEquals, tc.expectedCmdlines[i])
			}
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}

	}

}

func createMockGrubCfg(baseDir string) error {
	cfg := filepath.Join(baseDir, "EFI/ubuntu/grub.cfg")
	if err := os.MkdirAll(filepath.Dir(cfg), 0755); err != nil {
		return err
	}
	return os.WriteFile(cfg, []byte("# Snapd-Boot-Config-Edition: 1\n"), 0644)
}

func (s *sealSuite) TestSealKeyModelParams(c *C) {
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)
	defer dirs.SetRootDir("")

	model := boottest.MakeMockUC20Model()

	roleToBlName := map[bootloader.Role]string{
		bootloader.RoleRecovery: "grub",
		bootloader.RoleRunMode:  "grub",
	}
	// mock asset cache
	mockAssetsCache(c, rootdir, "grub", []string{
		"shim-shim-hash",
		"loader-loader-hash1",
		"loader-loader-hash2",
	})

	oldmodel := boottest.MakeMockUC20Model(map[string]interface{}{
		"model":     "old-model-uc20",
		"timestamp": "2019-10-01T08:00:00+00:00",
	})

	// old recovery
	oldrc := boot.BootChain{
		BrandID:        oldmodel.BrandID(),
		Model:          oldmodel.Model(),
		Grade:          oldmodel.Grade(),
		ModelSignKeyID: oldmodel.SignKeyID(),
		AssetChain: []boot.BootAsset{
			{Name: "shim", Role: bootloader.RoleRecovery, Hashes: []string{"shim-hash"}},
			{Name: "loader", Role: bootloader.RoleRecovery, Hashes: []string{"loader-hash1"}},
		},
		KernelCmdlines: []string{"panic=1", "oldrc"},
	}
	oldkbf := bootloader.BootFile{Snap: "pc-kernel_1.snap"}
	oldrc.SetKernelBootFile(oldkbf)

	// recovery
	rc1 := boot.BootChain{
		BrandID:        model.BrandID(),
		Model:          model.Model(),
		Grade:          model.Grade(),
		ModelSignKeyID: model.SignKeyID(),
		AssetChain: []boot.BootAsset{
			{Name: "shim", Role: bootloader.RoleRecovery, Hashes: []string{"shim-hash"}},
			{Name: "loader", Role: bootloader.RoleRecovery, Hashes: []string{"loader-hash1"}},
		},
		KernelCmdlines: []string{"panic=1", "rc1"},
	}
	rc1kbf := bootloader.BootFile{Snap: "pc-kernel_10.snap"}
	rc1.SetKernelBootFile(rc1kbf)

	// run system
	runc1 := boot.BootChain{
		BrandID:        model.BrandID(),
		Model:          model.Model(),
		Grade:          model.Grade(),
		ModelSignKeyID: model.SignKeyID(),
		AssetChain: []boot.BootAsset{
			{Name: "shim", Role: bootloader.RoleRecovery, Hashes: []string{"shim-hash"}},
			{Name: "loader", Role: bootloader.RoleRecovery, Hashes: []string{"loader-hash1"}},
			{Name: "loader", Role: bootloader.RoleRunMode, Hashes: []string{"loader-hash2"}},
		},
		KernelCmdlines: []string{"panic=1", "runc1"},
	}
	runc1kbf := bootloader.BootFile{Snap: "pc-kernel_50.snap"}
	runc1.SetKernelBootFile(runc1kbf)

	pbc := boot.ToPredictableBootChains([]boot.BootChain{rc1, runc1, oldrc})

	shim := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/shim-shim-hash"), bootloader.RoleRecovery)
	loader1 := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/loader-loader-hash1"), bootloader.RoleRecovery)
	loader2 := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/loader-loader-hash2"), bootloader.RoleRunMode)

	params, err := boot.SealKeyModelParams(pbc, roleToBlName)
	c.Assert(err, IsNil)
	c.Check(params, HasLen, 2)
	c.Check(params[0].Model.Model(), Equals, model.Model())
	// NB: merging of lists makes panic=1 appear once
	c.Check(params[0].KernelCmdlines, DeepEquals, []string{"panic=1", "rc1", "runc1"})

	c.Check(params[0].EFILoadChains, DeepEquals, []*secboot.LoadChain{
		secboot.NewLoadChain(shim,
			secboot.NewLoadChain(loader1,
				secboot.NewLoadChain(rc1kbf))),
		secboot.NewLoadChain(shim,
			secboot.NewLoadChain(loader1,
				secboot.NewLoadChain(loader2,
					secboot.NewLoadChain(runc1kbf)))),
	})

	c.Check(params[1].Model.Model(), Equals, oldmodel.Model())
	c.Check(params[1].KernelCmdlines, DeepEquals, []string{"oldrc", "panic=1"})
	c.Check(params[1].EFILoadChains, DeepEquals, []*secboot.LoadChain{
		secboot.NewLoadChain(shim,
			secboot.NewLoadChain(loader1,
				secboot.NewLoadChain(oldkbf))),
	})
}

func (s *sealSuite) TestIsResealNeeded(c *C) {
	if os.Geteuid() == 0 {
		c.Skip("the test cannot be run by the root user")
	}

	chains := []boot.BootChain{
		{
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "signed",
			ModelSignKeyID: "my-key-id",
			AssetChain: []boot.BootAsset{
				// hashes will be sorted
				{Role: bootloader.RoleRecovery, Name: "shim", Hashes: []string{"x", "y"}},
				{Role: bootloader.RoleRecovery, Name: "loader", Hashes: []string{"c", "d"}},
				{Role: bootloader.RoleRunMode, Name: "loader", Hashes: []string{"z", "x"}},
			},
			Kernel:         "pc-kernel-other",
			KernelRevision: "2345",
			KernelCmdlines: []string{`snapd_recovery_mode=run foo`},
		}, {
			BrandID:        "mybrand",
			Model:          "foo",
			Grade:          "dangerous",
			ModelSignKeyID: "my-key-id",
			AssetChain: []boot.BootAsset{
				// hashes will be sorted
				{Role: bootloader.RoleRecovery, Name: "shim", Hashes: []string{"y", "x"}},
				{Role: bootloader.RoleRecovery, Name: "loader", Hashes: []string{"c", "d"}},
			},
			Kernel:         "pc-kernel-recovery",
			KernelRevision: "1234",
			KernelCmdlines: []string{`snapd_recovery_mode=recover foo`},
		},
	}

	pbc := boot.ToPredictableBootChains(chains)

	rootdir := c.MkDir()
	err := boot.WriteBootChains(pbc, filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"), 2)
	c.Assert(err, IsNil)

	options := &boot.ResealToModeenvOptions{
		ExpectReseal: false,
		Force:        false,
	}
	needed, _, err := boot.IsResealNeeded(pbc, filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"), options)
	c.Assert(err, IsNil)
	c.Check(needed, Equals, false)

	otherchain := []boot.BootChain{pbc[0]}
	needed, cnt, err := boot.IsResealNeeded(otherchain, filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"), options)
	c.Assert(err, IsNil)
	// chains are different
	c.Check(needed, Equals, true)
	c.Check(cnt, Equals, 3)

	// boot-chains does not exist, we cannot compare so advise to reseal
	otherRootdir := c.MkDir()
	needed, cnt, err = boot.IsResealNeeded(otherchain, filepath.Join(dirs.SnapFDEDirUnder(otherRootdir), "boot-chains"), options)
	c.Assert(err, IsNil)
	c.Check(needed, Equals, true)
	c.Check(cnt, Equals, 1)

	// exists but cannot be read
	c.Assert(os.Chmod(filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"), 0000), IsNil)
	defer os.Chmod(filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"), 0755)
	needed, _, err = boot.IsResealNeeded(otherchain, filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"), options)
	c.Assert(err, ErrorMatches, "cannot open existing boot chains data file: open .*/boot-chains: permission denied")
	c.Check(needed, Equals, false)

	// unrevisioned kernel chain
	unrevchain := []boot.BootChain{pbc[0], pbc[1]}
	unrevchain[1].KernelRevision = ""
	// write on disk
	bootChainsFile := filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains")
	err = boot.WriteBootChains(unrevchain, bootChainsFile, 2)
	c.Assert(err, IsNil)

	needed, cnt, err = boot.IsResealNeeded(pbc, bootChainsFile, options)
	c.Assert(err, IsNil)
	c.Check(needed, Equals, true)
	c.Check(cnt, Equals, 3)

	// cases falling back to expectReseal
	needed, _, err = boot.IsResealNeeded(unrevchain, bootChainsFile, options)
	c.Assert(err, IsNil)
	c.Check(needed, Equals, false)

	options = &boot.ResealToModeenvOptions{
		ExpectReseal: true,
		Force:        false,
	}
	needed, cnt, err = boot.IsResealNeeded(unrevchain, bootChainsFile, options)
	c.Assert(err, IsNil)
	c.Check(needed, Equals, true)
	c.Check(cnt, Equals, 3)
}

func (s *sealSuite) TestSealToModeenvWithFdeHookHappy(c *C) {
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)
	defer dirs.SetRootDir("")
	model := boottest.MakeMockUC20Model()

	n := 0
	var runFDESetupHookReqs []*fde.SetupRequest
	restore := boot.MockRunFDESetupHook(func(req *fde.SetupRequest) ([]byte, error) {
		n++
		runFDESetupHookReqs = append(runFDESetupHookReqs, req)

		key := []byte(fmt.Sprintf("key-%v", strconv.Itoa(n)))
		return key, nil
	})
	defer restore()
	keyToSave := make(map[string][]byte)
	restore = boot.MockSecbootSealKeysWithFDESetupHook(func(runHook fde.RunSetupHookFunc, skrs []secboot.SealKeyRequest, params *secboot.SealKeysWithFDESetupHookParams) error {
		c.Check(params.Model.Model(), Equals, model.Model())
		c.Check(params.AuxKeyFile, Equals, filepath.Join(boot.InstallHostFDESaveDir, "aux-key"))
		for _, skr := range skrs {
			out, err := runHook(&fde.SetupRequest{
				Key:     skr.Key,
				KeyName: skr.KeyName,
			})
			c.Assert(err, IsNil)
			keyToSave[skr.KeyFile] = out
		}
		return nil
	})
	defer restore()

	modeenv := &boot.Modeenv{
		RecoverySystem: "20200825",
		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	key := keys.EncryptionKey{1, 2, 3, 4}
	saveKey := keys.EncryptionKey{5, 6, 7, 8}

	defer boot.MockModeenvLocked()()

	err := boot.SealKeyToModeenv(key, saveKey, model, modeenv, boot.MockSealKeyToModeenvFlags{HasFDESetupHook: true})
	c.Assert(err, IsNil)
	// check that runFDESetupHook was called the expected way
	c.Check(runFDESetupHookReqs, DeepEquals, []*fde.SetupRequest{
		{Key: key, KeyName: "ubuntu-data"},
		{Key: key, KeyName: "ubuntu-data"},
		{Key: saveKey, KeyName: "ubuntu-save"},
	})
	// check that the sealed keys got written to the expected places
	for i, p := range []string{
		filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
		filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
		filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
	} {
		// Check for a valid platform handle, encrypted payload (base64)
		mockedSealedKey := []byte(fmt.Sprintf("key-%v", strconv.Itoa(i+1)))
		c.Check(keyToSave[p], DeepEquals, mockedSealedKey)
	}

	marker := filepath.Join(dirs.SnapFDEDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), "sealed-keys")
	c.Check(marker, testutil.FileEquals, "fde-setup-hook")
}

func (s *sealSuite) TestSealToModeenvWithFdeHookSad(c *C) {
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)
	defer dirs.SetRootDir("")

	restore := boot.MockSecbootSealKeysWithFDESetupHook(func(fde.RunSetupHookFunc, []secboot.SealKeyRequest, *secboot.SealKeysWithFDESetupHookParams) error {
		return fmt.Errorf("hook failed")
	})
	defer restore()

	modeenv := &boot.Modeenv{
		RecoverySystem: "20200825",
	}
	key := keys.EncryptionKey{1, 2, 3, 4}
	saveKey := keys.EncryptionKey{5, 6, 7, 8}

	defer boot.MockModeenvLocked()()

	model := boottest.MakeMockUC20Model()
	err := boot.SealKeyToModeenv(key, saveKey, model, modeenv, boot.MockSealKeyToModeenvFlags{HasFDESetupHook: true})
	c.Assert(err, ErrorMatches, "hook failed")
	marker := filepath.Join(dirs.SnapFDEDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), "sealed-keys")
	c.Check(marker, testutil.FileAbsent)
}

func (s *sealSuite) TestResealKeyToModeenvWithFdeHookCalled(c *C) {
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)
	defer dirs.SetRootDir("")

	resealKeyToModeenvUsingFDESetupHookCalled := 0
	restore := boot.MockResealKeyToModeenvUsingFDESetupHook(func(string, *boot.Modeenv, *boot.ResealToModeenvOptions) error {
		resealKeyToModeenvUsingFDESetupHookCalled++
		return nil
	})
	defer restore()

	// TODO: this simulates that the hook is not available yet
	//       because of e.g. seeding. Longer term there will be
	//       more, see TODO in resealKeyToModeenvUsingFDESetupHookImpl
	restore = boot.MockHasFDESetupHook(func(kernel *snap.Info) (bool, error) {
		return false, fmt.Errorf("hook not available yet because e.g. seeding")
	})
	defer restore()

	marker := filepath.Join(dirs.SnapFDEDirUnder(rootdir), "sealed-keys")
	err := os.MkdirAll(filepath.Dir(marker), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(marker, []byte("fde-setup-hook"), 0644)
	c.Assert(err, IsNil)

	defer boot.MockModeenvLocked()()

	model := boottest.MakeMockUC20Model()
	modeenv := &boot.Modeenv{
		RecoverySystem: "20200825",
		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	options := &boot.ResealToModeenvOptions{
		ExpectReseal: false,
		Force:        false,
	}
	err = boot.ResealKeyToModeenv(rootdir, modeenv, options, nil)
	c.Assert(err, IsNil)
	c.Check(resealKeyToModeenvUsingFDESetupHookCalled, Equals, 1)
}

func (s *sealSuite) TestResealKeyToModeenvWithFdeHookVerySad(c *C) {
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)
	defer dirs.SetRootDir("")

	resealKeyToModeenvUsingFDESetupHookCalled := 0
	restore := boot.MockResealKeyToModeenvUsingFDESetupHook(func(string, *boot.Modeenv, *boot.ResealToModeenvOptions) error {
		resealKeyToModeenvUsingFDESetupHookCalled++
		return fmt.Errorf("fde setup hook failed")
	})
	defer restore()

	marker := filepath.Join(dirs.SnapFDEDirUnder(rootdir), "sealed-keys")
	err := os.MkdirAll(filepath.Dir(marker), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(marker, []byte("fde-setup-hook"), 0644)
	c.Assert(err, IsNil)

	defer boot.MockModeenvLocked()()

	model := boottest.MakeMockUC20Model()
	modeenv := &boot.Modeenv{
		RecoverySystem: "20200825",
		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}
	options := &boot.ResealToModeenvOptions{
		ExpectReseal: false,
		Force:        false,
	}
	err = boot.ResealKeyToModeenv(rootdir, modeenv, options, nil)
	c.Assert(err, ErrorMatches, "fde setup hook failed")
	c.Check(resealKeyToModeenvUsingFDESetupHookCalled, Equals, 1)
}

func (s *sealSuite) TestResealKeyToModeenvWithTryModel(c *C) {
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)
	defer dirs.SetRootDir("")

	c.Assert(os.MkdirAll(dirs.SnapFDEDir, 0755), IsNil)
	err := os.WriteFile(filepath.Join(dirs.SnapFDEDir, "sealed-keys"), nil, 0644)
	c.Assert(err, IsNil)

	err = createMockGrubCfg(filepath.Join(rootdir, "run/mnt/ubuntu-seed"))
	c.Assert(err, IsNil)

	err = createMockGrubCfg(filepath.Join(rootdir, "run/mnt/ubuntu-boot"))
	c.Assert(err, IsNil)

	model := boottest.MakeMockUC20Model()
	// a try model which would normally only appear during remodel
	tryModel := boottest.MakeMockUC20Model(map[string]interface{}{
		"model": "try-my-model-uc20",
		"grade": "secured",
	})

	modeenv := &boot.Modeenv{
		// recovery system set up like during a remodel, right before a
		// set-device is called, the recovery system of the new model
		// has been tested
		CurrentRecoverySystems: []string{"20200825", "1234", "off-model"},
		GoodRecoverySystems:    []string{"20200825", "1234"},

		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"grubx64.efi": []string{"grub-hash"},
			"bootx64.efi": []string{"shim-hash"},
		},

		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"grubx64.efi": []string{"run-grub-hash"},
		},

		CurrentKernels: []string{"pc-kernel_500.snap"},

		CurrentKernelCommandLines: boot.BootCommandLines{
			"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		},
		// the current model
		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
		// the try model
		TryModel:          tryModel.Model(),
		TryBrandID:        tryModel.BrandID(),
		TryGrade:          string(tryModel.Grade()),
		TryModelSignKeyID: tryModel.SignKeyID(),
	}

	// mock asset cache
	mockAssetsCache(c, rootdir, "grub", []string{
		"bootx64.efi-shim-hash",
		"grubx64.efi-grub-hash",
		"grubx64.efi-run-grub-hash",
	})

	// set a mock recovery kernel
	readSystemEssentialCalls := 0
	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		readSystemEssentialCalls++
		kernelRev := 1
		systemModel := model
		if label == "1234" {
			// recovery system for new model
			kernelRev = 999
			systemModel = tryModel
		}
		if label == "off-model" {
			// a model that matches neither current not try models
			systemModel = boottest.MakeMockUC20Model(map[string]interface{}{
				"model": "different-model-uc20",
				"grade": "secured",
			})
		}
		return systemModel, []*seed.Snap{mockKernelSeedSnap(snap.R(kernelRev)), mockGadgetSeedSnap(c, nil)}, nil
	})
	defer restore()

	defer boot.MockModeenvLocked()()

	// set mock key resealing
	resealKeysCalls := 0
	restore = boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		c.Check(params.TPMPolicyAuthKeyFile, Equals, filepath.Join(dirs.SnapSaveDir, "device/fde", "tpm-policy-auth-key"))
		c.Logf("got:")
		for _, mp := range params.ModelParams {
			c.Logf("model: %v", mp.Model.Model())
			for _, ch := range mp.EFILoadChains {
				printChain(c, ch, "-")
			}
		}

		resealKeysCalls++

		switch resealKeysCalls {
		case 1: // run key
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
			})
			// 2 models, one current and one try model
			c.Assert(params.ModelParams, HasLen, 2)
			// shared parameters
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")
			c.Assert(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
			})
			// 2 load chains (bootloader + run kernel, bootloader + recovery kernel)
			c.Assert(params.ModelParams[0].EFILoadChains, HasLen, 2)

			c.Assert(params.ModelParams[1].Model.Model(), Equals, "try-my-model-uc20")
			c.Assert(params.ModelParams[1].KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=factory-reset snapd_recovery_system=1234 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=recover snapd_recovery_system=1234 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
			})
			// 2 load chains (bootloader + run kernel, bootloader + recovery kernel)
			c.Assert(params.ModelParams[1].EFILoadChains, HasLen, 2)
		case 2: // recovery keys
			c.Assert(params.KeyFiles, DeepEquals, []string{
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
				filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
			})
			// only the current model
			c.Assert(params.ModelParams, HasLen, 1)
			// shared parameters
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")
			for _, mp := range params.ModelParams {
				c.Assert(mp.KernelCmdlines, DeepEquals, []string{
					"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
					"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				})
				// load chains
				c.Assert(mp.EFILoadChains, HasLen, 1)
			}
		default:
			c.Errorf("unexpected additional call to secboot.ResealKeys (call # %d)", resealKeysCalls)
		}

		// recovery parameters
		shim := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash"), bootloader.RoleRecovery)
		grub := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash"), bootloader.RoleRecovery)
		kernelOldRecovery := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
		// kernel from a tried recovery system
		kernelNewRecovery := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_999.snap", "kernel.efi", bootloader.RoleRecovery)
		// run mode parameters
		runGrub := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash"), bootloader.RoleRunMode)
		runKernel := bootloader.NewBootFile(filepath.Join(rootdir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode)

		// verify the load chains, which  are identical for both models
		switch resealKeysCalls {
		case 1: // run load chain for 2 models, current and a try model
			c.Assert(params.ModelParams, HasLen, 2)
			// each load chain has either the run kernel (shared for
			// both), or the kernel of the respective recovery
			// system
			c.Assert(params.ModelParams[0].EFILoadChains, DeepEquals, []*secboot.LoadChain{
				secboot.NewLoadChain(shim,
					secboot.NewLoadChain(grub,
						secboot.NewLoadChain(kernelOldRecovery),
					)),
				secboot.NewLoadChain(shim,
					secboot.NewLoadChain(grub,
						secboot.NewLoadChain(runGrub,
							secboot.NewLoadChain(runKernel)),
					)),
			})
			c.Assert(params.ModelParams[1].EFILoadChains, DeepEquals, []*secboot.LoadChain{
				secboot.NewLoadChain(shim,
					secboot.NewLoadChain(grub,
						secboot.NewLoadChain(kernelNewRecovery),
					)),
				secboot.NewLoadChain(shim,
					secboot.NewLoadChain(grub,
						secboot.NewLoadChain(runGrub,
							secboot.NewLoadChain(runKernel)),
					)),
			})
		case 2: // recovery load chains, only for the current model
			c.Assert(params.ModelParams, HasLen, 1)
			// load chain with a kernel from a recovery system that
			// matches the current model only
			c.Assert(params.ModelParams[0].EFILoadChains, DeepEquals, []*secboot.LoadChain{
				secboot.NewLoadChain(shim,
					secboot.NewLoadChain(grub,
						secboot.NewLoadChain(kernelOldRecovery),
					)),
			})
		}

		return nil
	})
	defer restore()

	// here we don't have unasserted kernels so just set
	// expectReseal to false as it doesn't matter;
	// the behavior with unasserted kernel is tested in
	// boot_test.go specific tests
	options := &boot.ResealToModeenvOptions{
		ExpectReseal: false,
		Force:        false,
	}
	err = boot.ResealKeyToModeenv(rootdir, modeenv, options, nil)
	c.Assert(err, IsNil)
	c.Assert(resealKeysCalls, Equals, 2)

	// verify the boot chains data file for run key

	recoveryAssetChain := []boot.BootAsset{{
		Role:   "recovery",
		Name:   "bootx64.efi",
		Hashes: []string{"shim-hash"},
	}, {
		Role:   "recovery",
		Name:   "grubx64.efi",
		Hashes: []string{"grub-hash"},
	}}
	runAssetChain := []boot.BootAsset{{
		Role:   "recovery",
		Name:   "bootx64.efi",
		Hashes: []string{"shim-hash"},
	}, {
		Role:   "recovery",
		Name:   "grubx64.efi",
		Hashes: []string{"grub-hash"},
	}, {
		Role:   "run-mode",
		Name:   "grubx64.efi",
		Hashes: []string{"run-grub-hash"},
	}}
	runPbc, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDir, "boot-chains"))
	c.Assert(err, IsNil)
	c.Assert(cnt, Equals, 1)
	c.Check(runPbc, DeepEquals, boot.PredictableBootChains{
		// the current model
		boot.BootChain{
			BrandID:        "my-brand",
			Model:          "my-model-uc20",
			Grade:          "dangerous",
			ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
			AssetChain:     recoveryAssetChain,
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
			AssetChain:     runAssetChain,
			Kernel:         "pc-kernel",
			KernelRevision: "500",
			KernelCmdlines: []string{
				"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
			},
		},
		// the try model
		boot.BootChain{
			BrandID:        "my-brand",
			Model:          "try-my-model-uc20",
			Grade:          "secured",
			ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
			AssetChain:     recoveryAssetChain,
			Kernel:         "pc-kernel",
			KernelRevision: "999",
			KernelCmdlines: []string{
				"snapd_recovery_mode=factory-reset snapd_recovery_system=1234 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=recover snapd_recovery_system=1234 console=ttyS0 console=tty1 panic=-1",
			},
		},
		boot.BootChain{
			BrandID:        "my-brand",
			Model:          "try-my-model-uc20",
			Grade:          "secured",
			ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
			AssetChain:     runAssetChain,
			Kernel:         "pc-kernel",
			KernelRevision: "500",
			KernelCmdlines: []string{
				"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
			},
		},
	})
	// recovery boot chains
	recoveryPbc, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDir, "recovery-boot-chains"))
	c.Assert(err, IsNil)
	c.Assert(cnt, Equals, 1)
	c.Check(recoveryPbc, DeepEquals, boot.PredictableBootChains{
		// recovery keys are sealed to current model only
		boot.BootChain{
			BrandID:        "my-brand",
			Model:          "my-model-uc20",
			Grade:          "dangerous",
			ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
			AssetChain:     recoveryAssetChain,
			Kernel:         "pc-kernel",
			KernelRevision: "1",
			KernelCmdlines: []string{
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
			},
		},
	})
}

func (s *sealSuite) TestMarkFactoryResetComplete(c *C) {

	for i, tc := range []struct {
		encrypted                 bool
		factoryKeyAlreadyMigrated bool
		pcrHandleOfKey            uint32
		pcrHandleOfKeyErr         error
		pcrHandleOfKeyCalls       int
		releasePCRHandlesErr      error
		releasePCRHandleCalls     int
		hasFDEHook                bool
		err                       string
	}{
		{
			// unencrypted is a nop
			encrypted: false,
		}, {
			// the old fallback key uses the main handle
			encrypted: true, pcrHandleOfKey: secboot.FallbackObjectPCRPolicyCounterHandle,
			factoryKeyAlreadyMigrated: true, pcrHandleOfKeyCalls: 1, releasePCRHandleCalls: 1,
		}, {
			// the old fallback key uses the alt handle
			encrypted: true, pcrHandleOfKey: secboot.AltFallbackObjectPCRPolicyCounterHandle,
			factoryKeyAlreadyMigrated: true, pcrHandleOfKeyCalls: 1, releasePCRHandleCalls: 1,
		}, {
			// unexpected reboot, the key file was already moved
			encrypted: true, pcrHandleOfKey: secboot.AltFallbackObjectPCRPolicyCounterHandle,
			pcrHandleOfKeyCalls: 1, releasePCRHandleCalls: 1,
		}, {
			// do nothing if we have the FDE hook
			encrypted: true, pcrHandleOfKeyErr: errors.New("unexpected call"),
			hasFDEHook: true,
		},
		// error cases
		{
			encrypted: true, pcrHandleOfKey: secboot.FallbackObjectPCRPolicyCounterHandle,
			factoryKeyAlreadyMigrated: true,
			pcrHandleOfKeyCalls:       1,
			pcrHandleOfKeyErr:         errors.New("handle error"),
			err:                       "cannot perform post factory reset boot cleanup: cannot cleanup secboot state: cannot inspect fallback key: handle error",
		}, {
			encrypted: true, pcrHandleOfKey: secboot.FallbackObjectPCRPolicyCounterHandle,
			factoryKeyAlreadyMigrated: true,
			pcrHandleOfKeyCalls:       1, releasePCRHandleCalls: 1,
			releasePCRHandlesErr: errors.New("release error"),
			err:                  "cannot perform post factory reset boot cleanup: cannot cleanup secboot state: release error",
		},
	} {
		c.Logf("tc %v", i)

		saveSealedKey := filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key")
		saveSealedKeyByFactoryReset := filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key.factory-reset")

		if tc.encrypted {
			c.Assert(os.MkdirAll(boot.InitramfsSeedEncryptionKeyDir, 0755), IsNil)
			if tc.factoryKeyAlreadyMigrated {
				c.Assert(os.WriteFile(saveSealedKey, []byte{'o', 'l', 'd'}, 0644), IsNil)
				c.Assert(os.WriteFile(saveSealedKeyByFactoryReset, []byte{'n', 'e', 'w'}, 0644), IsNil)
			} else {
				c.Assert(os.WriteFile(saveSealedKey, []byte{'n', 'e', 'w'}, 0644), IsNil)
			}
		}

		restore := boot.MockHasFDESetupHook(func(kernel *snap.Info) (bool, error) {
			c.Check(kernel, IsNil)
			return tc.hasFDEHook, nil
		})
		defer restore()

		pcrHandleOfKeyCalls := 0
		restore = boot.MockSecbootPCRHandleOfSealedKey(func(p string) (uint32, error) {
			pcrHandleOfKeyCalls++
			// XXX we're inspecting the current key after it got rotated
			c.Check(p, Equals, filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"))
			return tc.pcrHandleOfKey, tc.pcrHandleOfKeyErr
		})
		defer restore()

		releasePCRHandleCalls := 0
		restore = boot.MockSecbootReleasePCRResourceHandles(func(handles ...uint32) error {
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
			return tc.releasePCRHandlesErr
		})
		defer restore()

		err := boot.MarkFactoryResetComplete(tc.encrypted)
		if tc.err != "" {
			c.Assert(err, ErrorMatches, tc.err)
		} else {
			c.Assert(err, IsNil)
		}
		c.Check(pcrHandleOfKeyCalls, Equals, tc.pcrHandleOfKeyCalls)
		c.Check(releasePCRHandleCalls, Equals, tc.releasePCRHandleCalls)
		if tc.encrypted {
			c.Check(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
				testutil.FileEquals, []byte{'n', 'e', 'w'})
			c.Check(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key.factory-reset"),
				testutil.FileAbsent)
		}
	}

}

func (s *sealSuite) TestForceResealKeyToModeenv(c *C) {
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)
	defer dirs.SetRootDir("")

	marker := filepath.Join(dirs.SnapFDEDirUnder(rootdir), "sealed-keys")
	err := os.MkdirAll(filepath.Dir(marker), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(dirs.SnapFDEDir, "sealed-keys"), nil, 0644)
	c.Assert(err, IsNil)

	model := boottest.MakeMockUC20Model()

	defer boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		return model, []*seed.Snap{mockKernelSeedSnap(snap.R(1)), mockGadgetSeedSnap(c, nil)}, nil
	})()

	err = createMockGrubCfg(filepath.Join(rootdir, "run/mnt/ubuntu-seed"))
	c.Assert(err, IsNil)

	err = createMockGrubCfg(filepath.Join(rootdir, "run/mnt/ubuntu-boot"))
	c.Assert(err, IsNil)

	modeenv := &boot.Modeenv{
		RecoverySystem: "20200825",
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"grubx64.efi": []string{"grub-hash-1"},
			"bootx64.efi": []string{"shim-hash-1"},
		},

		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"grubx64.efi": []string{"run-grub-hash-1"},
		},

		CurrentKernels: []string{"pc-kernel_500.snap"},

		CurrentKernelCommandLines: boot.BootCommandLines{
			"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		},
		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}

	mockAssetsCache(c, rootdir, "grub", []string{
		"bootx64.efi-shim-hash-1",
		"grubx64.efi-grub-hash-1",
		"grubx64.efi-run-grub-hash-1",
	})

	keyForRole := map[string]keys.EncryptionKey{
		gadget.SystemData: {'d', 'a', 't', 'a', 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		gadget.SystemSave: {'s', 'a', 'v', 'e', 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
	}

	options := &boot.ResealToModeenvOptions{
		ExpectReseal: false,
		Force:        true,
		KeyForRole:   keyForRole,
	}

	secbootSealKeysCalls := 0
	defer boot.MockSecbootSealKeys(func(keys []secboot.SealKeyRequest, params *secboot.SealKeysParams) error {
		secbootSealKeysCalls++
		return nil
	})()

	defer boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		c.Errorf("Unexpected called to secboot.ResealKeys")
		return fmt.Errorf("Unexpected")
	})()

	defer boot.MockSecbootPCRHandleOfSealedKey(func(p string) (uint32, error) {
		c.Errorf("Unexpected called to secboot.ResealKeys")
		return 0, fmt.Errorf("Unexpected")
	})()

	defer boot.MockSecbootProvisionTPM(func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error {
		return nil
	})()

	secbootReleasePCRResourceHandlesCalls := 0
	defer boot.MockSecbootReleasePCRResourceHandles(func(handles ...uint32) error {
		secbootReleasePCRResourceHandlesCalls++
		return nil
	})()

	u := mockUnlocker{}
	defer boot.MockModeenvLocked()()
	err = boot.ResealKeyToModeenv(rootdir, modeenv, options, u.unlocker)
	c.Assert(err, IsNil)

	c.Assert(secbootSealKeysCalls, Equals, 2)
	c.Assert(secbootReleasePCRResourceHandlesCalls, Equals, 1)
}
