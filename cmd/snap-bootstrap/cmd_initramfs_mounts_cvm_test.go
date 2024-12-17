// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2024 Canonical Ltd
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

package main_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	main "github.com/snapcore/snapd/cmd/snap-bootstrap"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/testutil"
)

var (
	cvmEncPart = disks.Partition{
		FilesystemLabel:  "cloudimg-rootfs-enc",
		PartitionUUID:    "cloudimg-rootfs-enc-partuuid",
		KernelDeviceNode: "/dev/sda1",
	}

	cvmPart = disks.Partition{
		FilesystemLabel:  "cloudimg-rootfs",
		PartitionUUID:    "cloudimg-rootfs-partuuid",
		KernelDeviceNode: "/dev/sda1",
	}

	cvmVerityPart = disks.Partition{
		PartitionLabel:   "cloudimg-rootfs-verity",
		PartitionUUID:    "cloudimg-rootfs-verity-partuuid",
		KernelDeviceNode: "/dev/sda13",
	}

	defaultCVMDisk = &disks.MockDiskMapping{
		Structure: []disks.Partition{
			seedPart,
			cvmEncPart,
		},
		DiskHasPartitions: true,
		DevNum:            "defaultCVMDev",
	}

	defaultCVMDiskVerity = &disks.MockDiskMapping{
		Structure: []disks.Partition{
			seedPart,
			cvmPart,
			cvmVerityPart,
		},
		DiskHasPartitions: true,
		DevNum:            "defaultCVMDevVerity",
	}
)

type initramfsCVMMountsSuite struct {
	baseInitramfsMountsSuite
}

var _ = Suite(&initramfsCVMMountsSuite{})

func (s *initramfsCVMMountsSuite) SetUpTest(c *C) {
	s.baseInitramfsMountsSuite.SetUpTest(c)
	s.AddCleanup(main.MockSecbootProvisionForCVM(func(_ string) error {
		return nil
	}))
}

func (s *initramfsCVMMountsSuite) TestInitramfsMountsRunCVMModeHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=cloudimg-rootfs")

	restore := main.MockPartitionUUIDForBootedKernelDisk("specific-ubuntu-seed-partuuid")
	defer restore()

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}:                    defaultCVMDisk,
			{Mountpoint: boot.InitramfsDataDir, IsDecryptedDevice: true}: defaultCVMDisk,
		},
	)
	defer restore()

	// don't do anything from systemd-mount, we verify the arguments passed at
	// the end with cmd.Calls
	cmd := testutil.MockCommand(c, "systemd-mount", ``)
	defer cmd.Restore()

	// mock that in turn, /run/mnt/ubuntu-boot, /run/mnt/ubuntu-seed, etc. are
	// mounted
	n := 0
	restore = main.MockOsutilIsMounted(func(where string) (bool, error) {
		n++
		switch n {
		// first call for each mount returns false, then returns true, this
		// tests in the case where systemd is racy / inconsistent and things
		// aren't mounted by the time systemd-mount returns
		case 1, 2:
			c.Assert(where, Equals, boot.InitramfsUbuntuSeedDir)
		case 3, 4:
			c.Assert(where, Equals, boot.InitramfsDataDir)
		case 5, 6:
			c.Assert(where, Equals, boot.InitramfsUbuntuSeedDir)
		default:
			c.Errorf("unexpected IsMounted check on %s", where)
			return false, fmt.Errorf("unexpected IsMounted check on %s", where)
		}
		return n%2 == 0, nil
	})
	defer restore()

	// Mock the call to TPMCVM, to ensure that TPM provisioning is
	// done before unlock attempt
	provisionTPMCVMCalled := false
	restore = main.MockSecbootProvisionForCVM(func(_ string) error {
		// Ensure this function is only called once
		c.Assert(provisionTPMCVMCalled, Equals, false)
		provisionTPMCVMCalled = true
		return nil
	})
	defer restore()

	cloudimgActivated := false
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		c.Assert(provisionTPMCVMCalled, Equals, true)
		c.Assert(name, Equals, "cloudimg-rootfs")
		c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/cloudimg-rootfs.sealed-key"))
		c.Assert(opts.AllowRecoveryKey, Equals, true)
		c.Assert(opts.WhichModel, IsNil)

		cloudimgActivated = true
		// return true because we are using an encrypted device
		return happyUnlocked("cloudimg-rootfs", secboot.UnlockedWithSealedKey), nil
	})
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Check(s.Stdout.String(), Equals, "")

	// 2 per mountpoint + 1 more for cross check
	c.Assert(n, Equals, 5)

	// failed to use mockSystemdMountSequence way of asserting this
	// note that other test cases also mix & match using
	// mockSystemdMountSequence & DeepEquals
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{
			"systemd-mount",
			"/dev/disk/by-partuuid/specific-ubuntu-seed-partuuid",
			boot.InitramfsUbuntuSeedDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
			"--options=private",
			"--property=Before=initrd-fs.target",
		},
		{
			"systemd-mount",
			"/dev/mapper/cloudimg-rootfs-random",
			boot.InitramfsDataDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
			"--property=Before=initrd-fs.target",
		},
		{
			"systemd-mount",
			boot.InitramfsUbuntuSeedDir,
			"--umount",
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		},
	})

	c.Check(provisionTPMCVMCalled, Equals, true)
	c.Check(cloudimgActivated, Equals, true)
}

func (s *initramfsCVMMountsSuite) TestInitramfsMountsRunCVMModeEphemeralOverlayHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=cloudimg-rootfs")

	restore := main.MockPartitionUUIDForBootedKernelDisk("specific-ubuntu-seed-partuuid")
	defer restore()

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultCVMDiskVerity,
		},
	)
	defer restore()

	// don't do anything from systemd-mount, we verify the arguments passed at
	// the end with cmd.Calls
	cmd := testutil.MockCommand(c, "systemd-mount", ``)
	defer cmd.Restore()

	// mock that in turn, /run/mnt/ubuntu-seed, read-only /run/mnt/cloudimg-rootfs, ephemeral tmpfs /run/mnt/writable-tmp
	// and the overlayfs are mounted
	n := 0
	restore = main.MockOsutilIsMounted(func(where string) (bool, error) {
		n++
		switch n {
		// first call for each mount returns false, then returns true, this
		// tests in the case where systemd is racy / inconsistent and things
		// aren't mounted by the time systemd-mount returns
		case 1, 2:
			c.Assert(where, Equals, boot.InitramfsUbuntuSeedDir)
		case 3, 4:
			c.Assert(where, Equals, filepath.Join(boot.InitramfsRunMntDir, "cloudimg-rootfs"))
		case 5, 6:
			c.Assert(where, Equals, filepath.Join(boot.InitramfsRunMntDir, "writable-tmp"))
		case 7, 8:
			c.Assert(where, Equals, boot.InitramfsDataDir)
		case 9, 10:
			c.Assert(where, Equals, boot.InitramfsUbuntuSeedDir)
		default:
			c.Errorf("unexpected IsMounted check on %s", where)
			return false, fmt.Errorf("unexpected IsMounted check on %s", where)
		}
		return n%2 == 0, nil
	})
	defer restore()

	expectedRootHash := "000"
	manifestPath := filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/ubuntu")
	manifestJson := fmt.Sprintf(`{"partitions":[{"label":"cloudimg-rootfs","root_hash":%q,"overlay":"lowerdir"}]}`, expectedRootHash)

	err := os.MkdirAll(manifestPath, 0755)
	c.Assert(err, IsNil)

	err = os.WriteFile(filepath.Join(manifestPath, "manifest.json"), []byte(manifestJson), 0644)
	c.Assert(err, IsNil)

	// Mock the call to TPMCVM, to ensure that TPM provisioning is
	// done before unlock attempt
	provisionTPMCVMCalled := false
	restore = main.MockSecbootProvisionForCVM(func(_ string) error {
		// Ensure this function is only called once
		c.Assert(provisionTPMCVMCalled, Equals, false)
		provisionTPMCVMCalled = true
		return nil
	})
	defer restore()

	cloudimgActivated := false
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		c.Assert(provisionTPMCVMCalled, Equals, true)
		c.Assert(name, Equals, "cloudimg-rootfs")
		c.Assert(sealedEncryptionKeyFile, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/cloudimg-rootfs.sealed-key"))
		c.Assert(opts.AllowRecoveryKey, Equals, true)
		c.Assert(opts.WhichModel, IsNil)

		cloudimgActivated = true
		// return true because we are using an encrypted device
		// return happyUnlocked("cloudimg-rootfs", secboot.UnlockedWithSealedKey), nil
		return foundUnencrypted("cloudimg-rootfs"), nil
	})
	defer restore()

	// Mock the call to create the necessary dirs for the overlay on the ephemeral tmpfs.
	restore = main.MockCreateOverlayDirs(func(path string) error {
		c.Assert(path, Equals, filepath.Join(boot.InitramfsRunMntDir, "writable-tmp"))
		return nil
	})
	defer restore()

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Check(s.Stdout.String(), Equals, "")

	// 2 per mountpoint + 1 more for cross check
	c.Assert(n, Equals, 9)

	// failed to use mockSystemdMountSequence way of asserting this
	// note that other test cases also mix & match using
	// mockSystemdMountSequence & DeepEquals
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{
			"systemd-mount",
			"/dev/disk/by-partuuid/specific-ubuntu-seed-partuuid",
			boot.InitramfsUbuntuSeedDir,
			"--no-pager",
			"--no-ask-password",
			"--fsck=yes",
			"--options=private",
			"--property=Before=initrd-fs.target",
		},
		{
			"systemd-mount",
			"/dev/disk/by-partuuid/cloudimg-rootfs-partuuid",
			filepath.Join(boot.InitramfsRunMntDir, "cloudimg-rootfs"),
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
			"--options=verity.roothash=" + expectedRootHash + ",verity.hashdevice=/dev/sda13",
			"--property=Before=initrd-fs.target",
		},
		{
			"systemd-mount",
			"tmpfs",
			filepath.Join(boot.InitramfsRunMntDir, "writable-tmp"),
			"--no-pager",
			"--no-ask-password",
			"--type=tmpfs",
			"--fsck=no",
			"--property=Before=initrd-fs.target",
		},
		{
			"systemd-mount",
			"/dev/disk/by-partuuid/cloudimg-rootfs-partuuid",
			boot.InitramfsDataDir,
			"--no-pager",
			"--no-ask-password",
			"--type=overlay",
			"--fsck=no",
			"--options=lowerdir=" +
				filepath.Join(boot.InitramfsRunMntDir, "cloudimg-rootfs") +
				",upperdir=" + filepath.Join(boot.InitramfsRunMntDir, "writable-tmp", "upper") +
				",workdir=" + filepath.Join(boot.InitramfsRunMntDir, "writable-tmp", "work"),
			"--property=Before=initrd-fs.target",
		},
		{
			"systemd-mount",
			boot.InitramfsUbuntuSeedDir,
			"--umount",
			"--no-pager",
			"--no-ask-password",
			"--fsck=no",
		},
	})

	c.Check(provisionTPMCVMCalled, Equals, true)
	c.Check(cloudimgActivated, Equals, true)
}
