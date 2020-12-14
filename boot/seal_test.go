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
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
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
}

func (s *sealSuite) TestSealKeyToModeenv(c *C) {
	for _, tc := range []struct {
		sealErr error
		err     string
	}{
		{sealErr: nil, err: ""},
		{sealErr: errors.New("seal error"), err: "cannot seal the encryption keys: seal error"},
	} {
		rootdir := c.MkDir()
		dirs.SetRootDir(rootdir)
		defer dirs.SetRootDir("")

		err := createMockGrubCfg(filepath.Join(rootdir, "run/mnt/ubuntu-seed"))
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
		}

		// mock asset cache
		p := filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-1")
		err = os.MkdirAll(filepath.Dir(p), 0755)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(p, nil, 0644)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash-1"), nil, 0644)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash-1"), nil, 0644)
		c.Assert(err, IsNil)

		// set encryption key
		myKey := secboot.EncryptionKey{}
		myKey2 := secboot.EncryptionKey{}
		for i := range myKey {
			myKey[i] = byte(i)
			myKey2[i] = byte(128 + i)
		}

		model := boottest.MakeMockUC20Model()

		// set a mock recovery kernel
		readSystemEssentialCalls := 0
		restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
			readSystemEssentialCalls++
			kernelSnap := &seed.Snap{
				Path: "/var/lib/snapd/seed/snaps/pc-kernel_1.snap",
				SideInfo: &snap.SideInfo{
					RealName: "pc-kernel",
					Revision: snap.Revision{N: 1},
				},
			}
			return model, []*seed.Snap{kernelSnap}, nil
		})
		defer restore()

		// set mock key sealing
		sealKeysCalls := 0
		restore = boot.MockSecbootSealKeys(func(keys []secboot.SealKeyRequest, params *secboot.SealKeysParams) error {
			sealKeysCalls++
			switch sealKeysCalls {
			case 1:
				// the run object seals only the ubuntu-data key
				c.Check(params.TPMPolicyAuthKeyFile, Equals, filepath.Join(boot.InstallHostFDESaveDir, "tpm-policy-auth-key"))
				c.Check(params.TPMLockoutAuthFile, Equals, filepath.Join(boot.InstallHostFDESaveDir, "tpm-lockout-auth"))

				dataKeyFile := filepath.Join(rootdir, "/run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key")
				c.Check(keys, DeepEquals, []secboot.SealKeyRequest{{Key: myKey, KeyFile: dataKeyFile}})
			case 2:
				// the fallback object seals the ubuntu-data and the ubuntu-save keys
				c.Check(params.TPMPolicyAuthKeyFile, Equals, "")
				c.Check(params.TPMLockoutAuthFile, Equals, "")

				dataKeyFile := filepath.Join(rootdir, "/run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key")
				saveKeyFile := filepath.Join(rootdir, "/run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key")
				c.Check(keys, DeepEquals, []secboot.SealKeyRequest{{Key: myKey, KeyFile: dataKeyFile}, {Key: myKey2, KeyFile: saveKeyFile}})
			default:
				c.Errorf("unexpected additional call to secboot.SealKeys (call # %d)", sealKeysCalls)
			}
			c.Assert(params.ModelParams, HasLen, 1)
			for _, d := range []string{boot.InitramfsSeedEncryptionKeyDir, boot.InstallHostFDEDataDir} {
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
					"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				})
			default:
				c.Errorf("unexpected additional call to secboot.SealKeys (call # %d)", sealKeysCalls)
			}
			c.Assert(params.ModelParams[0].Model.DisplayName(), Equals, "My Model")

			return tc.sealErr
		})
		defer restore()

		err = boot.SealKeyToModeenv(myKey, myKey2, model, modeenv)
		if tc.sealErr != nil {
			c.Assert(sealKeysCalls, Equals, 1)
		} else {
			c.Assert(sealKeysCalls, Equals, 2)
		}
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
			continue
		}

		// verify the boot chains data file
		pbc, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDirUnder(boot.InstallHostWritableDir), "boot-chains"))
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
		pbc, cnt, err = boot.ReadBootChains(filepath.Join(dirs.SnapFDEDirUnder(boot.InstallHostWritableDir), "recovery-boot-chains"))
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
					"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				},
			},
		})

		// marker
		marker := filepath.Join(dirs.SnapFDEDirUnder(boot.InstallHostWritableDir), "sealed-keys")
		c.Check(marker, testutil.FileEquals, "tpm")
	}
}

// TODO:UC20: also test fallback reseal
func (s *sealSuite) TestResealKeyToModeenv(c *C) {
	var prevPbc boot.PredictableBootChains

	for _, tc := range []struct {
		sealedKeys bool
		prevPbc    bool
		resealErr  error
		err        string
	}{
		{sealedKeys: false, resealErr: nil, err: ""},
		{sealedKeys: true, resealErr: nil, err: ""},
		{sealedKeys: true, resealErr: errors.New("reseal error"), err: "cannot reseal the encryption key: reseal error"},
		{prevPbc: true, sealedKeys: true, resealErr: nil, err: ""},
	} {
		rootdir := c.MkDir()
		dirs.SetRootDir(rootdir)
		defer dirs.SetRootDir("")

		if tc.sealedKeys {
			c.Assert(os.MkdirAll(dirs.SnapFDEDir, 0755), IsNil)
			err := ioutil.WriteFile(filepath.Join(dirs.SnapFDEDir, "sealed-keys"), nil, 0644)
			c.Assert(err, IsNil)

		}

		err := createMockGrubCfg(filepath.Join(rootdir, "run/mnt/ubuntu-seed"))
		c.Assert(err, IsNil)

		err = createMockGrubCfg(filepath.Join(rootdir, "run/mnt/ubuntu-boot"))
		c.Assert(err, IsNil)

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
		}

		if tc.prevPbc {
			err := boot.WriteBootChains(prevPbc, filepath.Join(dirs.SnapFDEDir, "boot-chains"), 9)
			c.Assert(err, IsNil)
		}

		// mock asset cache
		p := filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-1")
		err = os.MkdirAll(filepath.Dir(p), 0755)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(p, nil, 0644)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-2"), nil, 0644)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash-1"), nil, 0644)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash-1"), nil, 0644)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash-2"), nil, 0644)
		c.Assert(err, IsNil)

		model := boottest.MakeMockUC20Model()

		// set a mock recovery kernel
		readSystemEssentialCalls := 0
		restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
			readSystemEssentialCalls++
			kernelSnap := &seed.Snap{
				Path: "/var/lib/snapd/seed/snaps/pc-kernel_1.snap",
				SideInfo: &snap.SideInfo{
					RealName: "pc-kernel",
					Revision: snap.Revision{N: 1},
				},
			}
			return model, []*seed.Snap{kernelSnap}, nil
		})
		defer restore()

		// set mock key resealing
		resealKeysCalls := 0
		restore = boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
			c.Check(params.TPMPolicyAuthKeyFile, Equals, filepath.Join(dirs.SnapSaveDir, "device/fde", "tpm-policy-auth-key"))

			resealKeysCalls++
			c.Assert(params.ModelParams, HasLen, 1)

			// shared parameters
			c.Assert(params.ModelParams[0].Model.DisplayName(), Equals, "My Model")
			switch resealKeysCalls {
			case 1:
				c.Assert(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
					"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
					"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
				})
				// load chains
				c.Assert(params.ModelParams[0].EFILoadChains, HasLen, 6)
			case 2:
				c.Assert(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
					"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				})
				// load chains
				c.Assert(params.ModelParams[0].EFILoadChains, HasLen, 2)
			default:
				c.Errorf("unexpected additional call to secboot.ResealKeys (call # %d)", resealKeysCalls)
			}

			// recovery parameters
			shim := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-1"), bootloader.RoleRecovery)
			shim2 := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-2"), bootloader.RoleRecovery)
			grub := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash-1"), bootloader.RoleRecovery)
			kernel := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)

			c.Assert(params.ModelParams[0].EFILoadChains[:2], DeepEquals, []*secboot.LoadChain{
				secboot.NewLoadChain(shim,
					secboot.NewLoadChain(grub,
						secboot.NewLoadChain(kernel))),
				secboot.NewLoadChain(shim2,
					secboot.NewLoadChain(grub,
						secboot.NewLoadChain(kernel))),
			})

			// run mode parameters
			runGrub := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash-1"), bootloader.RoleRunMode)
			runGrub2 := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash-2"), bootloader.RoleRunMode)
			runKernel := bootloader.NewBootFile(filepath.Join(rootdir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode)
			runKernel2 := bootloader.NewBootFile(filepath.Join(rootdir, "var/lib/snapd/snaps/pc-kernel_600.snap"), "kernel.efi", bootloader.RoleRunMode)

			switch resealKeysCalls {
			case 1:
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

			return tc.resealErr
		})
		defer restore()

		// here we don't have unasserted kernels so just set
		// expectReseal to false as it doesn't matter;
		// the behavior with unasserted kernel is tested in
		// boot_test.go specific tests
		const expectReseal = false
		err = boot.ResealKeyToModeenv(rootdir, model, modeenv, expectReseal)
		if !tc.sealedKeys || tc.prevPbc {
			// did nothing
			c.Assert(err, IsNil)
			c.Assert(resealKeysCalls, Equals, 0)
			continue
		}
		if tc.resealErr != nil {
			c.Assert(resealKeysCalls, Equals, 1)
		} else {
			c.Assert(resealKeysCalls, Equals, 2)
		}
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
			continue
		}

		// verify the boot chains data file
		pbc, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDir, "boot-chains"))
		c.Assert(err, IsNil)
		if tc.prevPbc {
			c.Assert(cnt, Equals, 10)
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
	}
}

func (s *sealSuite) TestRecoveryBootChainsForSystems(c *C) {
	for _, tc := range []struct {
		assetsMap          boot.BootAssetsMap
		recoverySystems    []string
		undefinedKernel    bool
		expectedAssets     []boot.BootAsset
		expectedKernelRevs []int
		err                string
	}{
		{
			// transition sequences
			recoverySystems: []string{"20200825"},
			assetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1", "grub-hash-2"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			expectedAssets: []boot.BootAsset{
				{Role: bootloader.RoleRecovery, Name: "bootx64.efi", Hashes: []string{"shim-hash-1"}},
				{Role: bootloader.RoleRecovery, Name: "grubx64.efi", Hashes: []string{"grub-hash-1", "grub-hash-2"}},
			},
			expectedKernelRevs: []int{1},
		},
		{
			// two systems
			recoverySystems: []string{"20200825", "20200831"},
			assetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1", "grub-hash-2"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			expectedAssets: []boot.BootAsset{
				{Role: bootloader.RoleRecovery, Name: "bootx64.efi", Hashes: []string{"shim-hash-1"}},
				{Role: bootloader.RoleRecovery, Name: "grubx64.efi", Hashes: []string{"grub-hash-1", "grub-hash-2"}},
			},
			expectedKernelRevs: []int{1, 3},
		},
		{
			// non-transition sequence
			recoverySystems: []string{"20200825"},
			assetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			expectedAssets: []boot.BootAsset{
				{Role: bootloader.RoleRecovery, Name: "bootx64.efi", Hashes: []string{"shim-hash-1"}},
				{Role: bootloader.RoleRecovery, Name: "grubx64.efi", Hashes: []string{"grub-hash-1"}},
			},
			expectedKernelRevs: []int{1},
		},
		{
			// invalid recovery system label
			recoverySystems: []string{"0"},
			err:             `invalid system seed label: "0"`,
		},
	} {
		rootdir := c.MkDir()
		dirs.SetRootDir(rootdir)
		defer dirs.SetRootDir("")

		// set recovery kernel
		restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
			if label != "20200825" && label != "20200831" {
				return nil, nil, fmt.Errorf("invalid system seed label: %q", label)
			}
			kernelRev := 1
			if label == "20200831" {
				kernelRev = 3
			}
			kernelSnap := &seed.Snap{
				Path: fmt.Sprintf("/var/lib/snapd/seed/snaps/pc-kernel_%d.snap", kernelRev),
				SideInfo: &snap.SideInfo{
					RealName: "pc-kernel",
					Revision: snap.R(kernelRev),
				},
			}
			return nil, []*seed.Snap{kernelSnap}, nil
		})
		defer restore()

		grubDir := filepath.Join(rootdir, "run/mnt/ubuntu-seed")
		err := createMockGrubCfg(grubDir)
		c.Assert(err, IsNil)

		bl, err := bootloader.Find(grubDir, &bootloader.Options{Role: bootloader.RoleRecovery})
		c.Assert(err, IsNil)
		tbl, ok := bl.(bootloader.TrustedAssetsBootloader)
		c.Assert(ok, Equals, true)

		model := boottest.MakeMockUC20Model()

		modeenv := &boot.Modeenv{
			CurrentTrustedRecoveryBootAssets: tc.assetsMap,
		}

		bc, err := boot.RecoveryBootChainsForSystems(tc.recoverySystems, tbl, model, modeenv)
		if tc.err == "" {
			c.Assert(err, IsNil)
			c.Assert(bc, HasLen, len(tc.recoverySystems))
			for i, chain := range bc {
				c.Assert(chain.AssetChain, DeepEquals, tc.expectedAssets)
				c.Check(chain.Kernel, Equals, "pc-kernel")
				expectedKernelRev := tc.expectedKernelRevs[i]
				c.Check(chain.KernelRevision, Equals, fmt.Sprintf("%d", expectedKernelRev))
				c.Check(chain.KernelBootFile(), DeepEquals, bootloader.BootFile{Snap: fmt.Sprintf("/var/lib/snapd/seed/snaps/pc-kernel_%d.snap", expectedKernelRev), Path: "kernel.efi", Role: bootloader.RoleRecovery})
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
	return ioutil.WriteFile(cfg, []byte("# Snapd-Boot-Config-Edition: 1\n"), 0644)
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
	p := filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/shim-shim-hash")
	err := os.MkdirAll(filepath.Dir(p), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(p, nil, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/loader-loader-hash1"), nil, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/loader-loader-hash2"), nil, 0644)
	c.Assert(err, IsNil)

	oldmodel := boottest.MakeMockUC20Model(map[string]interface{}{
		"model":     "old-model-uc20",
		"timestamp": "2019-10-01T08:00:00+00:00",
	})

	// old recovery
	oldrc := boot.BootChain{
		BrandID: oldmodel.BrandID(),
		Model:   oldmodel.Model(),
		AssetChain: []boot.BootAsset{
			{Name: "shim", Role: bootloader.RoleRecovery, Hashes: []string{"shim-hash"}},
			{Name: "loader", Role: bootloader.RoleRecovery, Hashes: []string{"loader-hash1"}},
		},
		KernelCmdlines: []string{"panic=1", "oldrc"},
	}
	oldrc.SetModelAssertion(oldmodel)
	oldkbf := bootloader.BootFile{Snap: "pc-kernel_1.snap"}
	oldrc.SetKernelBootFile(oldkbf)

	// recovery
	rc1 := boot.BootChain{
		BrandID: model.BrandID(),
		Model:   model.Model(),
		AssetChain: []boot.BootAsset{
			{Name: "shim", Role: bootloader.RoleRecovery, Hashes: []string{"shim-hash"}},
			{Name: "loader", Role: bootloader.RoleRecovery, Hashes: []string{"loader-hash1"}},
		},
		KernelCmdlines: []string{"panic=1", "rc1"},
	}
	rc1.SetModelAssertion(model)
	rc1kbf := bootloader.BootFile{Snap: "pc-kernel_10.snap"}
	rc1.SetKernelBootFile(rc1kbf)

	// run system
	runc1 := boot.BootChain{
		BrandID: model.BrandID(),
		Model:   model.Model(),
		AssetChain: []boot.BootAsset{
			{Name: "shim", Role: bootloader.RoleRecovery, Hashes: []string{"shim-hash"}},
			{Name: "loader", Role: bootloader.RoleRecovery, Hashes: []string{"loader-hash1"}},
			{Name: "loader", Role: bootloader.RoleRunMode, Hashes: []string{"loader-hash2"}},
		},
		KernelCmdlines: []string{"panic=1", "runc1"},
	}
	runc1.SetModelAssertion(model)
	runc1kbf := bootloader.BootFile{Snap: "pc-kernel_50.snap"}
	runc1.SetKernelBootFile(runc1kbf)

	pbc := boot.ToPredictableBootChains([]boot.BootChain{rc1, runc1, oldrc})

	shim := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/shim-shim-hash"), bootloader.RoleRecovery)
	loader1 := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/loader-loader-hash1"), bootloader.RoleRecovery)
	loader2 := bootloader.NewBootFile("", filepath.Join(rootdir, "var/lib/snapd/boot-assets/grub/loader-loader-hash2"), bootloader.RoleRunMode)

	params, err := boot.SealKeyModelParams(pbc, roleToBlName)
	c.Assert(err, IsNil)
	c.Check(params, HasLen, 2)
	c.Check(params[0].Model, Equals, model)
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

	c.Check(params[1].Model, Equals, oldmodel)
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

	needed, _, err := boot.IsResealNeeded(pbc, filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"), false)
	c.Assert(err, IsNil)
	c.Check(needed, Equals, false)

	otherchain := []boot.BootChain{pbc[0]}
	needed, cnt, err := boot.IsResealNeeded(otherchain, filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"), false)
	c.Assert(err, IsNil)
	// chains are different
	c.Check(needed, Equals, true)
	c.Check(cnt, Equals, 3)

	// boot-chains does not exist, we cannot compare so advise to reseal
	otherRootdir := c.MkDir()
	needed, cnt, err = boot.IsResealNeeded(otherchain, filepath.Join(dirs.SnapFDEDirUnder(otherRootdir), "boot-chains"), false)
	c.Assert(err, IsNil)
	c.Check(needed, Equals, true)
	c.Check(cnt, Equals, 1)

	// exists but cannot be read
	c.Assert(os.Chmod(filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"), 0000), IsNil)
	defer os.Chmod(filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"), 0755)
	needed, _, err = boot.IsResealNeeded(otherchain, filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains"), false)
	c.Assert(err, ErrorMatches, "cannot open existing boot chains data file: open .*/boot-chains: permission denied")
	c.Check(needed, Equals, false)

	// unrevisioned kernel chain
	unrevchain := []boot.BootChain{pbc[0], pbc[1]}
	unrevchain[1].KernelRevision = ""
	// write on disk
	bootChainsFile := filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains")
	err = boot.WriteBootChains(unrevchain, bootChainsFile, 2)
	c.Assert(err, IsNil)

	needed, cnt, err = boot.IsResealNeeded(pbc, bootChainsFile, false)
	c.Assert(err, IsNil)
	c.Check(needed, Equals, true)
	c.Check(cnt, Equals, 3)

	// cases falling back to expectReseal
	needed, _, err = boot.IsResealNeeded(unrevchain, bootChainsFile, false)
	c.Assert(err, IsNil)
	c.Check(needed, Equals, false)

	needed, cnt, err = boot.IsResealNeeded(unrevchain, bootChainsFile, true)
	c.Assert(err, IsNil)
	c.Check(needed, Equals, true)
	c.Check(cnt, Equals, 3)
}

func (s *sealSuite) TestSealToModeenvWithFdeHookHappy(c *C) {
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)
	defer dirs.SetRootDir("")

	restore := boot.MockHasFDESetupHook(func() (bool, error) {
		return true, nil
	})
	defer restore()

	n := 0
	var runFDESetupHookParams []*boot.FDESetupHookParams
	restore = boot.MockRunFDESetupHook(func(op string, params *boot.FDESetupHookParams) ([]byte, error) {
		n++
		c.Assert(op, Equals, "initial-setup")
		runFDESetupHookParams = append(runFDESetupHookParams, params)
		return []byte("sealed-key: " + strconv.Itoa(n)), nil
	})
	defer restore()

	modeenv := &boot.Modeenv{
		RecoverySystem: "20200825",
	}
	key := secboot.EncryptionKey{1, 2, 3, 4}
	saveKey := secboot.EncryptionKey{5, 6, 7, 8}

	model := boottest.MakeMockUC20Model()
	err := boot.SealKeyToModeenv(key, saveKey, model, modeenv)
	c.Assert(err, IsNil)
	// check that runFDESetupHook was called the expected way
	c.Check(runFDESetupHookParams, DeepEquals, []*boot.FDESetupHookParams{
		{Key: secboot.EncryptionKey{1, 2, 3, 4}, Models: []*asserts.Model{model}},
		{Key: secboot.EncryptionKey{1, 2, 3, 4}, Models: []*asserts.Model{model}},
		{Key: secboot.EncryptionKey{5, 6, 7, 8}, Models: []*asserts.Model{model}},
	})
	// check that the sealed keys got written to the expected places
	for i, p := range []string{
		filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
		filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
		filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
	} {
		c.Check(p, testutil.FileEquals, "sealed-key: "+strconv.Itoa(i+1))
	}
	marker := filepath.Join(dirs.SnapFDEDirUnder(boot.InstallHostWritableDir), "sealed-keys")
	c.Check(marker, testutil.FileEquals, "fde-setup-hook")
}

func (s *sealSuite) TestSealToModeenvWithFdeHookSad(c *C) {
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)
	defer dirs.SetRootDir("")

	restore := boot.MockHasFDESetupHook(func() (bool, error) {
		return true, nil
	})
	defer restore()

	restore = boot.MockRunFDESetupHook(func(op string, params *boot.FDESetupHookParams) ([]byte, error) {
		return nil, fmt.Errorf("hook failed")
	})
	defer restore()

	modeenv := &boot.Modeenv{
		RecoverySystem: "20200825",
	}
	key := secboot.EncryptionKey{1, 2, 3, 4}
	saveKey := secboot.EncryptionKey{5, 6, 7, 8}

	model := boottest.MakeMockUC20Model()
	err := boot.SealKeyToModeenv(key, saveKey, model, modeenv)
	c.Assert(err, ErrorMatches, "hook failed")
	marker := filepath.Join(dirs.SnapFDEDirUnder(boot.InstallHostWritableDir), "sealed-keys")
	c.Check(marker, testutil.FileAbsent)
}

func (s *sealSuite) TestResealKeyToModeenvWithFdeHookSad(c *C) {
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)
	defer dirs.SetRootDir("")

	resealKeyToModeenvUsingFDESetupHookCalled := 0
	restore := boot.MockResealKeyToModeenvUsingFDESetupHook(func(string, *asserts.Model, *boot.Modeenv, bool) error {
		resealKeyToModeenvUsingFDESetupHookCalled++
		return nil
	})
	defer restore()

	marker := filepath.Join(dirs.SnapFDEDirUnder(rootdir), "sealed-keys")
	err := os.MkdirAll(filepath.Dir(marker), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(marker, []byte("fde-setup-hook"), 0644)
	c.Assert(err, IsNil)

	modeenv := &boot.Modeenv{
		RecoverySystem: "20200825",
	}

	model := boottest.MakeMockUC20Model()
	expectReseal := false
	err = boot.ResealKeyToModeenv(rootdir, model, modeenv, expectReseal)
	c.Assert(err, IsNil)
	c.Check(resealKeyToModeenvUsingFDESetupHookCalled, Equals, 1)
}
