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
	"sort"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/lkenv"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type lkTestSuite struct {
	baseBootenvTestSuite
}

var _ = Suite(&lkTestSuite{})

func (s *lkTestSuite) TestNewLkNolkReturnsNil(c *C) {
	l := bootloader.NewLk("/does/not/exist", nil)
	c.Assert(l, IsNil)
}

func (s *lkTestSuite) TestNewLk(c *C) {
	bootloader.MockLkFiles(c, s.rootdir, nil)
	l := bootloader.NewLk(s.rootdir, nil)
	c.Assert(l, NotNil)
	c.Check(bootloader.LkRuntimeMode(l), Equals, true)
	c.Check(l.ConfigFile(), Equals, filepath.Join(s.rootdir, "/dev/disk/by-partlabel", "snapbootsel"))
}

func (s *lkTestSuite) TestNewLkImageBuildingTime(c *C) {
	opts := &bootloader.Options{
		PrepareImageTime: true,
	}
	bootloader.MockLkFiles(c, s.rootdir, opts)
	l := bootloader.NewLk(s.rootdir, opts)
	c.Assert(l, NotNil)
	c.Check(bootloader.LkRuntimeMode(l), Equals, false)
	c.Check(l.ConfigFile(), Equals, filepath.Join(s.rootdir, "/boot/lk", "snapbootsel.bin"))
}

func (s *lkTestSuite) TestSetGetBootVar(c *C) {
	bootloader.MockLkFiles(c, s.rootdir, nil)
	l := bootloader.NewLk(s.rootdir, nil)
	bootVars := map[string]string{"snap_mode": "try"}
	l.SetBootVars(bootVars)

	v, err := l.GetBootVars("snap_mode")
	c.Assert(err, IsNil)
	c.Check(v, HasLen, 1)
	c.Check(v["snap_mode"], Equals, "try")
}

func (s *lkTestSuite) TestExtractKernelAssetsUnpacksBootimgImageBuilding(c *C) {
	opts := &bootloader.Options{
		PrepareImageTime: true,
	}
	bootloader.MockLkFiles(c, s.rootdir, opts)
	l := bootloader.NewLk(s.rootdir, opts)

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

	// just boot.img and snapbootsel.bin are there, no kernel.img
	infos, err := ioutil.ReadDir(filepath.Join(s.rootdir, "boot", "lk", ""))
	c.Assert(err, IsNil)
	var fnames []string
	for _, info := range infos {
		fnames = append(fnames, info.Name())
	}
	sort.Strings(fnames)
	c.Assert(fnames, HasLen, 2)
	c.Assert(fnames, DeepEquals, []string{"boot.img", "snapbootsel.bin"})
}

func (s *lkTestSuite) TestExtractKernelAssetsUnpacksCustomBootimgImageBuilding(c *C) {
	opts := &bootloader.Options{
		PrepareImageTime: true,
	}
	bootloader.MockLkFiles(c, s.rootdir, opts)
	l := bootloader.NewLk(s.rootdir, opts)

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

	// boot-2.img is there
	bootimg := filepath.Join(s.rootdir, "boot", "lk", "boot-2.img")
	c.Assert(osutil.FileExists(bootimg), Equals, true)
}

func (s *lkTestSuite) TestExtractKernelAssetsUnpacksAndRemoveInRuntimeMode(c *C) {
	bootloader.MockLkFiles(c, s.rootdir, nil)
	lk := bootloader.NewLk(s.rootdir, nil)
	c.Assert(lk, NotNil)

	// create mock bootsel, boot_a, boot_b partitions
	for _, partName := range []string{"snapbootsel", "boot_a", "boot_b"} {
		mockPart := filepath.Join(s.rootdir, "/dev/disk/by-partlabel/", partName)
		err := os.MkdirAll(filepath.Dir(mockPart), 0755)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(mockPart, nil, 0600)
		c.Assert(err, IsNil)
	}
	// ensure we have a valid boot env
	bootselPartition := filepath.Join(s.rootdir, "/dev/disk/by-partlabel/snapbootsel")
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
	bootA := filepath.Join(s.rootdir, "/dev/disk/by-partlabel/boot_a")
	content, err := ioutil.ReadFile(bootA)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "I'm the default boot image name")

	// also validate that bootB is empty
	bootB := filepath.Join(s.rootdir, "/dev/disk/by-partlabel/boot_b")
	content, err = ioutil.ReadFile(bootB)
	c.Assert(err, IsNil)
	c.Assert(content, HasLen, 0)

	// test that boot partition got set
	err = lkenv.Load()
	c.Assert(err, IsNil)
	bootPart, err := lkenv.GetBootPartition("ubuntu-kernel_42.snap")
	c.Assert(err, IsNil)
	c.Assert(bootPart, Equals, "boot_a")

	// now remove the kernel
	err = lk.RemoveKernelAssets(info)
	c.Assert(err, IsNil)
	// and ensure its no longer available in the boot partions
	err = lkenv.Load()
	c.Assert(err, IsNil)
	bootPart, err = lkenv.GetBootPartition("ubuntu-kernel_42.snap")
	c.Assert(err, ErrorMatches, "cannot find kernel .* in boot image partitions")
	c.Assert(bootPart, Equals, "")
}
