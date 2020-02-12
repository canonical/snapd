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
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
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

	bootloader *bootloadertest.MockBootloader
}

var _ = Suite(&coreBootSetSuite{})

func (s *coreBootSetSuite) SetUpTest(c *C) {
	s.baseBootSetSuite.SetUpTest(c)

	s.bootloader = bootloadertest.Mock("mock", c.MkDir())
	s.forceBootloader(s.bootloader)
}

func (s *coreBootSetSuite) TestExtractKernelAssetsError(c *C) {
	bootloader.ForceError(errors.New("brkn"))
	err := boot.NewCoreKernel(&snap.Info{}, boottest.MockDevice("")).ExtractKernelAssets(nil)
	c.Check(err, ErrorMatches, `cannot extract kernel assets: brkn`)
}

func (s *coreBootSetSuite) TestRemoveKernelAssetsError(c *C) {
	bootloader.ForceError(errors.New("brkn"))
	err := boot.NewCoreKernel(&snap.Info{}, boottest.MockDevice("")).RemoveKernelAssets()
	c.Check(err, ErrorMatches, `cannot remove kernel assets: brkn`)
}

func (s *coreBootSetSuite) TestSetNextBootError(c *C) {
	coreDev := boottest.MockDevice("some-snap")

	s.bootloader.GetErr = errors.New("zap")
	_, err := boot.NewCoreBootParticipant(&snap.Info{}, snap.TypeKernel, coreDev).SetNextBoot()
	c.Check(err, ErrorMatches, `cannot set next boot: zap`)

	bootloader.ForceError(errors.New("brkn"))
	_, err = boot.NewCoreBootParticipant(&snap.Info{}, snap.TypeKernel, coreDev).SetNextBoot()
	c.Check(err, ErrorMatches, `cannot set next boot: brkn`)
}

func (s *coreBootSetSuite) TestSetNextBootForCore(c *C) {
	coreDev := boottest.MockDevice("core")

	info := &snap.Info{}
	info.SnapType = snap.TypeOS
	info.RealName = "core"
	info.Revision = snap.R(100)

	bs := boot.NewCoreBootParticipant(info, info.GetType(), coreDev)
	reboot, err := bs.SetNextBoot()
	c.Assert(err, IsNil)

	v, err := s.bootloader.GetBootVars("snap_try_core", "snap_mode")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_try_core": "core_100.snap",
		"snap_mode":     boot.TryStatus,
	})

	c.Check(reboot, Equals, true)
}

func (s *coreBootSetSuite) TestSetNextBootWithBaseForCore(c *C) {
	coreDev := boottest.MockDevice("core18")

	info := &snap.Info{}
	info.SnapType = snap.TypeBase
	info.RealName = "core18"
	info.Revision = snap.R(1818)

	bs := boot.NewCoreBootParticipant(info, info.GetType(), coreDev)
	reboot, err := bs.SetNextBoot()
	c.Assert(err, IsNil)

	v, err := s.bootloader.GetBootVars("snap_try_core", "snap_mode")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_try_core": "core18_1818.snap",
		"snap_mode":     boot.TryStatus,
	})

	c.Check(reboot, Equals, true)
}

func (s *coreBootSetSuite) TestSetNextBootForKernel(c *C) {
	coreDev := boottest.MockDevice("krnl")

	info := &snap.Info{}
	info.SnapType = snap.TypeKernel
	info.RealName = "krnl"
	info.Revision = snap.R(42)

	bp := boot.NewCoreBootParticipant(info, snap.TypeKernel, coreDev)
	reboot, err := bp.SetNextBoot()
	c.Assert(err, IsNil)

	v, err := s.bootloader.GetBootVars("snap_try_kernel", "snap_mode")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_try_kernel": "krnl_42.snap",
		"snap_mode":       boot.TryStatus,
	})

	bootVars := map[string]string{
		"snap_kernel":     "krnl_40.snap",
		"snap_try_kernel": "krnl_42.snap"}
	s.bootloader.SetBootVars(bootVars)
	c.Check(reboot, Equals, true)

	// simulate good boot
	bootVars = map[string]string{"snap_kernel": "krnl_42.snap"}
	s.bootloader.SetBootVars(bootVars)

	reboot, err = bp.SetNextBoot()
	c.Assert(err, IsNil)
	c.Check(reboot, Equals, false)
}

func (s *coreBootSetSuite) TestSetNextBoot20ForKernel(c *C) {
	coreDev := boottest.MockUC20Device("pc-kernel")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// setup current kernel
	kernel1, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r := s.bootloader.SetRunKernelImageEnabledKernel(kernel1)
	defer r()

	// create new kernel rev, set that as the next boot
	kernel2, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_2.snap")
	c.Assert(err, IsNil)

	bs := boot.NewCoreBootParticipant(kernel2, snap.TypeKernel, coreDev)
	c.Assert(bs.IsTrivial(), Equals, false)
	reboot, err := bs.SetNextBoot()
	c.Assert(err, IsNil)

	// check that kernel_status is now try
	v, err := s.bootloader.GetBootVars("kernel_status")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"kernel_status": boot.TryStatus,
	})

	c.Check(reboot, Equals, true)

	// check that SetNextBoot enabled kernel2 as a TryKernel
	actual, _ := s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableTryKernel")
	c.Assert(actual, DeepEquals, []snap.PlaceInfo{kernel2})

	// also didn't move any try kernels to trusted kernels
	actual, _ = s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableKernel")
	c.Assert(actual, DeepEquals, []snap.PlaceInfo(nil))

	// check that SetNextBoot asked the bootloader for a kernel
	_, nKernelCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("Kernel")
	c.Assert(nKernelCalls, Equals, 1)
}

func (s *coreBootSetSuite) TestSetNextBootForKernelForTheSameKernel(c *C) {
	coreDev := boottest.MockDevice("krnl")

	info := &snap.Info{}
	info.SnapType = snap.TypeKernel
	info.RealName = "krnl"
	info.Revision = snap.R(40)

	bootVars := map[string]string{"snap_kernel": "krnl_40.snap"}
	s.bootloader.SetBootVars(bootVars)

	reboot, err := boot.NewCoreBootParticipant(info, snap.TypeKernel, coreDev).SetNextBoot()
	c.Assert(err, IsNil)

	v, err := s.bootloader.GetBootVars("snap_kernel")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_kernel": "krnl_40.snap",
	})

	c.Check(reboot, Equals, false)
}

func (s *coreBootSetSuite) TestSetNextBoot20ForKernelForTheSameKernel(c *C) {
	coreDev := boottest.MockUC20Device("pc-kernel")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// setup current kernel
	kernel1, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r := s.bootloader.SetRunKernelImageEnabledKernel(kernel1)
	defer r()

	bs := boot.NewCoreBootParticipant(kernel1, snap.TypeKernel, coreDev)
	c.Assert(bs.IsTrivial(), Equals, false)
	reboot, err := bs.SetNextBoot()
	c.Assert(err, IsNil)

	// check that kernel_status is cleared
	v, err := s.bootloader.GetBootVars("kernel_status")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"kernel_status": boot.DefaultStatus,
	})

	c.Check(reboot, Equals, false)

	// check that SetNextBoot didn't try to enable any try kernels
	actual, _ := s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableTryKernel")
	c.Assert(actual, HasLen, 0)

	// also didn't move any try kernels to trusted kernels
	actual, _ = s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableKernel")
	c.Assert(actual, HasLen, 0)

	// check that SetNextBoot asked the bootloader for a kernel
	_, nKernelCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("Kernel")
	c.Assert(nKernelCalls, Equals, 1)
}

func (s *coreBootSetSuite) TestSetNextBootForKernelForTheSameKernelTryMode(c *C) {
	coreDev := boottest.MockDevice("krnl")

	info := &snap.Info{}
	info.SnapType = snap.TypeKernel
	info.RealName = "krnl"
	info.Revision = snap.R(40)

	bootVars := map[string]string{
		"snap_kernel":     "krnl_40.snap",
		"snap_try_kernel": "krnl_99.snap",
		"snap_mode":       boot.TryStatus}
	s.bootloader.SetBootVars(bootVars)

	reboot, err := boot.NewCoreBootParticipant(info, snap.TypeKernel, coreDev).SetNextBoot()
	c.Assert(err, IsNil)

	v, err := s.bootloader.GetBootVars("snap_kernel", "snap_try_kernel", "snap_mode")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_kernel":     "krnl_40.snap",
		"snap_try_kernel": "",
		"snap_mode":       boot.DefaultStatus,
	})

	c.Check(reboot, Equals, false)
}

func (s *coreBootSetSuite) TestSetNextBoot20ForKernelForTheSameKernelTryMode(c *C) {
	coreDev := boottest.MockUC20Device("pc-kernel")
	c.Assert(coreDev.HasModeenv(), Equals, true)

	// setup current kernel
	kernel1, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r := s.bootloader.SetRunKernelImageEnabledKernel(kernel1)
	defer r()

	bootVars := map[string]string{
		"kernel_status": boot.TryStatus,
	}
	s.bootloader.SetBootVars(bootVars)

	bs := boot.NewCoreBootParticipant(kernel1, snap.TypeKernel, coreDev)
	c.Assert(bs.IsTrivial(), Equals, false)
	reboot, err := bs.SetNextBoot()
	c.Assert(err, IsNil)

	// check that kernel_status is cleared
	v, err := s.bootloader.GetBootVars("kernel_status")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"kernel_status": boot.DefaultStatus,
	})

	c.Check(reboot, Equals, false)

	// check that SetNextBoot didn't try to enable any try kernels
	actual, _ := s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableTryKernel")
	c.Assert(actual, HasLen, 0)

	// also didn't move any try kernels to trusted kernels
	actual, _ = s.bootloader.GetRunKernelImageFunctionSnapCalls("EnableKernel")
	c.Assert(actual, HasLen, 0)

	// check that SetNextBoot asked the bootloader for a kernel
	_, nKernelCalls := s.bootloader.GetRunKernelImageFunctionSnapCalls("Kernel")
	c.Assert(nKernelCalls, Equals, 1)
}

// ubootBootSetSuite tests the uboot specific code in the bootloader handling
type ubootBootSetSuite struct {
	baseBootSetSuite
}

var _ = Suite(&ubootBootSetSuite{})

func (s *ubootBootSetSuite) SetUpTest(c *C) {
	s.baseBootSetSuite.SetUpTest(c)
	s.forceUbootBootloader(c)
}

func (s *ubootBootSetSuite) forceUbootBootloader(c *C) {
	bootloader.Force(nil)

	mockGadgetDir := c.MkDir()
	err := ioutil.WriteFile(filepath.Join(mockGadgetDir, "uboot.conf"), nil, 0644)
	c.Assert(err, IsNil)
	err = bootloader.InstallBootConfig(mockGadgetDir, dirs.GlobalRootDir, nil)
	c.Assert(err, IsNil)

	bloader, err := bootloader.Find("", nil)
	c.Assert(err, IsNil)
	c.Check(bloader, NotNil)
	s.forceBootloader(bloader)

	fn := filepath.Join(s.bootdir, "/uboot/uboot.env")
	c.Assert(osutil.FileExists(fn), Equals, true)
}

func (s *ubootBootSetSuite) TestExtractKernelAssetsAndRemoveOnUboot(c *C) {
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

	bp := boot.NewCoreKernel(info, boottest.MockDevice(""))
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

func (s *grubBootSetSuite) SetUpTest(c *C) {
	s.baseBootSetSuite.SetUpTest(c)
	s.forceGrubBootloader(c)
}

func (s *grubBootSetSuite) forceGrubBootloader(c *C) bootloader.Bootloader {
	bootloader.Force(nil)

	// make mock grub bootenv dir
	mockGadgetDir := c.MkDir()
	err := ioutil.WriteFile(filepath.Join(mockGadgetDir, "grub.conf"), nil, 0644)
	c.Assert(err, IsNil)
	err = bootloader.InstallBootConfig(mockGadgetDir, dirs.GlobalRootDir, nil)
	c.Assert(err, IsNil)

	bloader, err := bootloader.Find("", nil)
	c.Assert(err, IsNil)
	c.Check(bloader, NotNil)
	bloader.SetBootVars(map[string]string{
		"snap_kernel": "kernel_41.snap",
		"snap_core":   "core_21.snap",
	})
	s.forceBootloader(bloader)

	fn := filepath.Join(s.bootdir, "/grub/grub.cfg")
	c.Assert(osutil.FileExists(fn), Equals, true)
	return bloader
}

func (s *grubBootSetSuite) TestExtractKernelAssetsNoUnpacksKernelForGrub(c *C) {
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

	bp := boot.NewCoreKernel(info, boottest.MockDevice(""))
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

	bp := boot.NewCoreKernel(info, boottest.MockDevice(""))
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
