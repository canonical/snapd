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
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/mvo5/goconfigparser"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type grubTestSuite struct {
	baseBootenvTestSuite

	bootdir string
}

var _ = Suite(&grubTestSuite{})

func (s *grubTestSuite) SetUpTest(c *C) {
	s.baseBootenvTestSuite.SetUpTest(c)
	bootloader.MockGrubFiles(c, s.rootdir)

	s.bootdir = filepath.Join(s.rootdir, "boot")
}

// grubEditenvCmd finds the right grub{,2}-editenv command
func grubEditenvCmd() string {
	for _, exe := range []string{"grub2-editenv", "grub-editenv"} {
		if osutil.ExecutableExists(exe) {
			return exe
		}
	}
	return ""
}

func grubEnvPath(rootdir string) string {
	return filepath.Join(rootdir, "boot/grub/grubenv")
}

func (s *grubTestSuite) grubEditenvSet(c *C, key, value string) {
	if grubEditenvCmd() == "" {
		c.Skip("grub{,2}-editenv is not available")
	}

	err := exec.Command(grubEditenvCmd(), grubEnvPath(s.rootdir), "set", fmt.Sprintf("%s=%s", key, value)).Run()
	c.Assert(err, IsNil)
}

func (s *grubTestSuite) grubEditenvGet(c *C, key string) string {
	if grubEditenvCmd() == "" {
		c.Skip("grub{,2}-editenv is not available")
	}

	output, err := exec.Command(grubEditenvCmd(), grubEnvPath(s.rootdir), "list").CombinedOutput()
	c.Assert(err, IsNil)
	cfg := goconfigparser.New()
	cfg.AllowNoSectionHeader = true
	err = cfg.ReadString(string(output))
	c.Assert(err, IsNil)
	v, err := cfg.Get("", key)
	c.Assert(err, IsNil)
	return v
}

func (s *grubTestSuite) makeFakeGrubEnv(c *C) {
	s.grubEditenvSet(c, "k", "v")
}

func (s *grubTestSuite) TestNewGrubNoGrubReturnsNil(c *C) {
	g := bootloader.NewGrub("/something/not/there")
	c.Assert(g, IsNil)
}

func (s *grubTestSuite) TestNewGrub(c *C) {
	s.makeFakeGrubEnv(c)

	g := bootloader.NewGrub(s.rootdir)
	c.Assert(g, NotNil)
	c.Assert(g.Name(), Equals, "grub")
}

func (s *grubTestSuite) TestGetBootloaderWithGrub(c *C) {
	s.makeFakeGrubEnv(c)

	bootloader, err := bootloader.Find(s.rootdir, nil)
	c.Assert(err, IsNil)
	c.Assert(bootloader.Name(), Equals, "grub")
}

func (s *grubTestSuite) TestGetBootloaderWithGrubWithDefaultRoot(c *C) {
	s.makeFakeGrubEnv(c)

	dirs.SetRootDir(s.rootdir)
	defer func() { dirs.SetRootDir("") }()

	bootloader, err := bootloader.Find("", nil)
	c.Assert(err, IsNil)
	c.Assert(bootloader.Name(), Equals, "grub")
}

func (s *grubTestSuite) TestGetBootVer(c *C) {
	s.makeFakeGrubEnv(c)
	s.grubEditenvSet(c, "snap_mode", "regular")

	g := bootloader.NewGrub(s.rootdir)
	v, err := g.GetBootVars("snap_mode")
	c.Assert(err, IsNil)
	c.Check(v, HasLen, 1)
	c.Check(v["snap_mode"], Equals, "regular")
}

func (s *grubTestSuite) TestSetBootVer(c *C) {
	s.makeFakeGrubEnv(c)

	g := bootloader.NewGrub(s.rootdir)
	err := g.SetBootVars(map[string]string{
		"k1": "v1",
		"k2": "v2",
	})
	c.Assert(err, IsNil)

	c.Check(s.grubEditenvGet(c, "k1"), Equals, "v1")
	c.Check(s.grubEditenvGet(c, "k2"), Equals, "v2")
}

func (s *grubTestSuite) TestExtractKernelAssetsNoUnpacksKernelForGrub(c *C) {
	s.makeFakeGrubEnv(c)

	g := bootloader.NewGrub(s.rootdir)

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

	err = g.ExtractKernelAssets(info, snapf)
	c.Assert(err, IsNil)

	// kernel is *not* here
	kernimg := filepath.Join(s.bootdir, "grub", "ubuntu-kernel_42.snap", "kernel.img")
	c.Assert(osutil.FileExists(kernimg), Equals, false)
}

func (s *grubTestSuite) TestExtractKernelForceWorks(c *C) {
	s.makeFakeGrubEnv(c)

	g := bootloader.NewGrub(s.rootdir)
	c.Assert(g, NotNil)

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

	err = g.ExtractKernelAssets(info, snapf)
	c.Assert(err, IsNil)

	// kernel is extracted
	kernimg := filepath.Join(s.bootdir, "grub", "ubuntu-kernel_42.snap", "kernel.img")
	c.Assert(osutil.FileExists(kernimg), Equals, true)
	// initrd
	initrdimg := filepath.Join(s.bootdir, "grub", "ubuntu-kernel_42.snap", "initrd.img")
	c.Assert(osutil.FileExists(initrdimg), Equals, true)

	// ensure that removal of assets also works
	err = g.RemoveKernelAssets(info)
	c.Assert(err, IsNil)
	exists, _, err := osutil.DirExists(filepath.Dir(kernimg))
	c.Assert(err, IsNil)
	c.Check(exists, Equals, false)
}
