// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package bootloader_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/bootloader/efi"
	"github.com/snapcore/snapd/bootloader/ubootenv"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/osutil/kcmdline"
)

type ubootpartTestSuite struct {
	baseBootenvTestSuite
}

var _ = Suite(&ubootpartTestSuite{})

func (s *ubootpartTestSuite) TestNewUbootPart(c *C) {
	// no files means bl is not present, but we can still create the bl object
	opts := &bootloader.Options{PrepareImageTime: true}
	u := bootloader.NewUbootPart(s.rootdir, opts)
	c.Assert(u, NotNil)
	c.Assert(u.Name(), Equals, "ubootpart")

	present, err := u.Present()
	c.Assert(err, IsNil)
	c.Assert(present, Equals, false)

	// now with files present, the bl is present
	bootloader.MockUbootPartFiles(c, s.rootdir, opts)
	present, err = u.Present()
	c.Assert(err, IsNil)
	c.Assert(present, Equals, true)
}

func (s *ubootpartTestSuite) TestUbootPartGetSetEnvVar(c *C) {
	opts := &bootloader.Options{PrepareImageTime: true}
	bootloader.MockUbootPartFiles(c, s.rootdir, opts)
	u := bootloader.NewUbootPart(s.rootdir, opts)
	c.Assert(u, NotNil)
	testBootloaderGetSetEnvVar(c, u)
}

func (s *ubootpartTestSuite) TestUbootPartSetBootVarFwEnv(c *C) {
	opts := &bootloader.Options{PrepareImageTime: true}
	bootloader.MockUbootPartFiles(c, s.rootdir, opts)
	u := bootloader.NewUbootPart(s.rootdir, opts)
	testBootloaderSetBootVarFwEnv(c, u)
}

func (s *ubootpartTestSuite) TestUbootPartEnvPath(c *C) {
	opts := &bootloader.Options{PrepareImageTime: true}
	bootloader.MockUbootPartFiles(c, s.rootdir, opts)
	u := bootloader.NewUbootPart(s.rootdir, opts)

	envPath, err := bootloader.UbootPartEnvDevice(u)
	c.Assert(err, IsNil)
	c.Assert(envPath, Equals, filepath.Join(s.rootdir, "/boot/uboot/ubuntu-boot-state.img"))
}

func (s *ubootpartTestSuite) TestUbootPartRedundantEnv(c *C) {
	opts := &bootloader.Options{PrepareImageTime: true}
	bootloader.MockUbootPartFiles(c, s.rootdir, opts)
	u := bootloader.NewUbootPart(s.rootdir, opts)

	// Set some boot vars
	err := u.SetBootVars(map[string]string{
		"snap_kernel":     "kernel_1.snap",
		"snap_try_kernel": "kernel_2.snap",
		"kernel_status":   "try",
	})
	c.Assert(err, IsNil)

	// Verify they're stored in a redundant environment
	envPath, err := bootloader.UbootPartEnvDevice(u)
	c.Assert(err, IsNil)

	env, err := ubootenv.OpenRedundant(envPath, ubootenv.DefaultRedundantEnvSize)
	c.Assert(err, IsNil)
	c.Assert(env.Get("snap_kernel"), Equals, "kernel_1.snap")
	c.Assert(env.Get("snap_try_kernel"), Equals, "kernel_2.snap")
	c.Assert(env.Get("kernel_status"), Equals, "try")
	c.Assert(env.Redundant(), Equals, true)
}

func (s *ubootpartTestSuite) TestUbootPartExtractKernelAssetsAndRemove(c *C) {
	opts := &bootloader.Options{PrepareImageTime: true}
	bootloader.MockUbootPartFiles(c, s.rootdir, opts)
	u := bootloader.NewUbootPart(s.rootdir, opts)
	kernelAssetsDir := filepath.Join(s.rootdir, "boot", "uboot", "ubuntu-kernel_42.snap")
	testBootloaderExtractKernelAssetsAndRemove(c, u, kernelAssetsDir)
}

func (s *ubootpartTestSuite) TestUbootPartExtractRecoveryKernelAssets(c *C) {
	opts := &bootloader.Options{PrepareImageTime: true}
	bootloader.MockUbootPartFiles(c, s.rootdir, opts)
	u := bootloader.NewUbootPart(s.rootdir, opts)
	kernelAssetsDir := filepath.Join(s.rootdir, "recovery-dir", "kernel")
	testBootloaderExtractRecoveryKernelAssets(c, u, kernelAssetsDir)
}

func (s *ubootpartTestSuite) TestUbootPartPresentAfterInstall(c *C) {
	// Present() at prepare-image time checks for the installed environment
	// file, not the .conf marker (which lives in gadgetDir, not rootdir).
	// Use separate directories to verify this.
	opts := &bootloader.Options{PrepareImageTime: true}
	u := bootloader.NewUbootPart(s.rootdir, opts)

	// Marker in a separate gadget dir — not in rootdir
	gadgetDir := c.MkDir()
	err := os.WriteFile(filepath.Join(gadgetDir, "ubootpart.conf"), nil, 0644)
	c.Assert(err, IsNil)

	// Before install, not present (no env file yet)
	present, err := u.Present()
	c.Assert(err, IsNil)
	c.Assert(present, Equals, false)

	// Install creates the env file in rootdir
	err = u.InstallBootConfig(gadgetDir, opts)
	c.Assert(err, IsNil)

	// Now present via the env file, even though the marker is not in rootdir
	present, err = u.Present()
	c.Assert(err, IsNil)
	c.Assert(present, Equals, true)
}

func (s *ubootpartTestSuite) TestUbootPartInstallBootConfig(c *C) {
	opts := &bootloader.Options{PrepareImageTime: true}
	u := bootloader.NewUbootPart(s.rootdir, opts)

	gadgetDir := c.MkDir()
	err := os.WriteFile(filepath.Join(gadgetDir, "ubootpart.conf"), nil, 0644)
	c.Assert(err, IsNil)

	err = u.InstallBootConfig(gadgetDir, opts)
	c.Assert(err, IsNil)

	// Verify the environment file was created
	envPath := filepath.Join(s.rootdir, "/boot/uboot/ubuntu-boot-state.img")
	c.Assert(osutil.FileExists(envPath), Equals, true)

	// Verify it's a valid redundant environment with the default size
	env, err := ubootenv.OpenRedundant(envPath, ubootenv.DefaultRedundantEnvSize)
	c.Assert(err, IsNil)
	c.Assert(env.Redundant(), Equals, true)
}

func (s *ubootpartTestSuite) TestUbootPartInstallBootConfigSizeFromGadget(c *C) {
	// The environment size is a U-Boot compile option, so InstallBootConfig
	// should honour the size from the gadget's reference boot.sel rather
	// than always using the default.
	opts := &bootloader.Options{PrepareImageTime: true}
	u := bootloader.NewUbootPart(s.rootdir, opts)

	gadgetDir := c.MkDir()
	err := os.WriteFile(filepath.Join(gadgetDir, "ubootpart.conf"), nil, 0644)
	c.Assert(err, IsNil)

	// Create a reference boot.sel in the gadget with a non-default size
	customSize := 16384
	ref, err := ubootenv.Create(filepath.Join(gadgetDir, "boot.sel"), customSize,
		ubootenv.CreateOptions{HeaderFlagByte: true})
	c.Assert(err, IsNil)
	err = ref.Save()
	c.Assert(err, IsNil)

	err = u.InstallBootConfig(gadgetDir, opts)
	c.Assert(err, IsNil)

	// Verify the installed environment uses the gadget-specified size
	envPath := filepath.Join(s.rootdir, "/boot/uboot/ubuntu-boot-state.img")
	env, err := ubootenv.OpenRedundant(envPath, customSize)
	c.Assert(err, IsNil)
	c.Assert(env.Redundant(), Equals, true)
	c.Assert(env.Size(), Equals, customSize)
}

func (s *ubootpartTestSuite) TestUbootPartSetBootVarsNoUselessWrites(c *C) {
	opts := &bootloader.Options{PrepareImageTime: true}
	bootloader.MockUbootPartFiles(c, s.rootdir, opts)
	u := bootloader.NewUbootPart(s.rootdir, opts)
	c.Assert(u, NotNil)

	envPath, err := bootloader.UbootPartEnvDevice(u)
	c.Assert(err, IsNil)

	// Set initial value
	err = u.SetBootVars(map[string]string{"snap_ab": "b"})
	c.Assert(err, IsNil)

	st, err := os.Stat(envPath)
	c.Assert(err, IsNil)

	// Set to the same value - should not modify the file
	err = u.SetBootVars(map[string]string{"snap_ab": "b"})
	c.Assert(err, IsNil)

	st2, err := os.Stat(envPath)
	c.Assert(err, IsNil)
	c.Assert(st.ModTime(), Equals, st2.ModTime())
}

func (s *ubootpartTestSuite) TestUbootPartRuntimePresent(c *C) {
	// At runtime (not prepare-image time), Present() checks for partition
	restore := efi.MockVars(nil, nil)
	defer restore()

	opts := &bootloader.Options{PrepareImageTime: false}
	u := bootloader.NewUbootPart(s.rootdir, opts)

	// No partition exists yet
	present, err := u.Present()
	c.Assert(err, IsNil)
	c.Assert(present, Equals, false)

	// Create the partition symlink
	bootloader.MockUbootPartFiles(c, s.rootdir, opts)

	present, err = u.Present()
	c.Assert(err, IsNil)
	c.Assert(present, Equals, true)
}

func (s *ubootpartTestSuite) TestUbootPartRuntimeEnvDevice(c *C) {
	// At runtime, envDevice() resolves the partition symlink
	restore := efi.MockVars(nil, nil)
	defer restore()

	opts := &bootloader.Options{PrepareImageTime: false}
	bootloader.MockUbootPartFiles(c, s.rootdir, opts)
	u := bootloader.NewUbootPart(s.rootdir, opts)

	envPath, err := bootloader.UbootPartEnvDevice(u)
	c.Assert(err, IsNil)
	// Should resolve to the mock device file
	c.Assert(envPath, Equals, filepath.Join(s.rootdir, "/dev/mock-bootstate"))
}

func (s *ubootpartTestSuite) TestUbootPartRuntimeEnvDeviceError(c *C) {
	// At runtime, envDevice() returns error if partition doesn't exist
	restore := efi.MockVars(nil, nil)
	defer restore()

	opts := &bootloader.Options{PrepareImageTime: false}
	u := bootloader.NewUbootPart(s.rootdir, opts)

	_, err := bootloader.UbootPartEnvDevice(u)
	c.Assert(err, ErrorMatches, "cannot resolve boot state partition:.*")
}

func (s *ubootpartTestSuite) TestUbootPartRuntimeWithKernelCmdline(c *C) {
	// At runtime with snapd_system_disk in kernel cmdline
	restore := efi.MockVars(nil, nil)
	defer restore()

	opts := &bootloader.Options{PrepareImageTime: false}

	// Create mock kernel cmdline
	cmdlineFile := filepath.Join(c.MkDir(), "cmdline")
	err := os.WriteFile(cmdlineFile, []byte("snapd_system_disk=mmcblk0"), 0644)
	c.Assert(err, IsNil)
	restore = kcmdline.MockProcCmdline(cmdlineFile)
	defer restore()

	// Mock the disk
	bootStateDisk := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				PartitionLabel: "ubuntu-boot-state",
				PartitionUUID:  "boot-state-partuuid",
			},
		},
		DiskHasPartitions: true,
		DevNum:            "bootstate-dev-num",
	}
	restoreDisk := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"mmcblk0": bootStateDisk,
	})
	defer restoreDisk()

	u := bootloader.NewUbootPart(s.rootdir, opts)

	envPath, err := bootloader.UbootPartEnvDevice(u)
	c.Assert(err, IsNil)
	c.Assert(envPath, Equals, filepath.Join(s.rootdir, "/dev/disk/by-partuuid/boot-state-partuuid"))
}

func (s *ubootpartTestSuite) TestUbootPartRuntimeGetSetEnvVar(c *C) {
	// Test get/set at runtime using the partition
	restore := efi.MockVars(nil, nil)
	defer restore()

	opts := &bootloader.Options{PrepareImageTime: false}
	bootloader.MockUbootPartFiles(c, s.rootdir, opts)
	u := bootloader.NewUbootPart(s.rootdir, opts)

	testBootloaderGetSetEnvVar(c, u)
}

func (s *ubootpartTestSuite) TestUbootPartSetBootVarsEnvNotExist(c *C) {
	// SetBootVars should fail when environment file doesn't exist
	opts := &bootloader.Options{PrepareImageTime: true}
	u := bootloader.NewUbootPart(s.rootdir, opts)

	// Don't create environment files - just try to set vars
	err := u.SetBootVars(map[string]string{"key": "value"})
	c.Assert(err, ErrorMatches, ".*no such file or directory")
}

func (s *ubootpartTestSuite) TestUbootPartGetBootVarsEnvNotExist(c *C) {
	// GetBootVars should fail when environment file doesn't exist
	opts := &bootloader.Options{PrepareImageTime: true}
	u := bootloader.NewUbootPart(s.rootdir, opts)

	// Don't create environment files - just try to get vars
	_, err := u.GetBootVars("key")
	c.Assert(err, ErrorMatches, ".*no such file or directory")
}

func (s *ubootpartTestSuite) TestUbootPartRuntimeSetBootVarsEnvDeviceError(c *C) {
	// At runtime, SetBootVars should fail if partition doesn't exist
	restore := efi.MockVars(nil, nil)
	defer restore()

	opts := &bootloader.Options{PrepareImageTime: false}
	u := bootloader.NewUbootPart(s.rootdir, opts)

	err := u.SetBootVars(map[string]string{"key": "value"})
	c.Assert(err, ErrorMatches, "cannot resolve boot state partition:.*")
}

func (s *ubootpartTestSuite) TestUbootPartRuntimeGetBootVarsEnvDeviceError(c *C) {
	// At runtime, GetBootVars should fail if partition doesn't exist
	restore := efi.MockVars(nil, nil)
	defer restore()

	opts := &bootloader.Options{PrepareImageTime: false}
	u := bootloader.NewUbootPart(s.rootdir, opts)

	_, err := u.GetBootVars("key")
	c.Assert(err, ErrorMatches, "cannot resolve boot state partition:.*")
}

func (s *ubootpartTestSuite) TestUbootPartRuntimeInstallBootConfigEnvDeviceError(c *C) {
	// At runtime, InstallBootConfig should fail if partition doesn't exist
	restore := efi.MockVars(nil, nil)
	defer restore()

	opts := &bootloader.Options{PrepareImageTime: false}
	u := bootloader.NewUbootPart(s.rootdir, opts)

	gadgetDir := c.MkDir()
	err := u.InstallBootConfig(gadgetDir, opts)
	c.Assert(err, ErrorMatches, "cannot resolve boot state partition:.*")
}

func (s *ubootpartTestSuite) TestUbootPartRuntimeCmdlineError(c *C) {
	// At runtime, kernel cmdline read error should propagate
	restore := efi.MockVars(nil, nil)
	defer restore()

	opts := &bootloader.Options{PrepareImageTime: false}
	u := bootloader.NewUbootPart(s.rootdir, opts)

	// Mock a non-existent cmdline file to trigger error
	restore = kcmdline.MockProcCmdline("/nonexistent/cmdline")
	defer restore()

	_, err := bootloader.UbootPartEnvDevice(u)
	c.Assert(err, ErrorMatches, ".*no such file or directory")
}

func (s *ubootpartTestSuite) TestUbootPartRuntimeWithDevicePath(c *C) {
	// At runtime with snapd_system_disk as a device path (fallback)
	restore := efi.MockVars(nil, nil)
	defer restore()

	opts := &bootloader.Options{PrepareImageTime: false}

	// Create mock kernel cmdline with a device path
	cmdlineFile := filepath.Join(c.MkDir(), "cmdline")
	err := os.WriteFile(cmdlineFile, []byte("snapd_system_disk=/dev/disk/by-id/mmc-FOO"), 0644)
	c.Assert(err, IsNil)
	restore = kcmdline.MockProcCmdline(cmdlineFile)
	defer restore()

	// Mock the disk - device name lookup fails, device path succeeds
	bootStateDisk := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				PartitionLabel: "ubuntu-boot-state",
				PartitionUUID:  "boot-state-path-partuuid",
			},
		},
		DiskHasPartitions: true,
		DevNum:            "bootstate-path-dev-num",
	}
	restoreDisk := disks.MockDevicePathToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/disk/by-id/mmc-FOO": bootStateDisk,
	})
	defer restoreDisk()

	u := bootloader.NewUbootPart(s.rootdir, opts)

	envPath, err := bootloader.UbootPartEnvDevice(u)
	c.Assert(err, IsNil)
	c.Assert(envPath, Equals, filepath.Join(s.rootdir, "/dev/disk/by-partuuid/boot-state-path-partuuid"))
}

func (s *ubootpartTestSuite) TestUbootPartRuntimeWithEFI(c *C) {
	// At runtime with EFI LoaderDevicePartUUID available
	opts := &bootloader.Options{PrepareImageTime: false}

	// Mock the EFI variable with a partition UUID (UTF-16 encoded, upper case)
	const loaderDevicePartUUID = "LoaderDevicePartUUID-4a67b082-0a4c-41cf-b6c7-440b29bb8c4f"
	restoreEFI := efi.MockVars(map[string][]byte{
		loaderDevicePartUUID: bootloadertest.UTF16Bytes("AAAA-BBBB-CCCC"),
	}, nil)
	defer restoreEFI()

	// Create a by-partuuid symlink that resolves to a device node
	byPartUUID := filepath.Join(s.rootdir, "/dev/disk/by-partuuid")
	err := os.MkdirAll(byPartUUID, 0755)
	c.Assert(err, IsNil)
	mockPartDev := filepath.Join(s.rootdir, "/dev/mmcblk0p3")
	err = os.WriteFile(mockPartDev, nil, 0644)
	c.Assert(err, IsNil)
	err = os.Symlink(mockPartDev, filepath.Join(byPartUUID, "aaaa-bbbb-cccc"))
	c.Assert(err, IsNil)

	// Mock DiskFromPartitionDeviceNode to return a disk with boot-state
	bootStateDisk := &disks.MockDiskMapping{
		Structure: []disks.Partition{
			{
				PartitionLabel: "ubuntu-boot-state",
				PartitionUUID:  "boot-state-efi-partuuid",
			},
		},
		DiskHasPartitions: true,
		DevNum:            "bootstate-efi-dev-num",
	}
	restoreDisk := disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		mockPartDev: bootStateDisk,
	})
	defer restoreDisk()

	u := bootloader.NewUbootPart(s.rootdir, opts)

	envPath, err := bootloader.UbootPartEnvDevice(u)
	c.Assert(err, IsNil)
	c.Assert(envPath, Equals, filepath.Join(s.rootdir, "/dev/disk/by-partuuid/boot-state-efi-partuuid"))
}
