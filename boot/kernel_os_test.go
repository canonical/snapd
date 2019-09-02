// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

const packageKernel = `
name: ubuntu-kernel
version: 4.0-1
type: kernel
vendor: Someone
`

// coreBootSetSuite tests the abstract bootloader behaviour including
// bootenv setting, error handling etc., for a core BootSet.
type coreBootSetSuite struct {
	baseBootSetSuite

	loader *bootloadertest.MockBootloader
}

var _ = Suite(&coreBootSetSuite{})

func (s *coreBootSetSuite) SetUpTest(c *C) {
	s.baseBootSetSuite.SetUpTest(c)

	s.loader = bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(s.loader)
	s.AddCleanup(func() { bootloader.Force(nil) })
}

func (s *coreBootSetSuite) TestExtractKernelAssetsError(c *C) {
	bootloader.ForceError(errors.New("brkn"))
	err := boot.NewCoreKernel(&snap.Info{}).ExtractKernelAssets(nil)
	c.Check(err, ErrorMatches, `cannot extract kernel assets: brkn`)
}

func (s *coreBootSetSuite) TestRemoveKernelAssetsError(c *C) {
	bootloader.ForceError(errors.New("brkn"))
	err := boot.NewCoreKernel(&snap.Info{}).RemoveKernelAssets()
	c.Check(err, ErrorMatches, `cannot remove kernel assets: brkn`)
}

func (s *coreBootSetSuite) TestChangeRequiresRebootError(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()
	bp := boot.NewCoreBootParticipant(&snap.Info{}, snap.TypeBase)

	s.loader.GetErr = errors.New("zap")

	c.Check(bp.ChangeRequiresReboot(), Equals, false)
	c.Check(logbuf.String(), testutil.Contains, `cannot get boot variables: zap`)
	s.loader.GetErr = nil
	logbuf.Reset()

	bootloader.ForceError(errors.New("brkn"))
	c.Check(bp.ChangeRequiresReboot(), Equals, false)
	c.Check(logbuf.String(), testutil.Contains, `cannot get boot settings: brkn`)
}

func (s *coreBootSetSuite) TestSetNextBootError(c *C) {
	s.loader.GetErr = errors.New("zap")
	err := boot.NewCoreBootParticipant(&snap.Info{}, snap.TypeApp).SetNextBoot()
	c.Check(err, ErrorMatches, `cannot set next boot: zap`)

	bootloader.ForceError(errors.New("brkn"))
	err = boot.NewCoreBootParticipant(&snap.Info{}, snap.TypeApp).SetNextBoot()
	c.Check(err, ErrorMatches, `cannot set next boot: brkn`)
}

func (s *coreBootSetSuite) TestSetNextBootForCore(c *C) {
	info := &snap.Info{}
	info.SnapType = snap.TypeOS
	info.RealName = "core"
	info.Revision = snap.R(100)

	bs := boot.NewCoreBootParticipant(info, info.GetType())
	err := bs.SetNextBoot()
	c.Assert(err, IsNil)

	v, err := s.loader.GetBootVars("snap_try_core", "snap_mode")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_try_core": "core_100.snap",
		"snap_mode":     "try",
	})

	c.Check(bs.ChangeRequiresReboot(), Equals, true)
}

func (s *coreBootSetSuite) TestSetNextBootWithBaseForCore(c *C) {
	info := &snap.Info{}
	info.SnapType = snap.TypeBase
	info.RealName = "core18"
	info.Revision = snap.R(1818)

	bs := boot.NewCoreBootParticipant(info, info.GetType())
	err := bs.SetNextBoot()
	c.Assert(err, IsNil)

	v, err := s.loader.GetBootVars("snap_try_core", "snap_mode")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_try_core": "core18_1818.snap",
		"snap_mode":     "try",
	})

	c.Check(bs.ChangeRequiresReboot(), Equals, true)
}

func (s *coreBootSetSuite) TestSetNextBootForKernel(c *C) {
	info := &snap.Info{}
	info.SnapType = snap.TypeKernel
	info.RealName = "krnl"
	info.Revision = snap.R(42)

	bp := boot.NewCoreKernel(info)
	err := bp.SetNextBoot()
	c.Assert(err, IsNil)

	v, err := s.loader.GetBootVars("snap_try_kernel", "snap_mode")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_try_kernel": "krnl_42.snap",
		"snap_mode":       "try",
	})

	bootVars := map[string]string{
		"snap_kernel":     "krnl_40.snap",
		"snap_try_kernel": "krnl_42.snap"}
	s.loader.SetBootVars(bootVars)
	c.Check(bp.ChangeRequiresReboot(), Equals, true)

	// simulate good boot
	bootVars = map[string]string{"snap_kernel": "krnl_42.snap"}
	s.loader.SetBootVars(bootVars)
	c.Check(bp.ChangeRequiresReboot(), Equals, false)
}

func (s *coreBootSetSuite) TestSetNextBootForKernelForTheSameKernel(c *C) {
	info := &snap.Info{}
	info.SnapType = snap.TypeKernel
	info.RealName = "krnl"
	info.Revision = snap.R(40)

	bootVars := map[string]string{"snap_kernel": "krnl_40.snap"}
	s.loader.SetBootVars(bootVars)

	err := boot.NewCoreKernel(info).SetNextBoot()
	c.Assert(err, IsNil)

	v, err := s.loader.GetBootVars("snap_kernel")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_kernel": "krnl_40.snap",
	})
}

func (s *coreBootSetSuite) TestSetNextBootForKernelForTheSameKernelTryMode(c *C) {
	info := &snap.Info{}
	info.SnapType = snap.TypeKernel
	info.RealName = "krnl"
	info.Revision = snap.R(40)

	bootVars := map[string]string{
		"snap_kernel":     "krnl_40.snap",
		"snap_try_kernel": "krnl_99.snap",
		"snap_mode":       "try"}
	s.loader.SetBootVars(bootVars)

	err := boot.NewCoreKernel(info).SetNextBoot()
	c.Assert(err, IsNil)

	v, err := s.loader.GetBootVars("snap_kernel", "snap_try_kernel", "snap_mode")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_kernel":     "krnl_40.snap",
		"snap_try_kernel": "",
		"snap_mode":       "",
	})
}

// ubootBootSetSuite tests the uboot specific code in the bootloader handling
type ubootBootSetSuite struct {
	baseBootSetSuite
}

var _ = Suite(&ubootBootSetSuite{})

func (s *ubootBootSetSuite) forceUbootBootloader(c *C) bootloader.Bootloader {
	mockGadgetDir := c.MkDir()
	err := ioutil.WriteFile(filepath.Join(mockGadgetDir, "uboot.conf"), nil, 0644)
	c.Assert(err, IsNil)
	err = bootloader.InstallBootConfig(mockGadgetDir)
	c.Assert(err, IsNil)

	loader, err := bootloader.Find()
	c.Assert(err, IsNil)
	c.Check(loader, NotNil)
	bootloader.Force(loader)
	s.AddCleanup(func() { bootloader.Force(nil) })

	fn := filepath.Join(s.bootdir, "/uboot/uboot.env")
	c.Assert(osutil.FileExists(fn), Equals, true)
	return loader
}

func (s *ubootBootSetSuite) TestExtractKernelAssetsAndRemoveOnUboot(c *C) {
	loader := s.forceUbootBootloader(c)
	c.Assert(loader, NotNil)

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"dtbs/foo.dtb", "g'day, I'm foo.dtb"},
		{"dtbs/bar.dtb", "hello, I'm bar.dtb"},
		// must be last
		{"meta/kernel.yaml", "version: 4.2"},
	}

	si := &snap.SideInfo{
		RealName: "ubuntu-kernel",
		Revision: snap.R(42),
	}
	fn := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)
	snapf, err := snap.Open(fn)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, si)
	c.Assert(err, IsNil)

	bp := boot.NewCoreKernel(info)
	err = bp.ExtractKernelAssets(snapf)
	c.Assert(err, IsNil)

	// this is where the kernel/initrd is unpacked
	kernelAssetsDir := filepath.Join(s.bootdir, "/uboot/ubuntu-kernel_42.snap")
	for _, def := range files {
		if def[0] == "meta/kernel.yaml" {
			break
		}

		fullFn := filepath.Join(kernelAssetsDir, def[0])
		c.Check(fullFn, testutil.FileEquals, def[1])
	}

	// it's idempotent
	err = bp.ExtractKernelAssets(snapf)
	c.Assert(err, IsNil)

	// remove
	err = bp.RemoveKernelAssets()
	c.Assert(err, IsNil)
	c.Check(osutil.FileExists(kernelAssetsDir), Equals, false)

	// it's idempotent
	err = bp.RemoveKernelAssets()
	c.Assert(err, IsNil)
}

// grubBootSetSuite tests the GRUB specific code in the bootloader handling
type grubBootSetSuite struct {
	baseBootSetSuite
}

var _ = Suite(&grubBootSetSuite{})

func (s *grubBootSetSuite) forceGrubBootloader(c *C) bootloader.Bootloader {
	// make mock grub bootenv dir
	mockGadgetDir := c.MkDir()
	err := ioutil.WriteFile(filepath.Join(mockGadgetDir, "grub.conf"), nil, 0644)
	c.Assert(err, IsNil)
	err = bootloader.InstallBootConfig(mockGadgetDir)
	c.Assert(err, IsNil)

	loader, err := bootloader.Find()
	c.Assert(err, IsNil)
	c.Check(loader, NotNil)
	loader.SetBootVars(map[string]string{
		"snap_kernel": "kernel_41.snap",
		"snap_core":   "core_21.snap",
	})
	bootloader.Force(loader)
	s.AddCleanup(func() { bootloader.Force(nil) })

	fn := filepath.Join(s.bootdir, "/grub/grub.cfg")
	c.Assert(osutil.FileExists(fn), Equals, true)
	return loader
}

func (s *grubBootSetSuite) TestExtractKernelAssetsNoUnpacksKernelForGrub(c *C) {
	s.forceGrubBootloader(c)

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"meta/kernel.yaml", "version: 4.2"},
	}
	si := &snap.SideInfo{
		RealName: "ubuntu-kernel",
		Revision: snap.R(42),
	}
	fn := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)
	snapf, err := snap.Open(fn)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, si)
	c.Assert(err, IsNil)

	bp := boot.NewCoreKernel(info)
	err = bp.ExtractKernelAssets(snapf)
	c.Assert(err, IsNil)

	// kernel is *not* here
	kernimg := filepath.Join(s.bootdir, "grub", "ubuntu-kernel_42.snap", "kernel.img")
	c.Assert(osutil.FileExists(kernimg), Equals, false)

	// it's idempotent
	err = bp.ExtractKernelAssets(snapf)
	c.Assert(err, IsNil)
}

func (s *grubBootSetSuite) TestExtractKernelForceWorks(c *C) {
	s.forceGrubBootloader(c)

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"meta/force-kernel-extraction", ""},
		{"meta/kernel.yaml", "version: 4.2"},
	}
	si := &snap.SideInfo{
		RealName: "ubuntu-kernel",
		Revision: snap.R(42),
	}
	fn := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)
	snapf, err := snap.Open(fn)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, si)
	c.Assert(err, IsNil)

	bp := boot.NewCoreKernel(info)
	err = bp.ExtractKernelAssets(snapf)
	c.Assert(err, IsNil)

	// kernel is extracted
	kernimg := filepath.Join(s.bootdir, "/grub/ubuntu-kernel_42.snap/kernel.img")
	c.Assert(osutil.FileExists(kernimg), Equals, true)
	// initrd
	initrdimg := filepath.Join(s.bootdir, "/grub/ubuntu-kernel_42.snap/initrd.img")
	c.Assert(osutil.FileExists(initrdimg), Equals, true)

	// it's idempotent
	err = bp.ExtractKernelAssets(snapf)
	c.Assert(err, IsNil)

	// ensure that removal of assets also works
	err = bp.RemoveKernelAssets()
	c.Assert(err, IsNil)
	exists, _, err := osutil.DirExists(filepath.Dir(kernimg))
	c.Assert(err, IsNil)
	c.Check(exists, Equals, false)

	// it's idempotent
	err = bp.RemoveKernelAssets()
	c.Assert(err, IsNil)
}
