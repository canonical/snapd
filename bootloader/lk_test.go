// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
