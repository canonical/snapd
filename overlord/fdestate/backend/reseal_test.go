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
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	fdeBackend "github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/testutil"
)

func TestFDEBackend(t *testing.T) { TestingT(t) }

func isChainPresent(allowed []*secboot.LoadChain, files []bootloader.BootFile) bool {
	if len(files) == 0 {
		return len(allowed) == 0
	}

	current := files[0]
	for _, c := range allowed {
		if current.Path == c.Path && current.Snap == c.Snap && current.Role == c.Role {
			if isChainPresent(c.Next, files[1:]) {
				return true
			}
		}
	}

	return false
}

type containsChainChecker struct {
	*CheckerInfo
}

var ContainsChain Checker = &containsChainChecker{
	&CheckerInfo{Name: "ContainsChain", Params: []string{"chainscontainer", "chain"}},
}

func (c *containsChainChecker) Check(params []interface{}, names []string) (result bool, error string) {
	allowed, ok := params[0].([]*secboot.LoadChain)
	if !ok {
		return false, "Wrong type for chain container"
	}
	bootFiles, ok := params[1].([]bootloader.BootFile)
	if !ok {
		return false, "Wrong type for boot file chain"
	}
	result = isChainPresent(allowed, bootFiles)
	if !result {
		error = fmt.Sprintf("Chain %v is not present in allowed boot chains", bootFiles)
	}
	return result, error
}

func removeKernelBootFiles(bootChains []boot.BootChain) []boot.BootChain {
	var ret []boot.BootChain
	for _, v := range bootChains {
		v.KernelBootFile = bootloader.BootFile{}
		ret = append(ret, v)
	}
	return ret
}

func mockAssetsCache(c *C, rootdir, bootloaderName string, cachedAssets []string) {
	p := filepath.Join(dirs.SnapBootAssetsDirUnder(rootdir), bootloaderName)
	err := os.MkdirAll(p, 0755)
	c.Assert(err, IsNil)
	for _, cachedAsset := range cachedAssets {
		err = os.WriteFile(filepath.Join(p, cachedAsset), nil, 0644)
		c.Assert(err, IsNil)
	}
}

type resealTestSuite struct {
	testutil.BaseTest

	rootdir string
}

var _ = Suite(&resealTestSuite{})

func (s *resealTestSuite) SetUpTest(c *C) {
	s.rootdir = c.MkDir()
	dirs.SetRootDir(s.rootdir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })
}

func (s *resealTestSuite) TestTPMResealHappy(c *C) {
	bl := bootloadertest.Mock("trusted", "").WithTrustedAssets()
	bootloader.Force(bl)
	defer bootloader.Force(nil)

	bl.TrustedAssetsMap = map[string]string{
		"asset": "asset",
		"shim":  "shim",
	}
	recoveryKernel := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
	runKernel := bootloader.NewBootFile(filepath.Join(s.rootdir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode)
	shimBf := bootloader.NewBootFile("", filepath.Join(dirs.SnapBootAssetsDir, "trusted", "shim-shimhash"), bootloader.RoleRecovery)
	assetBf := bootloader.NewBootFile("", filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-assethash"), bootloader.RoleRecovery)
	runAssetBf := bootloader.NewBootFile("", filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-runassethash"), bootloader.RoleRunMode)

	bl.RecoveryBootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "shim", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "asset", bootloader.RoleRecovery),
		recoveryKernel,
	}
	bl.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "shim", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "asset", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "asset", bootloader.RoleRunMode),
		runKernel,
	}

	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapBootAssetsDir, "trusted"), 0755), IsNil)
	for _, name := range []string{
		"shim-shimhash",
		"asset-runassethash",
		"asset-assethash",
	} {
		err := os.WriteFile(filepath.Join(dirs.SnapBootAssetsDir, "trusted", name), nil, 0644)
		c.Assert(err, IsNil)
	}

	model := boottest.MakeMockUC20Model()
	params := &boot.ResealKeyForBootChainsParams{
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
						Name: "shim",
						Hashes: []string{
							"shimhash",
						},
					},
					{
						Role: bootloader.RoleRecovery,
						Name: "asset",
						Hashes: []string{
							"assethash",
						},
					},
					{
						Role: bootloader.RoleRunMode,
						Name: "asset",
						Hashes: []string{
							"runassethash",
						},
					},
				},

				Kernel:         "kernel.efi",
				KernelRevision: "500",
				KernelCmdlines: []string{
					"mode=run",
				},
				KernelBootFile: runKernel,
			},
		},

		RecoveryBootChainsForRunKey: []boot.BootChain{
			{
				BrandID:        model.BrandID(),
				Model:          model.Model(),
				Classic:        model.Classic(),
				Grade:          model.Grade(),
				ModelSignKeyID: model.SignKeyID(),

				AssetChain: []boot.BootAsset{
					{
						Role: bootloader.RoleRecovery,
						Name: "shim",
						Hashes: []string{
							"shimhash",
						},
					},
					{
						Role: bootloader.RoleRecovery,
						Name: "asset",
						Hashes: []string{
							"assethash",
						},
					},
				},

				Kernel:         "kernel.efi",
				KernelRevision: "1",
				KernelCmdlines: []string{
					"mode=recover",
				},
				KernelBootFile: recoveryKernel,
			},
		},

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
						Name: "shim",
						Hashes: []string{
							"shimhash",
						},
					},
					{
						Role: bootloader.RoleRecovery,
						Name: "asset",
						Hashes: []string{
							"assethash",
						},
					},
				},

				Kernel:         "kernel.efi",
				KernelRevision: "1",
				KernelCmdlines: []string{
					"mode=recover",
				},
				KernelBootFile: recoveryKernel,
			},
		},

		RoleToBlName: map[bootloader.Role]string{
			bootloader.RoleRecovery: "trusted",
			bootloader.RoleRunMode:  "trusted",
		},
	}

	resealCalls := 0
	restore := fdeBackend.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++

		c.Check(params.TPMPolicyAuthKeyFile, Equals, filepath.Join(dirs.SnapSaveDir, "device/fde", "tpm-policy-auth-key"))

		c.Assert(params.ModelParams, HasLen, 1)
		mp := params.ModelParams[0]
		c.Check(mp.Model.Model(), Equals, model.Model())
		switch resealCalls {
		case 1:
			// Resealing the run+recover key for data partition
			c.Check(params.KeyFiles, DeepEquals, []string{
				filepath.Join(s.rootdir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"),
			})
			c.Check(mp.EFILoadChains, DeepEquals, []*secboot.LoadChain{
				secboot.NewLoadChain(shimBf,
					secboot.NewLoadChain(assetBf,
						secboot.NewLoadChain(recoveryKernel))),
				secboot.NewLoadChain(shimBf,
					secboot.NewLoadChain(assetBf,
						secboot.NewLoadChain(runAssetBf,
							secboot.NewLoadChain(runKernel)))),
			})
		case 2:
			// Resealing the recovery key for both data and save partitions
			c.Check(params.KeyFiles, DeepEquals, []string{
				filepath.Join(s.rootdir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"),
				filepath.Join(s.rootdir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"),
			})
			c.Check(mp.EFILoadChains, DeepEquals, []*secboot.LoadChain{
				secboot.NewLoadChain(shimBf,
					secboot.NewLoadChain(assetBf,
						secboot.NewLoadChain(recoveryKernel))),
			})
		default:
			c.Errorf("unexpected additional call to secboot.ResealKey (call # %d)", resealCalls)
		}
		return nil
	})

	defer restore()

	const expectReseal = true
	err := fdeBackend.ResealKeyForBootChains(device.SealingMethodTPM, s.rootdir, params, expectReseal)
	c.Assert(err, IsNil)

	c.Check(resealCalls, Equals, 2)

	pbc, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDir, "boot-chains"))
	c.Assert(err, IsNil)
	c.Assert(cnt, Equals, 1)
	c.Check(pbc, DeepEquals, boot.ToPredictableBootChains(removeKernelBootFiles(append(params.RunModeBootChains, params.RecoveryBootChainsForRunKey...))))

	recoveryPbc, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDir, "recovery-boot-chains"))
	c.Assert(err, IsNil)
	c.Assert(cnt, Equals, 1)
	c.Check(recoveryPbc, DeepEquals, boot.ToPredictableBootChains(removeKernelBootFiles(params.RecoveryBootChains)))
}

func (s *resealTestSuite) TestResealKeyToModeenvWithSystemFallback(c *C) {
	var prevPbc boot.PredictableBootChains
	var prevRecoveryPbc boot.PredictableBootChains

	for idx, tc := range []struct {
		reuseRunPbc      bool
		reuseRecoveryPbc bool
		resealErr        error
		shimId           string
		shimId2          string
		noShim2          bool
		grubId           string
		grubId2          string
		noGrub2          bool
		runGrubId        string
		err              string
	}{
		{shimId: "bootx64.efi", grubId: "grubx64.efi", resealErr: nil, err: ""},
		{shimId: "bootx64.efi", grubId: "grubx64.efi", resealErr: nil, err: ""},
		{shimId2: "bootx64.efi", grubId2: "grubx64.efi", resealErr: nil, err: ""},
		{shimId: "bootx64.efi", grubId: "grubx64.efi", shimId2: "ubuntu:shimx64.efi", grubId2: "ubuntu:grubx64.efi", resealErr: nil, err: ""},
		{noGrub2: true, resealErr: nil, err: ""},
		{noShim2: true, resealErr: nil, err: ""},
		{noShim2: true, noGrub2: true, resealErr: nil, err: ""},
		{resealErr: nil, err: ""},
		{resealErr: errors.New("reseal error"), err: "cannot reseal the encryption key: reseal error"},
		{reuseRunPbc: true, reuseRecoveryPbc: true, resealErr: nil, err: ""},
		// recovery boot chain is unchanged
		{reuseRunPbc: false, reuseRecoveryPbc: true, resealErr: nil, err: ""},
		// run boot chain is unchanged
		{reuseRunPbc: true, reuseRecoveryPbc: false, resealErr: nil, err: ""},
	} {
		c.Logf("tc: %v", idx)
		rootdir := c.MkDir()
		dirs.SetRootDir(rootdir)
		defer dirs.SetRootDir("/")

		shimId := tc.shimId
		if shimId == "" {
			shimId = "ubuntu:shimx64.efi"
		}
		shimId2 := tc.shimId2
		if shimId2 == "" && !tc.noShim2 {
			shimId2 = shimId
		}
		grubId := tc.grubId
		if grubId == "" {
			grubId = "ubuntu:grubx64.efi"
		}
		grubId2 := tc.grubId2
		if grubId2 == "" && !tc.noGrub2 {
			grubId2 = grubId
		}
		runGrubId := tc.runGrubId
		if runGrubId == "" {
			runGrubId = "grubx64.efi"
		}

		var expectedCache []string
		expectedCache = append(expectedCache, fmt.Sprintf("%s-shim-hash-1", shimId))
		if shimId2 != "" {
			expectedCache = append(expectedCache, fmt.Sprintf("%s-shim-hash-2", shimId2))
		}
		expectedCache = append(expectedCache, fmt.Sprintf("%s-grub-hash-1", grubId))
		if grubId2 != "" {
			expectedCache = append(expectedCache, fmt.Sprintf("%s-grub-hash-2", grubId2))
		}

		expectedCache = append(expectedCache,
			fmt.Sprintf("%s-run-grub-hash-1", runGrubId),
			fmt.Sprintf("%s-run-grub-hash-2", runGrubId),
		)

		if tc.reuseRunPbc {
			err := boot.WriteBootChains(prevPbc, filepath.Join(dirs.SnapFDEDir, "boot-chains"), 9)
			c.Assert(err, IsNil)
		}
		if tc.reuseRecoveryPbc {
			err := boot.WriteBootChains(prevRecoveryPbc, filepath.Join(dirs.SnapFDEDir, "recovery-boot-chains"), 9)
			c.Assert(err, IsNil)
		}

		// mock asset cache
		mockAssetsCache(c, rootdir, "grub", expectedCache)

		// set mock key resealing
		resealKeysCalls := 0
		restore := fdeBackend.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
			c.Check(params.TPMPolicyAuthKeyFile, Equals, filepath.Join(dirs.SnapSaveDir, "device/fde", "tpm-policy-auth-key"))

			resealKeysCalls++
			c.Assert(params.ModelParams, HasLen, 1)

			// shared parameters
			c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")

			// recovery parameters
			shim := bootloader.NewBootFile("", filepath.Join(rootdir, fmt.Sprintf("var/lib/snapd/boot-assets/grub/%s-shim-hash-1", shimId)), bootloader.RoleRecovery)
			shim2 := bootloader.NewBootFile("", filepath.Join(rootdir, fmt.Sprintf("var/lib/snapd/boot-assets/grub/%s-shim-hash-2", shimId2)), bootloader.RoleRecovery)
			grub := bootloader.NewBootFile("", filepath.Join(rootdir, fmt.Sprintf("var/lib/snapd/boot-assets/grub/%s-grub-hash-1", grubId)), bootloader.RoleRecovery)
			grub2 := bootloader.NewBootFile("", filepath.Join(rootdir, fmt.Sprintf("var/lib/snapd/boot-assets/grub/%s-grub-hash-2", grubId2)), bootloader.RoleRecovery)
			kernel := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
			// run mode parameters
			runGrub := bootloader.NewBootFile("", filepath.Join(rootdir, fmt.Sprintf("var/lib/snapd/boot-assets/grub/%s-run-grub-hash-1", runGrubId)), bootloader.RoleRunMode)
			runGrub2 := bootloader.NewBootFile("", filepath.Join(rootdir, fmt.Sprintf("var/lib/snapd/boot-assets/grub/%s-run-grub-hash-2", runGrubId)), bootloader.RoleRunMode)
			runKernel := bootloader.NewBootFile(filepath.Join(rootdir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode)
			runKernel2 := bootloader.NewBootFile(filepath.Join(rootdir, "var/lib/snapd/snaps/pc-kernel_600.snap"), "kernel.efi", bootloader.RoleRunMode)

			var possibleChains [][]bootloader.BootFile
			for _, possibleRunKernel := range []bootloader.BootFile{runKernel, runKernel2} {
				possibleChains = append(possibleChains, []bootloader.BootFile{
					shim,
					grub,
					runGrub,
					possibleRunKernel,
				})
				possibleChains = append(possibleChains, []bootloader.BootFile{
					shim,
					grub,
					runGrub2,
					possibleRunKernel,
				})
				if grubId2 != "" {
					if shimId2 == shimId {
						// We keep the same boot chain so, shim -> grub2 is possible.
						possibleChains = append(possibleChains, []bootloader.BootFile{
							shim,
							grub2,
							runGrub2,
							possibleRunKernel,
						})
					}
					if shimId2 != "" {
						possibleChains = append(possibleChains, []bootloader.BootFile{
							shim2,
							grub2,
							runGrub2,
							possibleRunKernel,
						})
					}
				} else if shimId2 != "" {
					// We should not test the case where we half update, to a completely new bootchain.
					c.Assert(shimId, Equals, shimId2)

					possibleChains = append(possibleChains, []bootloader.BootFile{
						shim2,
						grub,
						runGrub2,
						possibleRunKernel,
					})
				}
			}

			var possibleRecoveryChains [][]bootloader.BootFile
			possibleRecoveryChains = append(possibleRecoveryChains, []bootloader.BootFile{
				shim,
				grub,
				kernel,
			})
			if grubId2 != "" {
				if shimId2 == shimId {
					// We keep the same boot chain so, shim -> grub2 is possible.
					possibleRecoveryChains = append(possibleRecoveryChains, []bootloader.BootFile{
						shim,
						grub2,
						kernel,
					})
				}
				if shimId2 != "" {
					possibleRecoveryChains = append(possibleRecoveryChains, []bootloader.BootFile{
						shim2,
						grub2,
						kernel,
					})
				}
			} else if shimId2 != "" {
				// We should not test the case where we half update, to a completely new bootchain.
				c.Assert(shimId, Equals, shimId2)

				possibleRecoveryChains = append(possibleRecoveryChains, []bootloader.BootFile{
					shim2,
					grub,
					kernel,
				})
			}

			checkRunParams := func() {
				c.Check(params.KeyFiles, DeepEquals, []string{
					filepath.Join(boot.InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
				})
				c.Check(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
					"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
					"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
				})

				for _, chain := range possibleChains {
					c.Check(params.ModelParams[0].EFILoadChains, ContainsChain, chain)
				}
				for _, chain := range possibleRecoveryChains {
					c.Check(params.ModelParams[0].EFILoadChains, ContainsChain, chain)
				}
			}

			checkRecoveryParams := func() {
				c.Check(params.KeyFiles, DeepEquals, []string{
					filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
					filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
				})
				c.Check(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
					"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				})
				for _, chain := range possibleRecoveryChains {
					c.Check(params.ModelParams[0].EFILoadChains, ContainsChain, chain)
				}
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

		kernel := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
		runKernel := bootloader.NewBootFile(filepath.Join(rootdir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode)
		runKernel2 := bootloader.NewBootFile(filepath.Join(rootdir, "var/lib/snapd/snaps/pc-kernel_600.snap"), "kernel.efi", bootloader.RoleRunMode)

		var runBootChains []boot.BootChain
		var recoveryBootChainsForRun []boot.BootChain
		var recoveryBootChains []boot.BootChain
		var shimHashes []string
		shimHashes = append(shimHashes, "shim-hash-1")
		if shimId2 != "" && shimId2 == shimId {
			shimHashes = append(shimHashes, "shim-hash-2")
		}
		var grubHashes []string
		grubHashes = append(grubHashes, "grub-hash-1")
		if grubId2 != "" && grubId2 == grubId {
			grubHashes = append(grubHashes, "grub-hash-2")
		}
		recoveryBootChains = append(recoveryBootChains,
			boot.BootChain{
				BrandID:        "my-brand",
				Model:          "my-model-uc20",
				Grade:          "dangerous",
				ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
				AssetChain: []boot.BootAsset{
					{
						Role:   bootloader.RoleRecovery,
						Name:   shimId,
						Hashes: shimHashes,
					},
					{
						Role:   bootloader.RoleRecovery,
						Name:   grubId,
						Hashes: grubHashes,
					},
				},
				Kernel:         "pc-kernel",
				KernelRevision: "1",
				KernelCmdlines: []string{
					"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				},
				KernelBootFile: kernel,
			},
		)
		recoveryBootChainsForRun = append(recoveryBootChainsForRun,
			boot.BootChain{
				BrandID:        "my-brand",
				Model:          "my-model-uc20",
				Grade:          "dangerous",
				ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
				AssetChain: []boot.BootAsset{
					{
						Role:   bootloader.RoleRecovery,
						Name:   shimId,
						Hashes: shimHashes,
					},
					{
						Role:   bootloader.RoleRecovery,
						Name:   grubId,
						Hashes: grubHashes,
					},
				},
				Kernel:         "pc-kernel",
				KernelRevision: "1",
				KernelCmdlines: []string{
					"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
				},
				KernelBootFile: kernel,
			},
		)
		runBootChains = append(runBootChains,
			boot.BootChain{
				BrandID:        "my-brand",
				Model:          "my-model-uc20",
				Grade:          "dangerous",
				ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
				AssetChain: []boot.BootAsset{
					{
						Role:   bootloader.RoleRecovery,
						Name:   shimId,
						Hashes: shimHashes,
					},
					{
						Role:   bootloader.RoleRecovery,
						Name:   grubId,
						Hashes: grubHashes,
					},
					{
						Role:   bootloader.RoleRunMode,
						Name:   runGrubId,
						Hashes: []string{"run-grub-hash-1", "run-grub-hash-2"},
					},
				},
				Kernel:         "pc-kernel",
				KernelRevision: "500",
				KernelCmdlines: []string{
					"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
				},
				KernelBootFile: runKernel,
			},
			boot.BootChain{
				BrandID:        "my-brand",
				Model:          "my-model-uc20",
				Grade:          "dangerous",
				ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
				AssetChain: []boot.BootAsset{
					{
						Role:   bootloader.RoleRecovery,
						Name:   shimId,
						Hashes: shimHashes,
					},
					{
						Role:   bootloader.RoleRecovery,
						Name:   grubId,
						Hashes: grubHashes,
					},
					{
						Role:   bootloader.RoleRunMode,
						Name:   runGrubId,
						Hashes: []string{"run-grub-hash-1", "run-grub-hash-2"},
					},
				},
				Kernel:         "pc-kernel",
				KernelRevision: "600",
				KernelCmdlines: []string{
					"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
				},
				KernelBootFile: runKernel2,
			},
		)
		if shimId2 != "" && shimId2 != shimId && grubId2 != "" && grubId2 != grubId {
			extraRecoveryBootChains := []boot.BootChain{
				{
					BrandID:        "my-brand",
					Model:          "my-model-uc20",
					Grade:          "dangerous",
					ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
					AssetChain: []boot.BootAsset{
						{
							Role:   bootloader.RoleRecovery,
							Name:   shimId2,
							Hashes: []string{"shim-hash-2"},
						},
						{
							Role:   bootloader.RoleRecovery,
							Name:   grubId2,
							Hashes: []string{"grub-hash-2"},
						},
					},
					Kernel:         "pc-kernel",
					KernelRevision: "1",
					KernelCmdlines: []string{
						"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
					},
					KernelBootFile: kernel,
				},
			}
			extraRecoveryBootChainsForRun := []boot.BootChain{
				{
					BrandID:        "my-brand",
					Model:          "my-model-uc20",
					Grade:          "dangerous",
					ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
					AssetChain: []boot.BootAsset{
						{
							Role:   bootloader.RoleRecovery,
							Name:   shimId2,
							Hashes: []string{"shim-hash-2"},
						},
						{
							Role:   bootloader.RoleRecovery,
							Name:   grubId2,
							Hashes: []string{"grub-hash-2"},
						},
					},
					Kernel:         "pc-kernel",
					KernelRevision: "1",
					KernelCmdlines: []string{
						"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
					},
					KernelBootFile: kernel,
				},
			}
			extraRunBootChains := []boot.BootChain{
				{
					BrandID:        "my-brand",
					Model:          "my-model-uc20",
					Grade:          "dangerous",
					ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
					AssetChain: []boot.BootAsset{
						{
							Role:   bootloader.RoleRecovery,
							Name:   shimId2,
							Hashes: []string{"shim-hash-2"},
						},
						{
							Role:   bootloader.RoleRecovery,
							Name:   grubId2,
							Hashes: []string{"grub-hash-2"},
						},
						{
							Role:   bootloader.RoleRunMode,
							Name:   runGrubId,
							Hashes: []string{"run-grub-hash-1", "run-grub-hash-2"},
						},
					},
					Kernel:         "pc-kernel",
					KernelRevision: "500",
					KernelCmdlines: []string{
						"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
					},
					KernelBootFile: runKernel,
				},
				{
					BrandID:        "my-brand",
					Model:          "my-model-uc20",
					Grade:          "dangerous",
					ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
					AssetChain: []boot.BootAsset{
						{
							Role:   bootloader.RoleRecovery,
							Name:   shimId2,
							Hashes: []string{"shim-hash-2"},
						},
						{
							Role:   bootloader.RoleRecovery,
							Name:   grubId2,
							Hashes: []string{"grub-hash-2"},
						},
						{
							Role:   bootloader.RoleRunMode,
							Name:   runGrubId,
							Hashes: []string{"run-grub-hash-1", "run-grub-hash-2"},
						},
					},
					Kernel:         "pc-kernel",
					KernelRevision: "600",
					KernelCmdlines: []string{
						"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
					},
					KernelBootFile: runKernel2,
				},
			}
			// Let's try to simulate the correct behavior of the caller, where the older chains are always before newer ones
			if shimId == "bootx64.efi" {
				recoveryBootChains = append(recoveryBootChains, extraRecoveryBootChains...)
				recoveryBootChainsForRun = append(recoveryBootChainsForRun, extraRecoveryBootChainsForRun...)
				runBootChains = append(runBootChains, extraRunBootChains...)
			} else {
				recoveryBootChains = append(extraRecoveryBootChains, recoveryBootChains...)
				recoveryBootChainsForRun = append(extraRecoveryBootChainsForRun, recoveryBootChainsForRun...)
				runBootChains = append(extraRunBootChains, runBootChains...)
			}

		}

		params := &boot.ResealKeyForBootChainsParams{
			RunModeBootChains:           runBootChains,
			RecoveryBootChainsForRunKey: recoveryBootChainsForRun,
			RecoveryBootChains:          recoveryBootChains,
			RoleToBlName: map[bootloader.Role]string{
				bootloader.RoleRunMode:  "grub",
				bootloader.RoleRecovery: "grub",
			},
		}

		const expectReseal = false
		err := fdeBackend.ResealKeyForBootChains(device.SealingMethodTPM, rootdir, params, expectReseal)
		if tc.reuseRunPbc && tc.reuseRecoveryPbc {
			// did nothing
			c.Assert(err, IsNil)
			c.Assert(resealKeysCalls, Equals, 0)
			continue
		}
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
		c.Check(pbc, DeepEquals, boot.PredictableBootChains(removeKernelBootFiles(append(recoveryBootChainsForRun, runBootChains...))))

		prevPbc = pbc
		recoveryPbc, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDir, "recovery-boot-chains"))
		c.Assert(err, IsNil)
		if tc.reuseRecoveryPbc {
			c.Check(cnt, Equals, 9)
		} else {
			c.Check(cnt, Equals, 1)
		}
		prevRecoveryPbc = recoveryPbc
		c.Check(recoveryPbc, DeepEquals, boot.PredictableBootChains(removeKernelBootFiles(recoveryBootChains)))
	}
}

func (s *resealTestSuite) TestResealKeyToModeenvRecoveryKeysForGoodSystemsOnly(c *C) {
	// mock asset cache
	mockAssetsCache(c, s.rootdir, "grub", []string{
		"bootx64.efi-shim-hash",
		"grubx64.efi-grub-hash",
		"grubx64.efi-run-grub-hash",
	})

	// set mock key resealing
	resealKeysCalls := 0
	restore := fdeBackend.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		c.Check(params.TPMPolicyAuthKeyFile, Equals, filepath.Join(dirs.SnapSaveDir, "device/fde", "tpm-policy-auth-key"))

		resealKeysCalls++
		c.Assert(params.ModelParams, HasLen, 1)

		// shared parameters
		c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")
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
		shim := bootloader.NewBootFile("", filepath.Join(s.rootdir, "var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash"), bootloader.RoleRecovery)
		grub := bootloader.NewBootFile("", filepath.Join(s.rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash"), bootloader.RoleRecovery)
		kernelGoodRecovery := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
		// kernel from a tried recovery system
		kernelTriedRecovery := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_999.snap", "kernel.efi", bootloader.RoleRecovery)
		// run mode parameters
		runGrub := bootloader.NewBootFile("", filepath.Join(s.rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash"), bootloader.RoleRunMode)
		runKernel := bootloader.NewBootFile(filepath.Join(s.rootdir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode)

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

	kernelGoodRecovery := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
	kernelTriedRecovery := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_999.snap", "kernel.efi", bootloader.RoleRecovery)
	runKernel := bootloader.NewBootFile(filepath.Join(s.rootdir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode)

	runBootChains := []boot.BootChain{
		{
			BrandID:        "my-brand",
			Model:          "my-model-uc20",
			Grade:          "dangerous",
			ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
			AssetChain: []boot.BootAsset{
				{
					Role:   bootloader.RoleRecovery,
					Name:   "bootx64.efi",
					Hashes: []string{"shim-hash"},
				},
				{
					Role:   bootloader.RoleRecovery,
					Name:   "grubx64.efi",
					Hashes: []string{"grub-hash"},
				},
				{
					Role:   bootloader.RoleRunMode,
					Name:   "grubx64.efi",
					Hashes: []string{"run-grub-hash"},
				},
			},
			Kernel:         "pc-kernel",
			KernelRevision: "500",
			KernelCmdlines: []string{
				"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
			},
			KernelBootFile: runKernel,
		},
	}

	recoveryBootChainsForRun := []boot.BootChain{
		{
			BrandID:        "my-brand",
			Model:          "my-model-uc20",
			Grade:          "dangerous",
			ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
			AssetChain: []boot.BootAsset{
				{
					Role:   bootloader.RoleRecovery,
					Name:   "bootx64.efi",
					Hashes: []string{"shim-hash"},
				},
				{
					Role:   bootloader.RoleRecovery,
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
			KernelBootFile: kernelGoodRecovery,
		},
		{
			BrandID:        "my-brand",
			Model:          "my-model-uc20",
			Grade:          "dangerous",
			ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
			AssetChain: []boot.BootAsset{
				{
					Role:   bootloader.RoleRecovery,
					Name:   "bootx64.efi",
					Hashes: []string{"shim-hash"},
				},
				{
					Role:   bootloader.RoleRecovery,
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
			KernelBootFile: kernelTriedRecovery,
		},
	}

	recoveryBootChains := []boot.BootChain{
		{
			BrandID:        "my-brand",
			Model:          "my-model-uc20",
			Grade:          "dangerous",
			ModelSignKeyID: "Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij",
			AssetChain: []boot.BootAsset{
				{
					Role:   bootloader.RoleRecovery,
					Name:   "bootx64.efi",
					Hashes: []string{"shim-hash"},
				},
				{
					Role:   bootloader.RoleRecovery,
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
			KernelBootFile: kernelGoodRecovery,
		},
	}

	params := &boot.ResealKeyForBootChainsParams{
		RunModeBootChains:           runBootChains,
		RecoveryBootChainsForRunKey: recoveryBootChainsForRun,
		RecoveryBootChains:          recoveryBootChains,
		RoleToBlName: map[bootloader.Role]string{
			bootloader.RoleRunMode:  "grub",
			bootloader.RoleRecovery: "grub",
		},
	}

	const expectReseal = false
	err := fdeBackend.ResealKeyForBootChains(device.SealingMethodTPM, s.rootdir, params, expectReseal)
	c.Assert(err, IsNil)
	c.Assert(resealKeysCalls, Equals, 2)

	// verify the boot chains data file for run key
	runPbc, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDir, "boot-chains"))
	c.Assert(err, IsNil)
	c.Assert(cnt, Equals, 1)
	c.Check(runPbc, DeepEquals, boot.ToPredictableBootChains(removeKernelBootFiles(append(runBootChains, recoveryBootChainsForRun...))))
	// recovery boot chains
	recoveryPbc, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDir, "recovery-boot-chains"))
	c.Assert(err, IsNil)
	c.Assert(cnt, Equals, 1)
	c.Check(recoveryPbc, DeepEquals, boot.ToPredictableBootChains(removeKernelBootFiles(recoveryBootChains)))
}

func (s *resealTestSuite) testResealKeyToModeenvWithTryModel(c *C, shimId, grubId string) {
	// mock asset cache
	mockAssetsCache(c, s.rootdir, "grub", []string{
		fmt.Sprintf("%s-shim-hash", shimId),
		fmt.Sprintf("%s-grub-hash", grubId),
		"grubx64.efi-run-grub-hash",
	})

	// set mock key resealing
	resealKeysCalls := 0
	restore := fdeBackend.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		c.Check(params.TPMPolicyAuthKeyFile, Equals, filepath.Join(dirs.SnapSaveDir, "device/fde", "tpm-policy-auth-key"))

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
		shim := bootloader.NewBootFile("", filepath.Join(s.rootdir, fmt.Sprintf("var/lib/snapd/boot-assets/grub/%s-shim-hash", shimId)), bootloader.RoleRecovery)
		grub := bootloader.NewBootFile("", filepath.Join(s.rootdir, fmt.Sprintf("var/lib/snapd/boot-assets/grub/%s-grub-hash", grubId)), bootloader.RoleRecovery)
		kernelOldRecovery := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
		// kernel from a tried recovery system
		kernelNewRecovery := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_999.snap", "kernel.efi", bootloader.RoleRecovery)
		// run mode parameters
		runGrub := bootloader.NewBootFile("", filepath.Join(s.rootdir, "var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash"), bootloader.RoleRunMode)
		runKernel := bootloader.NewBootFile(filepath.Join(s.rootdir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode)

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

	recoveryAssetChain := []boot.BootAsset{{
		Role:   "recovery",
		Name:   shimId,
		Hashes: []string{"shim-hash"},
	}, {
		Role:   "recovery",
		Name:   grubId,
		Hashes: []string{"grub-hash"},
	}}
	runAssetChain := []boot.BootAsset{{
		Role:   "recovery",
		Name:   shimId,
		Hashes: []string{"shim-hash"},
	}, {
		Role:   "recovery",
		Name:   grubId,
		Hashes: []string{"grub-hash"},
	}, {
		Role:   "run-mode",
		Name:   "grubx64.efi",
		Hashes: []string{"run-grub-hash"},
	}}

	kernelOldRecovery := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
	kernelNewRecovery := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_999.snap", "kernel.efi", bootloader.RoleRecovery)
	runKernel := bootloader.NewBootFile(filepath.Join(s.rootdir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode)

	recoveryBootChainsForRun := []boot.BootChain{
		// the current model
		{
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
			KernelBootFile: kernelOldRecovery,
		},
		// the try model
		{
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
			KernelBootFile: kernelNewRecovery,
		},
	}

	runBootChains := []boot.BootChain{
		{
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
			KernelBootFile: runKernel,
		},
		{
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
			KernelBootFile: runKernel,
		},
	}

	recoveryBootChains := []boot.BootChain{
		// recovery keys are sealed to current model only
		{
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
			KernelBootFile: kernelOldRecovery,
		},
	}

	params := &boot.ResealKeyForBootChainsParams{
		RunModeBootChains:           runBootChains,
		RecoveryBootChainsForRunKey: recoveryBootChainsForRun,
		RecoveryBootChains:          recoveryBootChains,
		RoleToBlName: map[bootloader.Role]string{
			bootloader.RoleRunMode:  "grub",
			bootloader.RoleRecovery: "grub",
		},
	}

	const expectReseal = false
	err := fdeBackend.ResealKeyForBootChains(device.SealingMethodTPM, s.rootdir, params, expectReseal)
	c.Assert(err, IsNil)
	c.Assert(resealKeysCalls, Equals, 2)

	// verify the boot chains data file for run key
	runPbc, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDir, "boot-chains"))
	c.Assert(err, IsNil)
	c.Assert(cnt, Equals, 1)
	c.Check(runPbc, DeepEquals, boot.ToPredictableBootChains(removeKernelBootFiles(append(runBootChains, recoveryBootChainsForRun...))))
	// recovery boot chains
	recoveryPbc, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDir, "recovery-boot-chains"))
	c.Assert(err, IsNil)
	c.Assert(cnt, Equals, 1)
	c.Check(recoveryPbc, DeepEquals, boot.ToPredictableBootChains(removeKernelBootFiles(recoveryBootChains)))
}

func (s *resealTestSuite) TestResealKeyToModeenvWithTryModelOldBootChain(c *C) {
	s.testResealKeyToModeenvWithTryModel(c, "bootx64.efi", "grubx64.efi")
}

func (s *resealTestSuite) TestResealKeyToModeenvWithTryModelNewBootChain(c *C) {
	s.testResealKeyToModeenvWithTryModel(c, "ubuntu:shimx64.efi", "ubuntu:grubx64.efi")
}

func (s *resealTestSuite) TestResealKeyToModeenvFallbackCmdline(c *C) {
	err := boot.WriteBootChains(nil, filepath.Join(dirs.SnapFDEDir, "boot-chains"), 9)
	c.Assert(err, IsNil)
	// mock asset cache
	mockAssetsCache(c, s.rootdir, "trusted", []string{
		"asset-asset-hash-1",
	})

	// match one of current kernels
	runKernelBf := bootloader.NewBootFile("/var/lib/snapd/snap/pc-kernel_500.snap", "kernel.efi", bootloader.RoleRunMode)
	// match the seed kernel
	recoveryKernelBf := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)

	bootdir := c.MkDir()
	mtbl := bootloadertest.Mock("trusted", bootdir).WithTrustedAssets()
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
	defer bootloader.Force(nil)

	// set mock key resealing
	resealKeysCalls := 0
	restore := fdeBackend.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
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

	runBootChains := []boot.BootChain{
		{
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
			KernelBootFile: runKernelBf,
		},
	}

	recoveryBootChainsForRun := []boot.BootChain{
		{
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
			KernelBootFile: recoveryKernelBf,
		},
	}

	recoveryBootChains := []boot.BootChain{
		{
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
			KernelBootFile: recoveryKernelBf,
		},
	}

	params := &boot.ResealKeyForBootChainsParams{
		RunModeBootChains:           runBootChains,
		RecoveryBootChainsForRunKey: recoveryBootChainsForRun,
		RecoveryBootChains:          recoveryBootChains,
		RoleToBlName: map[bootloader.Role]string{
			bootloader.RoleRunMode:  "trusted",
			bootloader.RoleRecovery: "trusted",
		},
	}

	const expectReseal = false
	err = fdeBackend.ResealKeyForBootChains(device.SealingMethodTPM, s.rootdir, params, expectReseal)
	c.Assert(err, IsNil)
	c.Assert(resealKeysCalls, Equals, 2)

	// verify the boot chains data file
	pbc, cnt, err := boot.ReadBootChains(filepath.Join(dirs.SnapFDEDir, "boot-chains"))
	c.Assert(err, IsNil)
	c.Assert(cnt, Equals, 10)
	c.Check(pbc, DeepEquals, boot.ToPredictableBootChains(removeKernelBootFiles(append(runBootChains, recoveryBootChainsForRun...))))
}
