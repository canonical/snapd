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

		// set encryption key
		myKey := secboot.EncryptionKey{}
		for i := range myKey {
			myKey[i] = byte(i)
		}

		model := makeMockUC20Model()

		// set recovery kernel
		restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
			if label != "20200825" {
				return nil, nil, fmt.Errorf("invalid system seed label: %q", label)
			}
			kernelSnap := &seed.Snap{
				Path: "/var/lib/snapd/seed/snaps/pc-kernel_1.snap",
			}
			return model, []*seed.Snap{kernelSnap}, nil
		})
		defer restore()

		// set mock key sealing
		sealKeyCalls := 0
		restore = boot.MockSecbootSealKey(func(key secboot.EncryptionKey, params *secboot.SealKeyParams) error {
			sealKeyCalls++
			c.Check(key, DeepEquals, myKey)
			c.Assert(params.ModelParams, HasLen, 1)
			c.Assert(params.ModelParams[0].Model.DisplayName(), Equals, "My Model")
			cachedir := filepath.Join(tmpDir, "var/lib/snapd/boot-assets/grub")
			c.Assert(params.ModelParams[0].EFILoadChains, DeepEquals, [][]bootloader.BootFile{
				// run mode load sequence
				{
					bootloader.NewBootFile("", filepath.Join(cachedir, "bootx64.efi-shim-hash-1"), bootloader.RoleRecovery),
					bootloader.NewBootFile("", filepath.Join(cachedir, "grubx64.efi-grub-hash-1"), bootloader.RoleRecovery),
					bootloader.NewBootFile("", filepath.Join(cachedir, "grubx64.efi-run-grub-hash-1"), bootloader.RoleRunMode),
					bootloader.NewBootFile(filepath.Join(tmpDir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode),
				},
				// recover mode load sequence
				{
					bootloader.NewBootFile("", filepath.Join(cachedir, "bootx64.efi-shim-hash-1"), bootloader.RoleRecovery),
					bootloader.NewBootFile("", filepath.Join(cachedir, "grubx64.efi-grub-hash-1"), bootloader.RoleRecovery),
					bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery),
				},
			})
			c.Assert(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
			})
			return tc.sealErr
		})
		defer restore()

		err = boot.SealKeyToModeenv(myKey, model, modeenv)
		if tc.err == "" {
			c.Assert(err, IsNil)
			c.Assert(sealKeyCalls, Equals, 1)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}
	}
}

func (s *sealSuite) TestRecoverModeLoadSequences(c *C) {
	for _, tc := range []struct {
		assetsMap         boot.BootAssetsMap
		recoverySystem    string
		undefinedKernel   bool
		expectedSequences [][]bootloader.BootFile
		err               string
	}{
		{
			// transition sequences
			recoverySystem: "20200825",
			assetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1", "grub-hash-2"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			expectedSequences: [][]bootloader.BootFile{
				{
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-1", bootloader.RoleRecovery),
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash-1", bootloader.RoleRecovery),
					bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery),
				},
				{
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-1", bootloader.RoleRecovery),
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash-2", bootloader.RoleRecovery),
					bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery),
				},
			},
		},
		{
			// non-transition sequence
			recoverySystem: "20200825",
			assetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			expectedSequences: [][]bootloader.BootFile{
				{
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-1", bootloader.RoleRecovery),
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash-1", bootloader.RoleRecovery),
					bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery),
				},
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

		// set recovery kernel
		restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
			if label != "20200825" {
				return nil, nil, fmt.Errorf("invalid system seed label: %q", label)
			}
			kernelSnap := &seed.Snap{
				Path: "/var/lib/snapd/seed/snaps/pc-kernel_1.snap",
			}
			return nil, []*seed.Snap{kernelSnap}, nil
		})
		defer restore()

		err := createMockGrubCfg(tmpDir)
		c.Assert(err, IsNil)

		bl, err := bootloader.Find(tmpDir, &bootloader.Options{Role: bootloader.RoleRecovery})
		c.Assert(err, IsNil)

		modeenv := &boot.Modeenv{
			RecoverySystem:                   tc.recoverySystem,
			CurrentTrustedRecoveryBootAssets: tc.assetsMap,
		}

		sequences, err := boot.RecoverModeLoadSequences(bl, modeenv)
		if tc.err == "" {
			c.Assert(err, IsNil)
			c.Assert(sequences, DeepEquals, tc.expectedSequences)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}
	}
}

func (s *sealSuite) TestRunModeLoadSequences(c *C) {
	for _, tc := range []struct {
		recoveryAssetsMap boot.BootAssetsMap
		assetsMap         boot.BootAssetsMap
		kernels           []string
		recoverySystem    string
		expectedSequences [][]bootloader.BootFile
		err               string
	}{
		{
			// transition sequences with new system bootloader
			recoverySystem: "20200825",
			recoveryAssetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			assetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"run-grub-hash-1", "run-grub-hash-2"},
			},
			kernels: []string{"pc-kernel_500.snap"},
			expectedSequences: [][]bootloader.BootFile{
				{
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-1", bootloader.RoleRecovery),
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash-1", bootloader.RoleRecovery),
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash-1", bootloader.RoleRunMode),
					bootloader.NewBootFile("/var/lib/snapd/snaps/pc-kernel_500.snap", "kernel.efi", bootloader.RoleRunMode),
				},
				{
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-1", bootloader.RoleRecovery),
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash-1", bootloader.RoleRecovery),
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash-2", bootloader.RoleRunMode),
					bootloader.NewBootFile("/var/lib/snapd/snaps/pc-kernel_500.snap", "kernel.efi", bootloader.RoleRunMode),
				},
			},
		},
		{
			// transition sequences with new kernel
			recoverySystem: "20200825",
			recoveryAssetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			assetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"run-grub-hash-1"},
			},
			kernels: []string{"pc-kernel_500.snap", "pc-kernel_501.snap"},
			expectedSequences: [][]bootloader.BootFile{
				{
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-1", bootloader.RoleRecovery),
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash-1", bootloader.RoleRecovery),
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash-1", bootloader.RoleRunMode),
					bootloader.NewBootFile("/var/lib/snapd/snaps/pc-kernel_500.snap", "kernel.efi", bootloader.RoleRunMode),
				},
				{
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-1", bootloader.RoleRecovery),
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash-1", bootloader.RoleRecovery),
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash-1", bootloader.RoleRunMode),
					bootloader.NewBootFile("/var/lib/snapd/snaps/pc-kernel_501.snap", "kernel.efi", bootloader.RoleRunMode),
				},
			},
		},
		{
			// no transition sequence
			recoverySystem: "20200825",
			recoveryAssetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			assetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"run-grub-hash-1"},
			},
			kernels: []string{"pc-kernel_500.snap"},
			expectedSequences: [][]bootloader.BootFile{
				{
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-1", bootloader.RoleRecovery),
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash-1", bootloader.RoleRecovery),
					bootloader.NewBootFile("", "/var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash-1", bootloader.RoleRunMode),
					bootloader.NewBootFile("/var/lib/snapd/snaps/pc-kernel_500.snap", "kernel.efi", bootloader.RoleRunMode),
				},
			},
		},
		{
			// no run mode assets
			recoverySystem: "20200825",
			recoveryAssetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			kernels: []string{"pc-kernel_500.snap"},
			err:     "cannot find asset grubx64.efi in modeenv",
		},
		{
			// no kernels listed in modeenv
			recoverySystem: "20200825",
			recoveryAssetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"grub-hash-1"},
				"bootx64.efi": []string{"shim-hash-1"},
			},
			assetsMap: boot.BootAssetsMap{
				"grubx64.efi": []string{"run-grub-hash-1"},
			},
			err: "invalid number of kernels in modeenv",
		},
	} {
		tmpDir := c.MkDir()

		err := createMockGrubCfg(tmpDir)
		c.Assert(err, IsNil)

		rbl, err := bootloader.Find(tmpDir, &bootloader.Options{Role: bootloader.RoleRecovery})
		c.Assert(err, IsNil)

		bl, err := bootloader.Find(tmpDir, &bootloader.Options{NoSlashBoot: true, Role: bootloader.RoleRunMode})
		c.Assert(err, IsNil)

		modeenv := &boot.Modeenv{
			RecoverySystem:                   tc.recoverySystem,
			CurrentTrustedRecoveryBootAssets: tc.recoveryAssetsMap,
			CurrentTrustedBootAssets:         tc.assetsMap,
			CurrentKernels:                   tc.kernels,
		}

		sequences, err := boot.RunModeLoadSequences(rbl, bl, modeenv)
		if tc.err == "" {
			c.Assert(err, IsNil)
			c.Assert(sequences, DeepEquals, tc.expectedSequences)
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
