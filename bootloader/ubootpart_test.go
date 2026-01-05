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
	"github.com/snapcore/snapd/bootloader/ubootenv"
	"github.com/snapcore/snapd/osutil"
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
	c.Assert(u.Name(), Equals, "uboot")

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

func (s *ubootpartTestSuite) TestUbootPartInstallBootConfig(c *C) {
	opts := &bootloader.Options{PrepareImageTime: true}
	u := bootloader.NewUbootPart(s.rootdir, opts)

	gadgetDir := c.MkDir()
	confFile, err := os.Create(filepath.Join(gadgetDir, "uboot.conf"))
	c.Assert(err, IsNil)
	err = confFile.Close()
	c.Assert(err, IsNil)

	err = u.InstallBootConfig(gadgetDir, opts)
	c.Assert(err, IsNil)

	// Verify the environment file was created
	envPath := filepath.Join(s.rootdir, "/boot/uboot/ubuntu-boot-state.img")
	c.Assert(osutil.FileExists(envPath), Equals, true)

	// Verify it's a valid redundant environment
	env, err := ubootenv.OpenRedundant(envPath, ubootenv.DefaultRedundantEnvSize)
	c.Assert(err, IsNil)
	c.Assert(env.Redundant(), Equals, true)
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
	opts := &bootloader.Options{PrepareImageTime: false}
	u := bootloader.NewUbootPart(s.rootdir, opts)

	_, err := bootloader.UbootPartEnvDevice(u)
	c.Assert(err, ErrorMatches, "cannot resolve boot state partition:.*")
}

func (s *ubootpartTestSuite) TestUbootPartRuntimeWithKernelCmdline(c *C) {
	// At runtime with snapd_ubootpart_disk in kernel cmdline
	opts := &bootloader.Options{PrepareImageTime: false}

	// Create mock kernel cmdline
	cmdlineFile := filepath.Join(c.MkDir(), "cmdline")
	err := os.WriteFile(cmdlineFile, []byte("snapd_ubootpart_disk=mmcblk0"), 0644)
	c.Assert(err, IsNil)
	restore := kcmdline.MockProcCmdline(cmdlineFile)
	defer restore()

	// Create the partition (needed for the disk path)
	bootloader.MockUbootPartFiles(c, s.rootdir, opts)
	u := bootloader.NewUbootPart(s.rootdir, opts)

	// envDevice should return the partition path (disk-specific lookup not yet implemented)
	envPath, err := bootloader.UbootPartEnvDevice(u)
	c.Assert(err, IsNil)
	c.Assert(envPath, Equals, filepath.Join(s.rootdir, "/dev/disk/by-partlabel/ubuntu-boot-state"))
}

func (s *ubootpartTestSuite) TestUbootPartRuntimeGetSetEnvVar(c *C) {
	// Test get/set at runtime using the partition
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
	opts := &bootloader.Options{PrepareImageTime: false}
	u := bootloader.NewUbootPart(s.rootdir, opts)

	err := u.SetBootVars(map[string]string{"key": "value"})
	c.Assert(err, ErrorMatches, "cannot resolve boot state partition:.*")
}

func (s *ubootpartTestSuite) TestUbootPartRuntimeGetBootVarsEnvDeviceError(c *C) {
	// At runtime, GetBootVars should fail if partition doesn't exist
	opts := &bootloader.Options{PrepareImageTime: false}
	u := bootloader.NewUbootPart(s.rootdir, opts)

	_, err := u.GetBootVars("key")
	c.Assert(err, ErrorMatches, "cannot resolve boot state partition:.*")
}

func (s *ubootpartTestSuite) TestUbootPartRuntimeInstallBootConfigEnvDeviceError(c *C) {
	// At runtime, InstallBootConfig should fail if partition doesn't exist
	opts := &bootloader.Options{PrepareImageTime: false}
	u := bootloader.NewUbootPart(s.rootdir, opts)

	gadgetDir := c.MkDir()
	err := u.InstallBootConfig(gadgetDir, opts)
	c.Assert(err, ErrorMatches, "cannot resolve boot state partition:.*")
}

func (s *ubootpartTestSuite) TestUbootPartRuntimeGetAllowedDiskError(c *C) {
	// At runtime, getAllowedDisk error should propagate
	opts := &bootloader.Options{PrepareImageTime: false}
	u := bootloader.NewUbootPart(s.rootdir, opts)

	// Mock a non-existent cmdline file to trigger error
	restore := kcmdline.MockProcCmdline("/nonexistent/cmdline")
	defer restore()

	_, err := bootloader.UbootPartEnvDevice(u)
	c.Assert(err, ErrorMatches, ".*no such file or directory")
}
