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

const packageKernel = `
name: ubuntu-kernel
version: 4.0-1
type: kernel
vendor: Someone
`

// baseKernelOSSuite is used to setup the common test environment
type baseKernelOSSuite struct {
	testutil.BaseTest

	bootdir string
}

func (s *baseKernelOSSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
	restore := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	s.AddCleanup(restore)
	restore = release.MockOnClassic(false)
	s.AddCleanup(restore)

	s.bootdir = filepath.Join(dirs.GlobalRootDir, "boot")
}

// kernelOSSuite tests the abstract bootloader behaviour including
// bootenv setting, error handling etc
type kernelOSSuite struct {
	baseKernelOSSuite

	loader *boottest.MockBootloader
}

var _ = Suite(&kernelOSSuite{})

func (s *kernelOSSuite) SetUpTest(c *C) {
	s.baseKernelOSSuite.SetUpTest(c)

	s.loader = boottest.NewMockBootloader("mock", c.MkDir())
	bootloader.Force(s.loader)
	s.AddCleanup(func() { bootloader.Force(nil) })
}

func (s *kernelOSSuite) TestExtractKernelAssetsError(c *C) {
	info := &snap.Info{}
	info.SnapType = snap.TypeApp

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

	c.Assert(s.loader.BootVars, HasLen, 0)
}

func (s *kernelOSSuite) TestSetNextBootForCore(c *C) {
	info := &snap.Info{}
	info.SnapType = snap.TypeOS
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
	info := &snap.Info{}
	info.SnapType = snap.TypeBase
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
	info := &snap.Info{}
	info.SnapType = snap.TypeKernel
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
	info := &snap.Info{}
	info.SnapType = snap.TypeKernel
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
	info := &snap.Info{}
	info.SnapType = snap.TypeKernel
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
		s.loader.BootVars[t.bootVarKey] = t.bootVarValue
		c.Assert(boot.InUse(t.snapName, t.snapRev), Equals, t.inUse, Commentf("unexpected result: %s %s %v", t.snapName, t.snapRev, t.inUse))
	}
}

func (s *kernelOSSuite) TestNameAndRevnoFromSnapValid(c *C) {
	info, err := boot.NameAndRevnoFromSnap("foo_2.snap")
	c.Assert(err, IsNil)
	c.Assert(info.Name, Equals, "foo")
	c.Assert(info.Revision, Equals, snap.R(2))
}

func (s *kernelOSSuite) TestNameAndRevnoFromSnapInvalidFormat(c *C) {
	_, err := boot.NameAndRevnoFromSnap("invalid")
	c.Assert(err, ErrorMatches, `input "invalid" has invalid format \(not enough '_'\)`)
}

func BenchmarkNameAndRevno(b *testing.B) {
	for n := 0; n < b.N; n++ {
		for _, sn := range []string{
			"core_21.snap",
			"kernel_41.snap",
			"some-long-kernel-name-kernel_82.snap",
			"what-is-this-core_111.snap",
		} {
			boot.NameAndRevnoFromSnap(sn)
		}
	}
}

func (s *kernelOSSuite) TestCurrentBootNameAndRevision(c *C) {
	s.loader.BootVars["snap_core"] = "core_2.snap"
	s.loader.BootVars["snap_kernel"] = "canonical-pc-linux_2.snap"

	current, err := boot.GetCurrentBoot(snap.TypeOS)
	c.Check(err, IsNil)
	c.Check(current.Name, Equals, "core")
	c.Check(current.Revision, Equals, snap.R(2))

	current, err = boot.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, IsNil)
	c.Check(current.Name, Equals, "canonical-pc-linux")
	c.Check(current.Revision, Equals, snap.R(2))

	s.loader.BootVars["snap_mode"] = "trying"
	_, err = boot.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, Equals, boot.ErrBootNameAndRevisionAgain)
}

func (s *kernelOSSuite) TestCurrentBootNameAndRevisionUnhappy(c *C) {
	_, err := boot.GetCurrentBoot(snap.TypeKernel)
	c.Check(err, ErrorMatches, "cannot get name and revision of boot kernel: unset")

	_, err = boot.GetCurrentBoot(snap.TypeOS)
	c.Check(err, ErrorMatches, "cannot get name and revision of boot snap: unset")

	_, err = boot.GetCurrentBoot(snap.TypeBase)
	c.Check(err, ErrorMatches, "cannot get name and revision of boot snap: unset")

	_, err = boot.GetCurrentBoot(snap.TypeApp)
	c.Check(err, ErrorMatches, "internal error: cannot find boot revision for snap type \"app\"")
}

// ubootKernelOSSuite tests the uboot specific code in the bootloader handling
type ubootKernelOSSuite struct {
	baseKernelOSSuite
}

var _ = Suite(&ubootKernelOSSuite{})

func (s *ubootKernelOSSuite) forceUbootBootloader(c *C) bootloader.Bootloader {
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

func (s *ubootKernelOSSuite) TestExtractKernelAssetsAndRemoveOnUboot(c *C) {
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

	err = boot.ExtractKernelAssets(info, snapf)
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
	err = boot.ExtractKernelAssets(info, snapf)
	c.Assert(err, IsNil)

	// remove
	err = boot.RemoveKernelAssets(info)
	c.Assert(err, IsNil)
	c.Check(osutil.FileExists(kernelAssetsDir), Equals, false)

	// it's idempotent
	err = boot.RemoveKernelAssets(info)
	c.Assert(err, IsNil)
}

// grubKernelOSSuite tests the GRUB specific code in the bootloader handling
type grubKernelOSSuite struct {
	baseKernelOSSuite
}

var _ = Suite(&grubKernelOSSuite{})

func (s *grubKernelOSSuite) forceGrubBootloader(c *C) bootloader.Bootloader {
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

func (s *grubKernelOSSuite) TestExtractKernelAssetsNoUnpacksKernelForGrub(c *C) {
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
	kernimg := filepath.Join(s.bootdir, "grub", "ubuntu-kernel_42.snap", "kernel.img")
	c.Assert(osutil.FileExists(kernimg), Equals, false)

	// it's idempotent
	err = boot.ExtractKernelAssets(info, snapf)
	c.Assert(err, IsNil)
}

func (s *grubKernelOSSuite) TestExtractKernelForceWorks(c *C) {
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
	kernimg := filepath.Join(s.bootdir, "/grub/ubuntu-kernel_42.snap/kernel.img")
	c.Assert(osutil.FileExists(kernimg), Equals, true)
	// initrd
	initrdimg := filepath.Join(s.bootdir, "/grub/ubuntu-kernel_42.snap/initrd.img")
	c.Assert(osutil.FileExists(initrdimg), Equals, true)

	// it's idempotent
	err = boot.ExtractKernelAssets(info, snapf)
	c.Assert(err, IsNil)

	// ensure that removal of assets also works
	err = boot.RemoveKernelAssets(info)
	c.Assert(err, IsNil)
	exists, _, err := osutil.DirExists(filepath.Dir(kernimg))
	c.Assert(err, IsNil)
	c.Check(exists, Equals, false)

	// it's idempotent
	err = boot.RemoveKernelAssets(info)
	c.Assert(err, IsNil)
}
