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
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mvo5/goconfigparser"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/grubenv"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
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

	output, err := exec.Command(grubEditenvCmd(), grubEnvPath(s.rootdir), "set", fmt.Sprintf("%s=%s", key, value)).CombinedOutput()
	c.Check(err, IsNil)
	c.Check(string(output), Equals, "")
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
	g := bootloader.NewGrub("/something/not/there", nil)
	c.Assert(g, IsNil)
}

func (s *grubTestSuite) TestNewGrub(c *C) {
	s.makeFakeGrubEnv(c)

	g := bootloader.NewGrub(s.rootdir, nil)
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

	g := bootloader.NewGrub(s.rootdir, nil)
	v, err := g.GetBootVars("snap_mode")
	c.Assert(err, IsNil)
	c.Check(v, HasLen, 1)
	c.Check(v["snap_mode"], Equals, "regular")
}

func (s *grubTestSuite) TestSetBootVer(c *C) {
	s.makeFakeGrubEnv(c)

	g := bootloader.NewGrub(s.rootdir, nil)
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

	g := bootloader.NewGrub(s.rootdir, nil)

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

	g := bootloader.NewGrub(s.rootdir, nil)
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

func (s *grubTestSuite) grubDir() string {
	return filepath.Join(s.bootdir, "grub")
}

func (s *grubTestSuite) grubRecoveryDir() string {
	return filepath.Join(s.rootdir, "EFI/ubuntu")
}

func (s *grubTestSuite) makeFakeGrubRecoveryEnv(c *C) {
	err := os.MkdirAll(s.grubRecoveryDir(), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(s.grubRecoveryDir(), "grub.cfg"), nil, 0644)
	c.Assert(err, IsNil)
}

func (s *grubTestSuite) TestNewGrubWithOptionRecovery(c *C) {
	s.makeFakeGrubRecoveryEnv(c)

	g := bootloader.NewGrub(s.rootdir, &bootloader.Options{Recovery: true})
	c.Assert(g, NotNil)
	c.Assert(g.Name(), Equals, "grub")
}

func (s *grubTestSuite) TestNewGrubWithOptionRecoveryBootEnv(c *C) {
	s.makeFakeGrubRecoveryEnv(c)
	g := bootloader.NewGrub(s.rootdir, &bootloader.Options{Recovery: true})

	// check that setting vars goes to the right place
	c.Check(filepath.Join(s.grubRecoveryDir(), "grubenv"), testutil.FileAbsent)
	err := g.SetBootVars(map[string]string{
		"k1": "v1",
		"k2": "v2",
	})
	c.Assert(err, IsNil)
	c.Check(filepath.Join(s.grubRecoveryDir(), "grubenv"), testutil.FilePresent)

	env, err := g.GetBootVars("k1", "k2")
	c.Assert(err, IsNil)
	c.Check(env, DeepEquals, map[string]string{
		"k1": "v1",
		"k2": "v2",
	})
}

func (s *grubTestSuite) TestNewGrubWithOptionRecoveryNoEnv(c *C) {
	// fake a *regular* grub env
	s.makeFakeGrubEnv(c)

	// we can't create a recovery grub with that
	g := bootloader.NewGrub(s.rootdir, &bootloader.Options{Recovery: true})
	c.Assert(g, IsNil)
}

func (s *grubTestSuite) TestGrubSetRecoverySystemEnv(c *C) {
	s.makeFakeGrubRecoveryEnv(c)
	g := bootloader.NewGrub(s.rootdir, &bootloader.Options{Recovery: true})

	// check that we can set a recovery system specific bootenv
	bvars := map[string]string{
		"snapd_recovery_kernel": "/snaps/pc-kernel_1.snap",
		"other_options":         "are-supported",
	}

	err := g.SetRecoverySystemEnv("/systems/20191209", bvars)
	c.Assert(err, IsNil)
	recoverySystemGrubenv := filepath.Join(s.rootdir, "/systems/20191209/grubenv")
	c.Assert(recoverySystemGrubenv, testutil.FilePresent)

	genv := grubenv.NewEnv(recoverySystemGrubenv)
	err = genv.Load()
	c.Assert(err, IsNil)
	c.Check(genv.Get("snapd_recovery_kernel"), Equals, "/snaps/pc-kernel_1.snap")
	c.Check(genv.Get("other_options"), Equals, "are-supported")
}

func (s *grubTestSuite) makeKernelAssetSnap(c *C, snapFileName string) snap.PlaceInfo {
	kernelSnap, err := snap.ParsePlaceInfoFromSnapFileName(snapFileName)
	c.Assert(err, IsNil)

	// make a kernel.efi snap as it would be by ExtractKernelAssets()
	kernelSnapExtractedAssetsDir := filepath.Join(s.grubDir(), snapFileName)
	err = os.MkdirAll(kernelSnapExtractedAssetsDir, 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(kernelSnapExtractedAssetsDir, "kernel.efi"), nil, 0644)
	c.Assert(err, IsNil)

	return kernelSnap
}

func (s *grubTestSuite) makeKernelAssetSnapAndSymlink(c *C, snapFileName, symlinkName string) snap.PlaceInfo {
	kernelSnap := s.makeKernelAssetSnap(c, snapFileName)

	// make a kernel.efi symlink to the kernel.efi above
	err := os.Symlink(
		filepath.Join(snapFileName, "kernel.efi"),
		filepath.Join(s.grubDir(), symlinkName),
	)
	c.Assert(err, IsNil)

	return kernelSnap
}

func (s *grubTestSuite) TestGrubExtractedRunKernelImageKernel(c *C) {
	s.makeFakeGrubEnv(c)
	g := bootloader.NewGrub(s.rootdir, nil)
	eg, ok := g.(bootloader.ExtractedRunKernelImageBootloader)
	c.Assert(ok, Equals, true)

	kernel := s.makeKernelAssetSnapAndSymlink(c, "pc-kernel_1.snap", "kernel.efi")

	// ensure that the returned kernel is the same as the one we put there
	sn, err := eg.Kernel()
	c.Assert(err, IsNil)
	c.Assert(sn, DeepEquals, kernel)
}

func (s *grubTestSuite) TestGrubExtractedRunKernelImageTryKernel(c *C) {
	s.makeFakeGrubEnv(c)
	g := bootloader.NewGrub(s.rootdir, nil)
	eg, ok := g.(bootloader.ExtractedRunKernelImageBootloader)
	c.Assert(ok, Equals, true)

	// ensure it doesn't return anything when the symlink doesn't exist
	_, exists, err := eg.TryKernel()
	c.Assert(err, IsNil)
	c.Assert(exists, Equals, false)

	// when a bad kernel snap name is in the extracted path, it will complain
	// appropriately
	kernelSnapExtractedAssetsDir := filepath.Join(s.grubDir(), "bad_snap_rev_name")
	badKernelSnapPath := filepath.Join(kernelSnapExtractedAssetsDir, "kernel.efi")
	tryKernelSymlink := filepath.Join(s.grubDir(), "try-kernel.efi")
	err = os.MkdirAll(kernelSnapExtractedAssetsDir, 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(badKernelSnapPath, nil, 0644)
	c.Assert(err, IsNil)

	err = os.Symlink("bad_snap_rev_name/kernel.efi", tryKernelSymlink)
	c.Assert(err, IsNil)

	_, exists, err = eg.TryKernel()
	c.Assert(err, ErrorMatches, "bad kernel snap file path at \"bad_snap_rev_name\".*")
	c.Assert(exists, Equals, false)

	// remove the bad symlink
	err = os.Remove(tryKernelSymlink)
	c.Assert(err, IsNil)

	// make a real symlink
	tryKernel := s.makeKernelAssetSnapAndSymlink(c, "pc-kernel_2.snap", "try-kernel.efi")

	// ensure that the returned kernel is the same as the one we put there
	sn, exists, err := eg.TryKernel()
	c.Assert(err, IsNil)
	c.Assert(exists, Equals, true)
	c.Assert(sn, DeepEquals, tryKernel)

	// if the destination of the symlink is removed, we get an error
	err = os.Remove(filepath.Join(s.grubDir(), "pc-kernel_2.snap", "kernel.efi"))
	c.Assert(err, IsNil)
	_, exists, err = eg.TryKernel()
	c.Assert(exists, Equals, false)
	c.Assert(err, ErrorMatches, "cannot read dangling symlink try-kernel.efi")
}

func (s *grubTestSuite) TestGrubExtractedRunKernelImageEnableKernel(c *C) {
	s.makeFakeGrubEnv(c)
	g := bootloader.NewGrub(s.rootdir, nil)
	eg, ok := g.(bootloader.ExtractedRunKernelImageBootloader)
	c.Assert(ok, Equals, true)

	// ensure we fail to create a dangling symlink to a kernel snap that was not
	// actually extracted
	nonExistSnap, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_12.snap")
	c.Assert(err, IsNil)
	err = eg.EnableKernel(nonExistSnap)
	c.Assert(err, ErrorMatches, "cannot enable kernel.efi at pc-kernel_12.snap/kernel.efi: file does not exist")

	kernel := s.makeKernelAssetSnap(c, "pc-kernel_1.snap")

	// enable the Kernel we extracted
	err = eg.EnableKernel(kernel)
	c.Assert(err, IsNil)

	// ensure that the symlink was put where we expect it
	asset, err := os.Readlink(filepath.Join(s.grubDir(), "kernel.efi"))
	c.Assert(err, IsNil)

	c.Assert(asset, DeepEquals, filepath.Join("pc-kernel_1.snap", "kernel.efi"))
}

func (s *grubTestSuite) TestGrubExtractedRunKernelImageEnableTryKernel(c *C) {
	s.makeFakeGrubEnv(c)
	g := bootloader.NewGrub(s.rootdir, nil)
	eg, ok := g.(bootloader.ExtractedRunKernelImageBootloader)
	c.Assert(ok, Equals, true)

	kernel := s.makeKernelAssetSnap(c, "pc-kernel_1.snap")

	// enable the Kernel we extracted
	err := eg.EnableTryKernel(kernel)
	c.Assert(err, IsNil)

	// ensure that the symlink was put where we expect it
	asset, err := os.Readlink(filepath.Join(s.grubDir(), "try-kernel.efi"))
	c.Assert(err, IsNil)

	c.Assert(asset, DeepEquals, filepath.Join("pc-kernel_1.snap", "kernel.efi"))
}

func (s *grubTestSuite) TestGrubExtractedRunKernelImageDisableTryKernel(c *C) {
	s.makeFakeGrubEnv(c)
	g := bootloader.NewGrub(s.rootdir, nil)
	eg, ok := g.(bootloader.ExtractedRunKernelImageBootloader)
	c.Assert(ok, Equals, true)

	// trying to disable when the try-kernel.efi symlink is missing produces an
	// error
	err := eg.DisableTryKernel()
	c.Assert(err, ErrorMatches, "cannot disable kernel, symlink .* missing")

	// make the symlink and check that the symlink is missing afterwards
	s.makeKernelAssetSnapAndSymlink(c, "pc-kernel_1.snap", "try-kernel.efi")
	err = eg.DisableTryKernel()
	c.Assert(err, IsNil)

	// ensure that the symlink is no longer there
	c.Assert(filepath.Join(s.rootdir, "try-kernel.efi"), testutil.FileAbsent)
}
