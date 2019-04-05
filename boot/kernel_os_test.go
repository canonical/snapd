// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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
	"io/ioutil"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

func TestBoot(t *testing.T) { TestingT(t) }

type kernelOSSuite struct {
	testutil.BaseTest
	mocLoader *boottest.MockBootloader
	loader    bootloader.Bootloader
}

var _ = Suite(&kernelOSSuite{})

func (s *kernelOSSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
	dirs.SetRootDir(c.MkDir())
}

func (s *kernelOSSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("")
	bootloader.Force(nil)
}

func (s *kernelOSSuite) forceMockBootloader(c *C) {
	s.mocLoader = boottest.NewMockBootloader("mock", c.MkDir())
	bootloader.Force(s.mocLoader)
}

func (s *kernelOSSuite) forceGrubBootloader(c *C) {
	mockGadgetDir := c.MkDir()
	err := ioutil.WriteFile(filepath.Join(mockGadgetDir, "grub.conf"), nil, 0644)
	c.Assert(err, IsNil)
	err = bootloader.InstallBootConfig(mockGadgetDir)
	c.Assert(err, IsNil)
	s.loader, err = bootloader.Find()
	c.Assert(err, IsNil)
	c.Check(s.loader, NotNil)
	s.loader.SetBootVars(map[string]string{
		"snap_kernel": "kernel_41.snap",
		"snap_core":   "core_21.snap",
	})
	bootloader.Force(s.loader)
	fn := filepath.Join(dirs.GlobalRootDir, "/boot/grub/grub.cfg")
	c.Assert(osutil.FileExists(fn), Equals, true)
}

func (s *kernelOSSuite) forceUbootBootloader(c *C) {
	mockGadgetDir := c.MkDir()
	err := ioutil.WriteFile(filepath.Join(mockGadgetDir, "uboot.conf"), nil, 0644)
	c.Assert(err, IsNil)
	err = bootloader.InstallBootConfig(mockGadgetDir)
	c.Assert(err, IsNil)
	s.loader, err = bootloader.Find()
	c.Assert(err, IsNil)
	c.Check(s.loader, NotNil)
	bootloader.Force(s.loader)
	fn := filepath.Join(dirs.GlobalRootDir, "/boot/uboot/uboot.env")
	c.Assert(osutil.FileExists(fn), Equals, true)
}

const packageKernel = `
name: ubuntu-kernel
version: 4.0-1
type: kernel
vendor: Someone
`

func (s *kernelOSSuite) TestExtractKernelAssetsAndRemove(c *C) {
	s.forceUbootBootloader(c)
	l, err := bootloader.Find()
	c.Assert(err, IsNil)
	c.Assert(l, NotNil)

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

	err = boot.ExtractKernelAssets(info, snapf)
	c.Assert(err, IsNil)

	// this is where the kernel/initrd is unpacked
	bootdir := l.Dir()

	kernelAssetsDir := filepath.Join(bootdir, "ubuntu-kernel_42.snap")

	for _, def := range files {
		if def[0] == "meta/kernel.yaml" {
			break
		}

		fullFn := filepath.Join(kernelAssetsDir, def[0])
		c.Check(fullFn, testutil.FileEquals, def[1])
	}

	// remove
	err = boot.RemoveKernelAssets(info)
	c.Assert(err, IsNil)

	c.Check(osutil.FileExists(kernelAssetsDir), Equals, false)
}

func (s *kernelOSSuite) TestExtractKernelAssetsNoUnpacksKernelForGrub(c *C) {
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

	err = boot.ExtractKernelAssets(info, snapf)
	c.Assert(err, IsNil)

	// kernel is *not* here
	kernimg := filepath.Join(s.loader.Dir(), "ubuntu-kernel_42.snap", "kernel.img")
	c.Assert(osutil.FileExists(kernimg), Equals, false)
}

func (s *kernelOSSuite) TestExtractKernelForceWorks(c *C) {
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

	err = boot.ExtractKernelAssets(info, snapf)
	c.Assert(err, IsNil)

	// kernel is extracted
	kernimg := filepath.Join(s.loader.Dir(), "ubuntu-kernel_42.snap", "kernel.img")
	c.Assert(osutil.FileExists(kernimg), Equals, true)
	// initrd
	initrdimg := filepath.Join(s.loader.Dir(), "ubuntu-kernel_42.snap", "initrd.img")
	c.Assert(osutil.FileExists(initrdimg), Equals, true)

	// ensure that removal of assets also works
	err = boot.RemoveKernelAssets(info)
	c.Assert(err, IsNil)
	exists, _, err := osutil.DirExists(filepath.Dir(kernimg))
	c.Assert(err, IsNil)
	c.Check(exists, Equals, false)
}

func (s *kernelOSSuite) TestExtractKernelAssetsError(c *C) {
	s.forceGrubBootloader(c)
	info := &snap.Info{}
	info.Type = snap.TypeApp

	err := boot.ExtractKernelAssets(info, nil)
	c.Assert(err, ErrorMatches, `cannot extract kernel assets from snap type "app"`)
}

// SetNextBoot should do nothing on classic LP: #1580403
func (s *kernelOSSuite) TestSetNextBootOnClassic(c *C) {
	s.forceMockBootloader(c)
	restore := release.MockOnClassic(true)
	defer restore()

	// Create a fake OS snap that we try to update
	snapInfo := snaptest.MockSnap(c, "name: os\ntype: os", &snap.SideInfo{Revision: snap.R(42)})
	err := boot.SetNextBoot(snapInfo)
	c.Assert(err, ErrorMatches, "cannot set next boot on classic systems")

	c.Assert(s.mocLoader.BootVars, HasLen, 0)
}

func (s *kernelOSSuite) TestSetNextBootForCore(c *C) {
	s.forceGrubBootloader(c)
	restore := release.MockOnClassic(false)
	defer restore()

	info := &snap.Info{}
	info.Type = snap.TypeOS
	info.RealName = "core"
	info.Revision = snap.R(100)

	err := boot.SetNextBoot(info)
	c.Assert(err, IsNil)

	v, err := s.loader.GetBootVars("snap_try_core", "snap_mode")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_try_core": "core_100.snap",
		"snap_mode":     "try",
	})

	c.Check(boot.ChangeRequiresReboot(info), Equals, true)
}

func (s *kernelOSSuite) TestSetNextBootWithBaseForCore(c *C) {
	s.forceGrubBootloader(c)
	restore := release.MockOnClassic(false)
	defer restore()

	info := &snap.Info{}
	info.Type = snap.TypeBase
	info.RealName = "core18"
	info.Revision = snap.R(1818)

	err := boot.SetNextBoot(info)
	c.Assert(err, IsNil)

	v, err := s.loader.GetBootVars("snap_try_core", "snap_mode")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_try_core": "core18_1818.snap",
		"snap_mode":     "try",
	})

	c.Check(boot.ChangeRequiresReboot(info), Equals, true)
}

func (s *kernelOSSuite) TestSetNextBootForKernel(c *C) {
	s.forceGrubBootloader(c)
	restore := release.MockOnClassic(false)
	defer restore()

	info := &snap.Info{}
	info.Type = snap.TypeKernel
	info.RealName = "krnl"
	info.Revision = snap.R(42)

	err := boot.SetNextBoot(info)
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
	c.Check(boot.ChangeRequiresReboot(info), Equals, true)

	// simulate good boot
	bootVars = map[string]string{"snap_kernel": "krnl_42.snap"}
	s.loader.SetBootVars(bootVars)
	c.Check(boot.ChangeRequiresReboot(info), Equals, false)
}

func (s *kernelOSSuite) TestSetNextBootForKernelForTheSameKernel(c *C) {
	s.forceGrubBootloader(c)
	restore := release.MockOnClassic(false)
	defer restore()

	info := &snap.Info{}
	info.Type = snap.TypeKernel
	info.RealName = "krnl"
	info.Revision = snap.R(40)

	bootVars := map[string]string{"snap_kernel": "krnl_40.snap"}
	s.loader.SetBootVars(bootVars)

	err := boot.SetNextBoot(info)
	c.Assert(err, IsNil)

	v, err := s.loader.GetBootVars("snap_kernel")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_kernel": "krnl_40.snap",
	})
}

func (s *kernelOSSuite) TestSetNextBootForKernelForTheSameKernelTryMode(c *C) {
	s.forceGrubBootloader(c)
	restore := release.MockOnClassic(false)
	defer restore()

	info := &snap.Info{}
	info.Type = snap.TypeKernel
	info.RealName = "krnl"
	info.Revision = snap.R(40)

	bootVars := map[string]string{
		"snap_kernel":     "krnl_40.snap",
		"snap_try_kernel": "krnl_99.snap",
		"snap_mode":       "try"}
	s.loader.SetBootVars(bootVars)

	err := boot.SetNextBoot(info)
	c.Assert(err, IsNil)

	v, err := s.loader.GetBootVars("snap_kernel", "snap_try_kernel", "snap_mode")
	c.Assert(err, IsNil)
	c.Assert(v, DeepEquals, map[string]string{
		"snap_kernel":     "krnl_40.snap",
		"snap_try_kernel": "",
		"snap_mode":       "",
	})
}

func (s *kernelOSSuite) TestInUse(c *C) {
	s.forceMockBootloader(c)
	for _, t := range []struct {
		bootVarKey   string
		bootVarValue string

		snapName string
		snapRev  snap.Revision

		inUse bool
	}{
		// in use
		{"snap_kernel", "kernel_41.snap", "kernel", snap.R(41), true},
		{"snap_try_kernel", "kernel_82.snap", "kernel", snap.R(82), true},
		{"snap_core", "core_21.snap", "core", snap.R(21), true},
		{"snap_try_core", "core_42.snap", "core", snap.R(42), true},
		// not in use
		{"snap_core", "core_111.snap", "core", snap.R(21), false},
		{"snap_try_core", "core_111.snap", "core", snap.R(21), false},
		{"snap_kernel", "kernel_111.snap", "kernel", snap.R(1), false},
		{"snap_try_kernel", "kernel_111.snap", "kernel", snap.R(1), false},
	} {
		s.mocLoader.BootVars[t.bootVarKey] = t.bootVarValue
		c.Assert(boot.InUse(t.snapName, t.snapRev), Equals, t.inUse, Commentf("unexpected result: %s %s %v", t.snapName, t.snapRev, t.inUse))
	}
}
