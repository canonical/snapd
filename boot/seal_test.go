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
			c.Assert(params.ModelParams[0].Model.DisplayName(), Equals, "My Model")

			bfs := bootFiles(c, params.ModelParams[0].EFILoadChains)
			c.Assert(bfs, DeepEquals, []bootloader.BootFile{
				bootloader.NewBootFile("", filepath.Join(tmpDir, "var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-1"), bootloader.RoleRecovery),
				bootloader.NewBootFile("", filepath.Join(tmpDir, "var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash-1"), bootloader.RoleRecovery),
				bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery),
			})
			c.Assert(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=recover snapd_recovery_system=20200825 console=ttyS0 console=tty1 panic=-1",
			})
			bfs = bootFiles(c, params.ModelParams[1].EFILoadChains)
			c.Assert(bfs, DeepEquals, []bootloader.BootFile{
				bootloader.NewBootFile("", filepath.Join(tmpDir, "var/lib/snapd/boot-assets/grub/bootx64.efi-shim-hash-1"), bootloader.RoleRecovery),
				bootloader.NewBootFile("", filepath.Join(tmpDir, "var/lib/snapd/boot-assets/grub/grubx64.efi-grub-hash-1"), bootloader.RoleRecovery),
				bootloader.NewBootFile("", filepath.Join(tmpDir, "var/lib/snapd/boot-assets/grub/grubx64.efi-run-grub-hash-1"), bootloader.RoleRunMode),
				bootloader.NewBootFile(filepath.Join(tmpDir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode),
			})
			c.Assert(params.ModelParams[1].KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
			})
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

// TODO:UC20: stop uisng this and switch to check actual trees when
// that makes sense
func bootFiles(c *C, chains []*secboot.LoadChain) (bfs []bootloader.BootFile) {
	for {
		c.Assert(chains, HasLen, 1)
		chain := chains[0]

		bfs = append(bfs, *chain.BootFile)

		if len(chain.Next) == 0 {
			break
		}
		chains = chain.Next
	}
	return bfs
}

func createMockGrubCfg(baseDir string) error {
	cfg := filepath.Join(baseDir, "EFI/ubuntu/grub.cfg")
	if err := os.MkdirAll(filepath.Dir(cfg), 0755); err != nil {
		return err
	}
	return ioutil.WriteFile(cfg, []byte("# Snapd-Boot-Config-Edition: 1\n"), 0644)
}
