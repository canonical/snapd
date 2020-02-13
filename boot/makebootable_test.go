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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type makeBootableSuite struct {
	baseBootSetSuite

	bootloader *bootloadertest.MockBootloader
}

var _ = Suite(&makeBootableSuite{})

func (s *makeBootableSuite) SetUpTest(c *C) {
	s.baseBootSetSuite.SetUpTest(c)

	s.bootloader = bootloadertest.Mock("mock", c.MkDir())
	s.forceBootloader(s.bootloader)
}

func (s *makeBootableSuite) makeSnap(c *C, name, yaml string, revno snap.Revision) (fn string, info *snap.Info) {
	return s.makeSnapWithFiles(c, name, yaml, revno, nil)
}

func (s *makeBootableSuite) makeSnapWithFiles(c *C, name, yaml string, revno snap.Revision, files [][]string) (fn string, info *snap.Info) {
	si := &snap.SideInfo{
		RealName: name,
		Revision: revno,
	}
	fn = snaptest.MakeTestSnapWithFiles(c, yaml, files)
	snapf, err := snap.Open(fn)
	c.Assert(err, IsNil)
	info, err = snap.ReadInfoFromSnapFile(snapf, si)
	c.Assert(err, IsNil)
	return fn, info
}

func (s *makeBootableSuite) TestMakeBootable(c *C) {
	dirs.SetRootDir("")

	headers := map[string]interface{}{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"display-name": "My Model",
		"architecture": "amd64",
		"base":         "core18",
		"gadget":       "pc=18",
		"kernel":       "pc-kernel=18",
		"timestamp":    "2018-01-01T08:00:00+00:00",
	}
	model := assertstest.FakeAssertion(headers).(*asserts.Model)

	grubCfg := []byte("#grub cfg")
	unpackedGadgetDir := c.MkDir()
	err := ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "grub.conf"), grubCfg, 0644)
	c.Assert(err, IsNil)

	rootdir := c.MkDir()

	seedSnapsDirs := filepath.Join(rootdir, "/var/lib/snapd/seed", "snaps")
	err = os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	baseFn, baseInfo := s.makeSnap(c, "core18", `name: core18
type: base
version: 4.0
`, snap.R(3))
	baseInSeed := filepath.Join(seedSnapsDirs, filepath.Base(baseInfo.MountFile()))
	err = os.Rename(baseFn, baseInSeed)
	c.Assert(err, IsNil)
	kernelFn, kernelInfo := s.makeSnap(c, "pc-kernel", `name: pc-kernel
type: kernel
version: 4.0
`, snap.R(5))
	kernelInSeed := filepath.Join(seedSnapsDirs, filepath.Base(kernelInfo.MountFile()))
	err = os.Rename(kernelFn, kernelInSeed)
	c.Assert(err, IsNil)

	bootWith := &boot.BootableSet{
		Base:              baseInfo,
		BasePath:          baseInSeed,
		Kernel:            kernelInfo,
		KernelPath:        kernelInSeed,
		UnpackedGadgetDir: unpackedGadgetDir,
	}

	err = boot.MakeBootable(model, rootdir, bootWith)
	c.Assert(err, IsNil)

	// check the bootloader config
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core", "snap_menuentry")
	c.Assert(err, IsNil)
	c.Check(m["snap_kernel"], Equals, "pc-kernel_5.snap")
	c.Check(m["snap_core"], Equals, "core18_3.snap")
	c.Check(m["snap_menuentry"], Equals, "My Model")

	// kernel was extracted as needed
	c.Check(s.bootloader.ExtractKernelAssetsCalls, DeepEquals, []snap.PlaceInfo{kernelInfo})

	// check symlinks from snap blob dir
	kernelBlob := filepath.Join(dirs.SnapBlobDirUnder(rootdir), filepath.Base(kernelInfo.MountFile()))
	dst, err := os.Readlink(filepath.Join(dirs.SnapBlobDirUnder(rootdir), filepath.Base(kernelInfo.MountFile())))
	c.Assert(err, IsNil)
	c.Check(dst, Equals, "../seed/snaps/pc-kernel_5.snap")
	c.Check(kernelBlob, testutil.FilePresent)

	baseBlob := filepath.Join(dirs.SnapBlobDirUnder(rootdir), filepath.Base(baseInfo.MountFile()))
	dst, err = os.Readlink(filepath.Join(dirs.SnapBlobDirUnder(rootdir), filepath.Base(baseInfo.MountFile())))
	c.Assert(err, IsNil)
	c.Check(dst, Equals, "../seed/snaps/core18_3.snap")
	c.Check(baseBlob, testutil.FilePresent)

	// check that the bootloader (grub here) configuration was copied
	c.Check(filepath.Join(rootdir, "boot", "grub/grub.cfg"), testutil.FileEquals, grubCfg)
}

func makeMockUC20Model() *asserts.Model {
	headers := map[string]interface{}{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model-uc20",
		"display-name": "My Model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"timestamp":    "2019-11-01T08:00:00+00:00",
		"snaps": []interface{}{
			map[string]interface{}{
				"name": "pc-linux",
				"id":   "pclinuxdidididididididididididid",
				"type": "kernel",
			},
			map[string]interface{}{
				"name": "pc",
				"id":   "pcididididididididididididididid",
				"type": "gadget",
			},
		},
	}
	return assertstest.FakeAssertion(headers).(*asserts.Model)
}

func (s *makeBootableSuite) TestMakeBootable20(c *C) {
	dirs.SetRootDir("")

	model := makeMockUC20Model()

	unpackedGadgetDir := c.MkDir()
	grubRecoveryCfg := []byte("#grub-recovery cfg")
	err := ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "grub-recovery.conf"), grubRecoveryCfg, 0644)
	c.Assert(err, IsNil)
	grubCfg := []byte("#grub cfg")
	err = ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "grub.conf"), grubCfg, 0644)
	c.Assert(err, IsNil)

	rootdir := c.MkDir()
	// on uc20 the seed layout if different
	seedSnapsDirs := filepath.Join(rootdir, "/snaps")
	err = os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	baseFn, baseInfo := s.makeSnap(c, "core20", `name: core20
type: base
version: 5.0
`, snap.R(3))
	baseInSeed := filepath.Join(seedSnapsDirs, filepath.Base(baseInfo.MountFile()))
	err = os.Rename(baseFn, baseInSeed)
	c.Assert(err, IsNil)
	kernelFn, kernelInfo := s.makeSnapWithFiles(c, "pc-kernel", `name: pc-kernel
type: kernel
version: 5.0
`, snap.R(5), [][]string{
		{"kernel.efi", "I'm a kernel.efi"},
	})
	kernelInSeed := filepath.Join(seedSnapsDirs, filepath.Base(kernelInfo.MountFile()))
	err = os.Rename(kernelFn, kernelInSeed)
	c.Assert(err, IsNil)

	recoverySystemDir := "/systems/20191209"
	bootWith := &boot.BootableSet{
		Base:              baseInfo,
		BasePath:          baseInSeed,
		Kernel:            kernelInfo,
		KernelPath:        kernelInSeed,
		RecoverySystemDir: recoverySystemDir,
		UnpackedGadgetDir: unpackedGadgetDir,
		Recovery:          true,
	}

	err = boot.MakeBootable(model, rootdir, bootWith)
	c.Assert(err, IsNil)

	// ensure only a single file got copied (the grub.cfg)
	files, err := filepath.Glob(filepath.Join(rootdir, "EFI/ubuntu/*"))
	c.Assert(err, IsNil)
	c.Check(files, HasLen, 1)
	// check that the recovery bootloader configuration was copied with
	// the correct content
	c.Check(filepath.Join(rootdir, "EFI/ubuntu/grub.cfg"), testutil.FileEquals, grubRecoveryCfg)

	// ensure no /boot was setup
	c.Check(filepath.Join(rootdir, "boot"), testutil.FileAbsent)

	// ensure the correct recovery system configuration was set
	c.Check(s.bootloader.RecoverySystemDir, Equals, recoverySystemDir)
	c.Check(s.bootloader.RecoverySystemBootVars, DeepEquals, map[string]string{
		"snapd_recovery_kernel": "/snaps/pc-kernel_5.snap",
	})
}

func (s *makeBootableSuite) TestMakeBootable20MultipleRecoverySystemsError(c *C) {
	dirs.SetRootDir("")

	model := makeMockUC20Model()

	bootWith := &boot.BootableSet{Recovery: true}
	rootdir := c.MkDir()
	err := os.MkdirAll(filepath.Join(rootdir, "systems/20191204"), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(rootdir, "systems/20191205"), 0755)
	c.Assert(err, IsNil)

	err = boot.MakeBootable(model, rootdir, bootWith)
	c.Assert(err, ErrorMatches, "cannot make multiple recovery systems bootable yet")
}

func (s *makeBootableSuite) TestMakeBootable20RunMode(c *C) {
	dirs.SetRootDir("")
	bootloader.Force(nil)

	model := makeMockUC20Model()
	rootdir := c.MkDir()
	seedSnapsDirs := filepath.Join(rootdir, "/snaps")
	err := os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	// grub on ubuntu-seed
	runMnt := filepath.Join(rootdir, "/run/mnt/")
	mockSeedGrubDir := filepath.Join(runMnt, "ubuntu-seed", "EFI", "ubuntu")
	mockSeedGrubCfg := filepath.Join(mockSeedGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockSeedGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockSeedGrubCfg, nil, 0644)
	c.Assert(err, IsNil)

	// grub on ubuntu-boot
	ubuntuBootPartition := filepath.Join(runMnt, "ubuntu-boot")
	mockBootGrubDir := filepath.Join(ubuntuBootPartition, "EFI", "ubuntu")
	mockBootGrubCfg := filepath.Join(mockBootGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockBootGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockBootGrubCfg, nil, 0644)
	c.Assert(err, IsNil)

	baseFn, baseInfo := s.makeSnap(c, "core20", `name: core20
type: base
version: 5.0
`, snap.R(3))
	baseInSeed := filepath.Join(seedSnapsDirs, filepath.Base(baseInfo.MountFile()))
	err = os.Rename(baseFn, baseInSeed)
	c.Assert(err, IsNil)
	kernelFn, kernelInfo := s.makeSnapWithFiles(c, "pc-kernel", `name: pc-kernel
type: kernel
version: 5.0
`, snap.R(5),
		[][]string{
			{"kernel.efi", "I'm a kernel.efi"},
		},
	)
	kernelInSeed := filepath.Join(seedSnapsDirs, filepath.Base(kernelInfo.MountFile()))
	err = os.Rename(kernelFn, kernelInSeed)
	c.Assert(err, IsNil)

	bootWith := &boot.BootableSet{
		RecoverySystemDir: "20191216",
		BasePath:          baseInSeed,
		Base:              baseInfo,
		KernelPath:        kernelInSeed,
		Kernel:            kernelInfo,
		Recovery:          false,
	}

	err = boot.MakeBootable(model, rootdir, bootWith)
	c.Assert(err, IsNil)

	// ensure base/kernel got copied to /var/lib/snapd/snaps
	runMntUbuntuData := filepath.Join(rootdir, "/run/mnt/ubuntu-data/system-data")
	c.Check(filepath.Join(runMntUbuntuData, dirs.SnapBlobDir, "core20_3.snap"), testutil.FilePresent)
	c.Check(filepath.Join(runMntUbuntuData, dirs.SnapBlobDir, "pc-kernel_5.snap"), testutil.FilePresent)

	// ensure the bootvars got updated the right way
	mockSeedGrubenv := filepath.Join(mockSeedGrubDir, "grubenv")
	c.Check(mockSeedGrubenv, testutil.FilePresent)
	c.Check(mockSeedGrubenv, testutil.FileContains, "snapd_recovery_mode=run")
	// TODO:UC20: update once we write the static UC20 kernels and stop
	// using the UC16 bootmode
	mockBootGrubenv := filepath.Join(mockBootGrubDir, "grubenv")
	c.Check(mockBootGrubenv, testutil.FilePresent)

	// ensure that kernel_status is empty, we specifically want this to be set
	// to the empty string
	// use (?m) to match multi-line file in the regex here, because the file is
	// a grubenv with padding #### blocks
	c.Check(mockBootGrubenv, testutil.FileMatches, `(?m)^kernel_status=$`)

	// check that we have the extracted kernel in the right places, both in the
	// old uc16/uc18 location and the new ubuntu-boot partition grub dir
	extractedKernel := filepath.Join(mockBootGrubDir, "pc-kernel_5.snap", "kernel.efi")
	c.Check(extractedKernel, testutil.FilePresent)

	// the new uc20 location
	extractedKernelSymlink := filepath.Join(mockBootGrubDir, "kernel.efi")
	c.Check(extractedKernelSymlink, testutil.FilePresent)

	// ensure modeenv looks correct
	ubuntuDataModeEnvPath := filepath.Join(rootdir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/modeenv")
	c.Check(ubuntuDataModeEnvPath, testutil.FileEquals, `mode=run
recovery_system=20191216
base=core20_3.snap
`)
}
