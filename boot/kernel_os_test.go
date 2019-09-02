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

	bl *bootloadertest.MockBootloader
}

var _ = Suite(&coreBootSetSuite{})

func (s *coreBootSetSuite) SetUpTest(c *C) {
	s.baseBootSetSuite.SetUpTest(c)

	s.bl = bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(s.bl)
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

	s.bl.GetErr = errors.New("zap")

	c.Check(bp.ChangeRequiresReboot(), Equals, false)
	c.Check(logbuf.String(), testutil.Contains, `cannot get boot variables: zap`)
	s.bl.GetErr = nil
	logbuf.Reset()

	bootloader.ForceError(errors.New("brkn"))
	c.Check(bp.ChangeRequiresReboot(), Equals, false)
	c.Check(logbuf.String(), testutil.Contains, `cannot get boot settings: brkn`)
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

	bl, err := bootloader.Find()
	c.Assert(err, IsNil)
	c.Check(bl, NotNil)
	bootloader.Force(bl)
	s.AddCleanup(func() { bootloader.Force(nil) })

	fn := filepath.Join(s.bootdir, "/uboot/uboot.env")
	c.Assert(osutil.FileExists(fn), Equals, true)
	return bl
}

func (s *ubootBootSetSuite) TestExtractKernelAssetsAndRemoveOnUboot(c *C) {
	bl := s.forceUbootBootloader(c)
	c.Assert(bl, NotNil)

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

	bl, err := bootloader.Find()
	c.Assert(err, IsNil)
	c.Check(bl, NotNil)
	bl.SetBootVars(map[string]string{
		"snap_kernel": "kernel_41.snap",
		"snap_core":   "core_21.snap",
	})
	bootloader.Force(bl)
	s.AddCleanup(func() { bootloader.Force(nil) })

	fn := filepath.Join(s.bootdir, "/grub/grub.cfg")
	c.Assert(osutil.FileExists(fn), Equals, true)
	return bl
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
