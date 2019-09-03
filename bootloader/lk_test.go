// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/lkenv"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type lkTestSuite struct {
	testutil.BaseTest
}

var _ = Suite(&lkTestSuite{})

func (g *lkTestSuite) SetUpTest(c *C) {
	g.BaseTest.SetUpTest(c)
	g.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
	dirs.SetRootDir(c.MkDir())
}

func (g *lkTestSuite) TearDownTest(c *C) {
	g.BaseTest.TearDownTest(c)
	dirs.SetRootDir("")
}

func (s *lkTestSuite) TestNewLkNolkReturnsNil(c *C) {
	l := bootloader.NewLk()
	c.Assert(l, IsNil)
}

func (s *lkTestSuite) TestNewLk(c *C) {
	bootloader.MockLkFiles(c)
	l := bootloader.NewLk()
	c.Assert(l, NotNil)
}

func (s *lkTestSuite) TestSetGetBootVar(c *C) {
	bootloader.MockLkFiles(c)
	l := bootloader.NewLk()
	bootVars := map[string]string{"snap_mode": "try"}
	l.SetBootVars(bootVars)

	v, err := l.GetBootVars("snap_mode")
	c.Assert(err, IsNil)
	c.Check(v, HasLen, 1)
	c.Check(v["snap_mode"], Equals, "try")
}

func (s *lkTestSuite) TestExtractKernelAssetsUnpacksBootimg(c *C) {
	bootloader.MockLkFiles(c)
	l := bootloader.NewLk()

	c.Assert(l, NotNil)

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"boot.img", "...and I'm an boot image"},
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

	err = l.ExtractKernelAssets(info, snapf)
	c.Assert(err, IsNil)

	// kernel is *not* here
	bootimg := filepath.Join(dirs.GlobalRootDir, "boot", "lk", "boot.img")
	c.Assert(osutil.FileExists(bootimg), Equals, true)
}

func (s *lkTestSuite) TestExtractKernelAssetsUnpacksCustomBootimg(c *C) {
	bootloader.MockLkFiles(c)
	l := bootloader.NewLk()

	c.Assert(l, NotNil)

	// first configure custom boot image file name
	env := lkenv.NewEnv(l.ConfigFile())
	env.Load()
	env.ConfigureBootimgName("boot-2.img")
	err := env.Save()
	c.Assert(err, IsNil)

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"boot-2.img", "...and I'm an boot image"},
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

	err = l.ExtractKernelAssets(info, snapf)
	c.Assert(err, IsNil)

	// kernel is *not* here
	bootimg := filepath.Join(dirs.GlobalRootDir, "boot", "lk", "boot-2.img")
	c.Assert(osutil.FileExists(bootimg), Equals, true)
}

func (s *lkTestSuite) TestExtractKernelAssetsUnpacksInRuntimeMode(c *C) {
	bootloader.MockLkFiles(c)
	lk := bootloader.NewLk()
	c.Assert(lk, NotNil)
	bootloader.MockLkRuntimeMode(lk, true)

	// create mock bootsel, boot_a, boot_b partitions
	for _, partName := range []string{"snapbootsel", "boot_a", "boot_b"} {
		mockPart := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/", partName)
		err := os.MkdirAll(filepath.Dir(mockPart), 0755)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(mockPart, nil, 0600)
		c.Assert(err, IsNil)
	}
	// ensure we have a valid boot env
	bootselPartition := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/snapbootsel")
	lkenv := lkenv.NewEnv(bootselPartition)
	lkenv.ConfigureBootPartitions("boot_a", "boot_b")
	err := lkenv.Save()
	c.Assert(err, IsNil)

	// mock a kernel snap that has a boot.img
	files := [][]string{
		{"boot.img", "I'm the default boot image name"},
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

	// now extract
	err = lk.ExtractKernelAssets(info, snapf)
	c.Assert(err, IsNil)

	// and validate it went to the "boot_a" partition
	bootA := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/boot_a")
	content, err := ioutil.ReadFile(bootA)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "I'm the default boot image name")

	// also validate that bootB is empty
	bootB := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/boot_b")
	content, err = ioutil.ReadFile(bootB)
	c.Assert(err, IsNil)
	c.Assert(content, HasLen, 0)
}
