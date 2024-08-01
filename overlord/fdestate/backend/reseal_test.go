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

func removeKernelBootFiles(bootChains []boot.BootChain) []boot.BootChain {
	var ret []boot.BootChain
	for _, v := range bootChains {
		v.KernelBootFile = bootloader.BootFile{}
		ret = append(ret, v)
	}
	return ret
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
