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
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/systemd"
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

func checkSysrootMount(c *C, onCore24Plus bool, systemctlNumCalls int, systemctlArgs [][]string) {
	// Check sysroot mount unit bits
	unitDir := dirs.SnapRuntimeServicesDirUnder(dirs.GlobalRootDir)
	baseUnitPath := filepath.Join(unitDir, "sysroot.mount")
	if onCore24Plus {
		c.Assert(baseUnitPath, testutil.FileEquals, `[Unit]
DefaultDependencies=no
Before=initrd-root-fs.target
After=snap-initramfs-mounts.service
Before=umount.target
Conflicts=umount.target

[Mount]
What=/run/mnt/data
Where=/sysroot
Type=none
Options=bind
`)
		symlinkPath := filepath.Join(unitDir, "initrd-root-fs.target.wants", "sysroot.mount")
		target, err := os.Readlink(symlinkPath)
		c.Assert(err, IsNil)
		c.Assert(target, Equals, "../sysroot.mount")

		c.Assert(systemctlNumCalls, Equals, 2)
		c.Assert(systemctlArgs, DeepEquals, [][]string{{"daemon-reload"},
			{"start", "--no-block", "initrd-root-fs.target"}})
	} else {
		// sysroot.mount is actually present in 24- but as a static
		// file on the base. We expect it to be absent as far as the
		// testsuite is concerned.
		c.Assert(baseUnitPath, testutil.FileAbsent)
		c.Assert(systemctlNumCalls, Equals, 0)
	}
}

func (s *initramfsCVMMountsSuite) TestInitramfsMountsRunCVMModeHappy(c *C) {
	onCore24Plus := false
	s.testInitramfsMountsRunCVMModeHappy(c, onCore24Plus)
}

func (s *initramfsCVMMountsSuite) TestInitramfsMountsRunCVMModeOn24PlusHappy(c *C) {
	onCore24Plus := true
	s.testInitramfsMountsRunCVMModeHappy(c, onCore24Plus)
}

func (s *initramfsCVMMountsSuite) testInitramfsMountsRunCVMModeHappy(c *C, onCore24Plus bool) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=cloudimg-rootfs")

	restore := main.MockPartitionUUIDForBootedKernelDisk("specific-ubuntu-seed-partuuid")
	defer restore()

	restore = main.MockOsGetenv(func(envVar string) string {
		if onCore24Plus && envVar == "CORE24_PLUS_INITRAMFS" {
			return "1"
		}
		return ""
	})
	defer restore()

	var systemctlArgs [][]string
	systemctlNumCalls := 0
	restore = systemd.MockSystemctl(func(args ...string) (buf []byte, err error) {
		systemctlArgs = append(systemctlArgs, args)
		systemctlNumCalls++
		return nil, nil
	})
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
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFiles []*secboot.LegacyKeyFile, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		c.Assert(provisionTPMCVMCalled, Equals, true)
		c.Assert(name, Equals, "cloudimg-rootfs")
		c.Assert(sealedEncryptionKeyFiles, HasLen, 1)
		c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/cloudimg-rootfs.sealed-key"))
		c.Assert(opts.AllowRecoveryKey, Equals, true)
		c.Assert(opts.WhichModel, IsNil)

		cloudimgActivated = true
		// return true because we are using an encrypted device
		return happyUnlocked("cloudimg-rootfs", secboot.UnlockedWithSealedKey, "external:legacy"), nil
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

	checkSysrootMount(c, onCore24Plus, systemctlNumCalls, systemctlArgs)
}

func (s *initramfsCVMMountsSuite) TestInitramfsMountsRunCVMModeEphemeralOverlayHappy(c *C) {
	onCore24Plus := false
	s.testInitramfsMountsRunCVMModeEphemeralOverlayHappy(c, onCore24Plus)
}

func (s *initramfsCVMMountsSuite) TestInitramfsMountsRunCVMModeEphemeralOverlayOn24PlusHappy(c *C) {
	onCore24Plus := true
	s.testInitramfsMountsRunCVMModeEphemeralOverlayHappy(c, onCore24Plus)
}

func (s *initramfsCVMMountsSuite) testInitramfsMountsRunCVMModeEphemeralOverlayHappy(c *C, onCore24Plus bool) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=cloudimg-rootfs")

	restore := main.MockPartitionUUIDForBootedKernelDisk("specific-ubuntu-seed-partuuid")
	defer restore()

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultCVMDiskVerity,
		},
	)
	defer restore()

	restore = main.MockOsGetenv(func(envVar string) string {
		if onCore24Plus && envVar == "CORE24_PLUS_INITRAMFS" {
			return "1"
		}
		return ""
	})
	defer restore()

	var systemctlArgs [][]string
	systemctlNumCalls := 0
	restore = systemd.MockSystemctl(func(args ...string) (buf []byte, err error) {
		systemctlArgs = append(systemctlArgs, args)
		systemctlNumCalls++
		return nil, nil
	})
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
	manifestJson := fmt.Sprintf(`{"partitions":[{"label":"cloudimg-rootfs","root_hash":%q,"read_only":true}]}`, expectedRootHash)

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
	restore = main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(activateContext secboot.ActivateContext, disk disks.Disk, name string, sealedEncryptionKeyFiles []*secboot.LegacyKeyFile, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		c.Assert(provisionTPMCVMCalled, Equals, true)
		c.Assert(name, Equals, "cloudimg-rootfs")
		c.Assert(sealedEncryptionKeyFiles, HasLen, 1)
		c.Assert(sealedEncryptionKeyFiles[0].Path, Equals, filepath.Join(s.tmpDir, "run/mnt/ubuntu-seed/device/fde/cloudimg-rootfs.sealed-key"))
		c.Assert(opts.AllowRecoveryKey, Equals, true)
		c.Assert(opts.WhichModel, IsNil)

		cloudimgActivated = true
		// return true because we are using an encrypted device
		// return happyUnlocked("cloudimg-rootfs", secboot.UnlockedWithSealedKey), nil
		return foundUnencrypted("cloudimg-rootfs"), nil
	})
	defer restore()

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Check(s.Stdout.String(), Equals, "")

	writableTmpCreated := osutil.IsDirectory(filepath.Join(boot.InitramfsRunMntDir, "writable-tmp"))
	c.Check(writableTmpCreated, Equals, true)

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
			"--fsck=no",
			"--type=overlay",
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

	checkSysrootMount(c, onCore24Plus, systemctlNumCalls, systemctlArgs)
}

func (s *initramfsCVMMountsSuite) TestGenerateMountsFromManifest(c *C) {
	testCases := []struct {
		im       string
		disk     *disks.MockDiskMapping
		writable string
		err      string
	}{
		// Valid, ephemeral disk
		{
			`{"partitions":[{"label":"cloudimg-rootfs","root_hash":"000","read_only":true}]}`,
			defaultCVMDiskVerity,
			"writable-tmp",
			"",
		},
		// Valid, non-ephemeral disk
		{
			`{"partitions":[{"label":"cloudimg-rootfs","root_hash":"000","read_only":true},{"label":"writable"}]}`,
			defaultCVMDiskVerity,
			"writable",
			"",
		},
		// Valid, missing root hash (to test this won't fail early when a root hash is missing)
		{
			`{"partitions":[{"label":"cloudimg-rootfs","read_only":true}]}`,
			defaultCVMDiskVerity,
			"writable-tmp",
			"",
		},
		// Invalid, missing ro partition
		{
			`{"partitions":[{"label":"cloudimg-rootfs"}]}`,
			defaultCVMDiskVerity,
			"writable",
			"manifest doesn't contain any partition marked as read-only",
		},
		// Invalid, 2 ro partitions
		{
			`{"partitions":[{"label":"cloudimg-rootfs","root_hash":"000","read_only":true},{"label":"test", "read_only":true}]}`,
			defaultCVMDiskVerity,
			"writable",
			"manifest contains multiple partitions marked as read-only",
		},
		// Invalid, 2 rw partitions
		{
			`{"partitions":[{"label":"cloudimg-rootfs","root_hash":"000","read_only":true},{"label":"test"},{"label":"test2"}]}`,
			defaultCVMDiskVerity,
			"writable",
			"manifest contains multiple writable partitions",
		},
	}

	for _, tc := range testCases {
		im, err := main.ParseImageManifest([]byte(tc.im))
		c.Assert(err, IsNil)

		pm, err := main.GenerateMountsFromManifest(im, tc.disk)
		if err != nil {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(pm[1].GptLabel, Equals, tc.writable)
		}
	}
}

func (s *initramfsCVMMountsSuite) TestCreateOverlayDirs(c *C) {
	testCases := []struct {
		createPath string
		perm       os.FileMode
		targetPath string
		err        string
	}{
		// Valid, target path exists already
		{
			"target",
			0755,
			"target",
			"",
		},
		// Invalid, target path doesn't exist
		{
			"other",
			0755,
			"target",
			"stat %s: no such file or directory",
		},
		// Invalid, target path doesn't exist and can't be created due to
		// a permission issue in its containing folder
		{
			"foo",
			0700,
			"foo/target",
			"stat %s: permission denied",
		},
		// Invalid, target path exists but with restrictive permissions
		{
			"target",
			0700,
			"target",
			"mkdir %s/upper: permission denied",
		},
	}

	for _, tc := range testCases {
		createPath := filepath.Join(dirs.GlobalRootDir, tc.createPath)
		err := os.Mkdir(createPath, tc.perm)
		c.Assert(err, IsNil)

		targetPath := filepath.Join(dirs.GlobalRootDir, tc.targetPath)
		err = main.CreateOverlayDirs(targetPath)
		if err != nil {
			c.Check(err, ErrorMatches, fmt.Sprintf(tc.err, targetPath))
		} else {
			_, err2 := os.Stat(filepath.Join(targetPath, "upper"))
			c.Check(err2, IsNil)

			_, err2 = os.Stat(filepath.Join(targetPath, "work"))
			c.Check(err2, IsNil)
		}

		err = os.RemoveAll(createPath)
		c.Assert(err, IsNil)
		err = os.RemoveAll(targetPath)
		c.Assert(err, IsNil)
	}
}
