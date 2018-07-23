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
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

func TestBoot(t *testing.T) { TestingT(t) }

type kernelOSSuite struct {
	testutil.BaseTest
	bootloader *boottest.MockBootloader
}

var _ = Suite(&kernelOSSuite{})

func (s *kernelOSSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
	dirs.SetRootDir(c.MkDir())
	s.bootloader = boottest.NewMockBootloader("mock", c.MkDir())
	partition.ForceBootloader(s.bootloader)
}

func (s *kernelOSSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("")
	partition.ForceBootloader(nil)
}

const packageKernel = `
name: ubuntu-kernel
version: 4.0-1
type: kernel
vendor: Someone
`

func (s *kernelOSSuite) TestExtractKernelAssetsAndRemove(c *C) {
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
	bootdir := s.bootloader.Dir()

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
	// pretend to be a grub system
	mockGrub := boottest.NewMockBootloader("grub", c.MkDir())
	partition.ForceBootloader(mockGrub)

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
	kernimg := filepath.Join(mockGrub.Dir(), "ubuntu-kernel_42.snap", "kernel.img")
	c.Assert(osutil.FileExists(kernimg), Equals, false)
}

func (s *kernelOSSuite) TestExtractKernelAssetsError(c *C) {
	info := &snap.Info{}
	info.Type = snap.TypeApp

	err := boot.ExtractKernelAssets(info, nil)
	c.Assert(err, ErrorMatches, `cannot extract kernel assets from snap type "app"`)
}

// SetNextBoot should do nothing on classic LP: #1580403
func (s *kernelOSSuite) TestSetNextBootOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	// Create a fake OS snap that we try to update
	snapInfo := snaptest.MockSnap(c, "name: os\ntype: os", &snap.SideInfo{Revision: snap.R(42)})
	err := boot.SetNextBoot(snapInfo)
	c.Assert(err, ErrorMatches, "cannot set next boot on classic systems")

	c.Assert(s.bootloader.BootVars, HasLen, 0)
}

func (s *kernelOSSuite) TestSetNextBootForCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	info := &snap.Info{}
	info.Type = snap.TypeOS
	info.RealName = "core"
	info.Revision = snap.R(100)

	err := boot.SetNextBoot(info)
	c.Assert(err, IsNil)

	c.Assert(s.bootloader.BootVars, DeepEquals, map[string]string{
		"snap_try_core": "core_100.snap",
		"snap_mode":     "try",
	})

	c.Check(boot.ChangeRequiresReboot(info), Equals, true)
}

func (s *kernelOSSuite) TestSetNextBootWithBaseForCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	info := &snap.Info{}
	info.Type = snap.TypeBase
	info.RealName = "core18"
	info.Revision = snap.R(1818)

	err := boot.SetNextBoot(info)
	c.Assert(err, IsNil)

	c.Assert(s.bootloader.BootVars, DeepEquals, map[string]string{
		"snap_try_core": "core18_1818.snap",
		"snap_mode":     "try",
	})

	c.Check(boot.ChangeRequiresReboot(info), Equals, true)
}

func (s *kernelOSSuite) TestSetNextBootForKernel(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	info := &snap.Info{}
	info.Type = snap.TypeKernel
	info.RealName = "krnl"
	info.Revision = snap.R(42)

	err := boot.SetNextBoot(info)
	c.Assert(err, IsNil)

	c.Assert(s.bootloader.BootVars, DeepEquals, map[string]string{
		"snap_try_kernel": "krnl_42.snap",
		"snap_mode":       "try",
	})

	s.bootloader.BootVars["snap_kernel"] = "krnl_40.snap"
	s.bootloader.BootVars["snap_try_kernel"] = "krnl_42.snap"
	c.Check(boot.ChangeRequiresReboot(info), Equals, true)

	// simulate good boot
	s.bootloader.BootVars["snap_kernel"] = "krnl_42.snap"
	c.Check(boot.ChangeRequiresReboot(info), Equals, false)
}

func (s *kernelOSSuite) TestSetNextBootForKernelForTheSameKernel(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	info := &snap.Info{}
	info.Type = snap.TypeKernel
	info.RealName = "krnl"
	info.Revision = snap.R(40)

	s.bootloader.BootVars["snap_kernel"] = "krnl_40.snap"

	err := boot.SetNextBoot(info)
	c.Assert(err, IsNil)

	c.Assert(s.bootloader.BootVars, DeepEquals, map[string]string{
		"snap_kernel": "krnl_40.snap",
	})
}

func (s *kernelOSSuite) TestSetNextBootForKernelForTheSameKernelTryMode(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	info := &snap.Info{}
	info.Type = snap.TypeKernel
	info.RealName = "krnl"
	info.Revision = snap.R(40)

	s.bootloader.BootVars["snap_kernel"] = "krnl_40.snap"
	s.bootloader.BootVars["snap_try_kernel"] = "krnl_99.snap"
	s.bootloader.BootVars["snap_mode"] = "try"

	err := boot.SetNextBoot(info)
	c.Assert(err, IsNil)

	c.Assert(s.bootloader.BootVars, DeepEquals, map[string]string{
		"snap_kernel":     "krnl_40.snap",
		"snap_try_kernel": "",
		"snap_mode":       "",
	})
}

func (s *kernelOSSuite) TestInUse(c *C) {
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
		s.bootloader.BootVars[t.bootVarKey] = t.bootVarValue
		c.Assert(boot.InUse(t.snapName, t.snapRev), Equals, t.inUse, Commentf("unexpected result: %s %s %v", t.snapName, t.snapRev, t.inUse))
	}
}
