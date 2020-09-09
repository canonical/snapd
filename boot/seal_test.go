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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
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

func (s *sealSuite) TestSealKeyToModeenv(c *C) {
	for _, tc := range []struct {
		sealErr error
		err     string
	}{
		{sealErr: nil, err: ""},
		{sealErr: errors.New("seal error"), err: "cannot seal the encryption key: seal error"},
	} {
		tmpDir := c.MkDir()
		dirs.SetRootDir(tmpDir)
		defer dirs.SetRootDir("")

		err := createMockGrubCfg(filepath.Join(tmpDir, "run/mnt/ubuntu-seed"))
		c.Assert(err, IsNil)

		err = createMockGrubCfg(filepath.Join(tmpDir, "run/mnt/ubuntu-boot"))
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
		}

		// mock asset cache
		p := filepath.Join(tmpDir, "var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-1")
		err = os.MkdirAll(filepath.Dir(p), 0755)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(p, nil, 0644)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(filepath.Join(tmpDir, "var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash-1"), nil, 0644)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(filepath.Join(tmpDir, "var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash-1"), nil, 0644)
		c.Assert(err, IsNil)

		// set encryption key
		myKey := secboot.EncryptionKey{}
		for i := range myKey {
			myKey[i] = byte(i)
		}

		model := makeMockUC20Model()

		// set a mock recovery kernel
		readSystemEssentialCalls := 0
		restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
			readSystemEssentialCalls++
			kernelSnap := &seed.Snap{
				Path: "/var/lib/snapd/seed/snaps/pc-kernel_1.snap",
				SideInfo: &snap.SideInfo{
					Revision: snap.Revision{N: 0},
				},
			}
			return model, []*seed.Snap{kernelSnap}, nil
		})
		defer restore()

		// set mock key sealing
		sealKeyCalls := 0
		restore = boot.MockSecbootSealKey(func(key secboot.EncryptionKey, params *secboot.SealKeyParams) error {
			sealKeyCalls++
			c.Check(key, DeepEquals, myKey)
			c.Assert(params.ModelParams, HasLen, 2)

			// recovery parameters
			shim := bootloader.NewBootFile("", filepath.Join(tmpDir, "var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-1"), bootloader.RoleRecovery)
			grub := bootloader.NewBootFile("", filepath.Join(tmpDir, "var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash-1"), bootloader.RoleRecovery)
			kernel := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)

			c.Assert(params.ModelParams[0].EFILoadChains, DeepEquals, []*secboot.LoadChain{
				secboot.NewLoadChain(shim, secboot.NewLoadChain(grub, secboot.NewLoadChain(kernel))),
			})
			c.Assert(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
			})
			c.Assert(params.ModelParams[0].Model.DisplayName(), Equals, "My Model")

			// run mode parameters
			runGrub := bootloader.NewBootFile("", filepath.Join(tmpDir, "var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash-1"), bootloader.RoleRunMode)
			runKernel := bootloader.NewBootFile(filepath.Join(tmpDir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode)

			c.Assert(params.ModelParams[1].EFILoadChains, DeepEquals, []*secboot.LoadChain{
				secboot.NewLoadChain(shim, secboot.NewLoadChain(grub, secboot.NewLoadChain(runGrub, secboot.NewLoadChain(runKernel)))),
			})
			c.Assert(params.ModelParams[1].KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
			})
			c.Assert(params.ModelParams[1].Model.DisplayName(), Equals, "My Model")

			return tc.sealErr
		})
		defer restore()

		err = boot.SealKeyToModeenv(myKey, model, modeenv)
		c.Assert(sealKeyCalls, Equals, 1)
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}
	}
}

func (s *sealSuite) TestBuildRecoveryBootChain(c *C) {
	for _, tc := range []struct {
		assetsMap       boot.BootAssetsMap
		recoverySystem  string
		undefinedKernel bool
		expectedAssets  []boot.BootAsset
		err             string
	}{
		{
			// transition sequences
			recoverySystem: "20200825",
			assetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1", "grub-hash-2"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			expectedAssets: []boot.BootAsset{
				{Role: bootloader.RoleRecovery, Name: "bootx64.efi", Hashes: []string{"shim-hash-1"}},
				{Role: bootloader.RoleRecovery, Name: "grubx64.efi", Hashes: []string{"grub-hash-1", "grub-hash-2"}},
			},
		},
		{
			// non-transition sequence
			recoverySystem: "20200825",
			assetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			expectedAssets: []boot.BootAsset{
				{Role: bootloader.RoleRecovery, Name: "bootx64.efi", Hashes: []string{"shim-hash-1"}},
				{Role: bootloader.RoleRecovery, Name: "grubx64.efi", Hashes: []string{"grub-hash-1"}},
			},
		},
		{
			// invalid recovery system label
			recoverySystem: "0",
			assetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			err: `invalid system seed label: "0"`,
		},
	} {
		tmpDir := c.MkDir()
		dirs.SetRootDir(tmpDir)
		defer dirs.SetRootDir("")

		// set recovery kernel
		restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
			if label != "20200825" {
				return nil, nil, fmt.Errorf("invalid system seed label: %q", label)
			}
			kernelSnap := &seed.Snap{
				Path:     "/var/lib/snapd/seed/snaps/pc-kernel_1.snap",
				SideInfo: &snap.SideInfo{Revision: snap.Revision{N: 1}},
			}
			return nil, []*seed.Snap{kernelSnap}, nil
		})
		defer restore()

		grubDir := filepath.Join(tmpDir, "run/mnt/ubuntu-seed")
		err := createMockGrubCfg(grubDir)
		c.Assert(err, IsNil)

		bl, err := bootloader.Find(grubDir, &bootloader.Options{Role: bootloader.RoleRecovery})
		c.Assert(err, IsNil)

		model := makeMockUC20Model()

		modeenv := &boot.Modeenv{
			RecoverySystem:                   tc.recoverySystem,
			CurrentTrustedRecoveryBootAssets: tc.assetsMap,
		}

		bc, err := boot.BuildRecoveryBootChain(bl, model, modeenv)
		if tc.err == "" {
			c.Assert(err, IsNil)
			c.Assert(bc.AssetChain, DeepEquals, tc.expectedAssets)
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
