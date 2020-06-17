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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/assets"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/bootloader/ubootenv"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type makeBootableSuite struct {
	baseBootenvSuite

	bootloader *bootloadertest.MockBootloader
}

var _ = Suite(&makeBootableSuite{})

func (s *makeBootableSuite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)

	s.bootloader = bootloadertest.Mock("mock", c.MkDir())
	s.forceBootloader(s.bootloader)
}

func makeSnap(c *C, name, yaml string, revno snap.Revision) (fn string, info *snap.Info) {
	return makeSnapWithFiles(c, name, yaml, revno, nil)
}

func makeSnapWithFiles(c *C, name, yaml string, revno snap.Revision, files [][]string) (fn string, info *snap.Info) {
	si := &snap.SideInfo{
		RealName: name,
		Revision: revno,
	}
	fn = snaptest.MakeTestSnapWithFiles(c, yaml, files)
	snapf, err := snapfile.Open(fn)
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

	baseFn, baseInfo := makeSnap(c, "core18", `name: core18
type: base
version: 4.0
`, snap.R(3))
	baseInSeed := filepath.Join(seedSnapsDirs, baseInfo.Filename())
	err = os.Rename(baseFn, baseInSeed)
	c.Assert(err, IsNil)
	kernelFn, kernelInfo := makeSnap(c, "pc-kernel", `name: pc-kernel
type: kernel
version: 4.0
`, snap.R(5))
	kernelInSeed := filepath.Join(seedSnapsDirs, kernelInfo.Filename())
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
	kernelBlob := filepath.Join(dirs.SnapBlobDirUnder(rootdir), kernelInfo.Filename())
	dst, err := os.Readlink(filepath.Join(dirs.SnapBlobDirUnder(rootdir), kernelInfo.Filename()))
	c.Assert(err, IsNil)
	c.Check(dst, Equals, "../seed/snaps/pc-kernel_5.snap")
	c.Check(kernelBlob, testutil.FilePresent)

	baseBlob := filepath.Join(dirs.SnapBlobDirUnder(rootdir), baseInfo.Filename())
	dst, err = os.Readlink(filepath.Join(dirs.SnapBlobDirUnder(rootdir), baseInfo.Filename()))
	c.Assert(err, IsNil)
	c.Check(dst, Equals, "../seed/snaps/core18_3.snap")
	c.Check(baseBlob, testutil.FilePresent)

	// check that the bootloader (grub here) configuration was copied
	c.Check(filepath.Join(rootdir, "boot", "grub/grub.cfg"), testutil.FileEquals, grubCfg)
}

type makeBootable20Suite struct {
	baseBootenvSuite

	bootloader *bootloadertest.MockRecoveryAwareBootloader
}

type makeBootable20UbootSuite struct {
	baseBootenvSuite

	bootloader *bootloadertest.MockExtractedRecoveryKernelImageBootloader
}

var _ = Suite(&makeBootable20Suite{})
var _ = Suite(&makeBootable20UbootSuite{})

func (s *makeBootable20Suite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)

	s.bootloader = bootloadertest.Mock("mock", c.MkDir()).RecoveryAware()
	s.forceBootloader(s.bootloader)
}

func (s *makeBootable20UbootSuite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)

	s.bootloader = bootloadertest.Mock("mock", c.MkDir()).ExtractedRecoveryKernelImage()
	s.forceBootloader(s.bootloader)
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

func (s *makeBootable20Suite) TestMakeBootable20(c *C) {
	dirs.SetRootDir("")

	model := makeMockUC20Model()

	unpackedGadgetDir := c.MkDir()
	grubRecoveryCfg := []byte("#grub-recovery cfg")
	grubRecoveryCfgAsset := []byte("#grub-recovery cfg from assets")
	err := ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "grub-recovery.conf"), grubRecoveryCfg, 0644)
	restore := assets.MockInternal("grub-recovery.cfg", grubRecoveryCfgAsset)
	defer restore()

	c.Assert(err, IsNil)
	grubCfg := []byte("#grub cfg")
	err = ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "grub.conf"), grubCfg, 0644)
	c.Assert(err, IsNil)

	rootdir := c.MkDir()
	// on uc20 the seed layout if different
	seedSnapsDirs := filepath.Join(rootdir, "/snaps")
	err = os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	baseFn, baseInfo := makeSnap(c, "core20", `name: core20
type: base
version: 5.0
`, snap.R(3))
	baseInSeed := filepath.Join(seedSnapsDirs, baseInfo.Filename())
	err = os.Rename(baseFn, baseInSeed)
	c.Assert(err, IsNil)
	kernelFn, kernelInfo := makeSnapWithFiles(c, "pc-kernel", `name: pc-kernel
type: kernel
version: 5.0
`, snap.R(5), [][]string{
		{"kernel.efi", "I'm a kernel.efi"},
	})
	kernelInSeed := filepath.Join(seedSnapsDirs, kernelInfo.Filename())
	err = os.Rename(kernelFn, kernelInSeed)
	c.Assert(err, IsNil)

	label := "20191209"
	recoverySystemDir := filepath.Join("/systems", label)
	bootWith := &boot.BootableSet{
		Base:                baseInfo,
		BasePath:            baseInSeed,
		Kernel:              kernelInfo,
		KernelPath:          kernelInSeed,
		RecoverySystemDir:   recoverySystemDir,
		RecoverySystemLabel: label,
		UnpackedGadgetDir:   unpackedGadgetDir,
		Recovery:            true,
	}

	err = boot.MakeBootable(model, rootdir, bootWith)
	c.Assert(err, IsNil)

	// ensure only a single file got copied (the grub.cfg)
	files, err := filepath.Glob(filepath.Join(rootdir, "EFI/ubuntu/*"))
	c.Assert(err, IsNil)
	c.Check(files, HasLen, 1)
	// check that the recovery bootloader configuration was copied with
	// the correct content
	c.Check(filepath.Join(rootdir, "EFI/ubuntu/grub.cfg"), testutil.FileEquals, grubRecoveryCfgAsset)

	// ensure no /boot was setup
	c.Check(filepath.Join(rootdir, "boot"), testutil.FileAbsent)

	// ensure the correct recovery system configuration was set
	c.Check(s.bootloader.RecoverySystemDir, Equals, recoverySystemDir)
	c.Check(s.bootloader.RecoverySystemBootVars, DeepEquals, map[string]string{
		"snapd_recovery_kernel": "/snaps/pc-kernel_5.snap",
	})
	c.Check(s.bootloader.BootVars, DeepEquals, map[string]string{
		"snapd_recovery_system": label,
	})
}

func (s *makeBootable20Suite) TestMakeBootable20UnsetRecoverySystemLabelError(c *C) {
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

	label := "20191209"
	recoverySystemDir := filepath.Join("/systems", label)
	bootWith := &boot.BootableSet{
		RecoverySystemDir: recoverySystemDir,
		UnpackedGadgetDir: unpackedGadgetDir,
		Recovery:          true,
	}

	err = boot.MakeBootable(model, rootdir, bootWith)
	c.Assert(err, ErrorMatches, "internal error: recovery system label unset")
}

func (s *makeBootable20Suite) TestMakeBootable20MultipleRecoverySystemsError(c *C) {
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

func (s *makeBootable20Suite) TestMakeBootable20RunMode(c *C) {
	dirs.SetRootDir("")
	bootloader.Force(nil)

	model := makeMockUC20Model()
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)
	seedSnapsDirs := filepath.Join(rootdir, "/snaps")
	err := os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	// grub on ubuntu-seed
	mockSeedGrubDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI", "ubuntu")
	mockSeedGrubCfg := filepath.Join(mockSeedGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockSeedGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockSeedGrubCfg, nil, 0644)
	c.Assert(err, IsNil)

	// grub on ubuntu-boot
	mockBootGrubDir := filepath.Join(boot.InitramfsUbuntuBootDir, "EFI", "ubuntu")
	mockBootGrubCfg := filepath.Join(mockBootGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockBootGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockBootGrubCfg, nil, 0644)
	c.Assert(err, IsNil)

	// make the snaps symlinks so that we can ensure that makebootable follows
	// the symlinks and copies the files and not the symlinks
	baseFn, baseInfo := makeSnap(c, "core20", `name: core20
type: base
version: 5.0
`, snap.R(3))
	baseInSeed := filepath.Join(seedSnapsDirs, baseInfo.Filename())
	err = os.Symlink(baseFn, baseInSeed)
	c.Assert(err, IsNil)
	kernelFn, kernelInfo := makeSnapWithFiles(c, "pc-kernel", `name: pc-kernel
type: kernel
version: 5.0
`, snap.R(5),
		[][]string{
			{"kernel.efi", "I'm a kernel.efi"},
		},
	)
	kernelInSeed := filepath.Join(seedSnapsDirs, kernelInfo.Filename())
	err = os.Symlink(kernelFn, kernelInSeed)
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
	core20Snap := filepath.Join(dirs.SnapBlobDirUnder(boot.InstallHostWritableDir), "core20_3.snap")
	pcKernelSnap := filepath.Join(dirs.SnapBlobDirUnder(boot.InstallHostWritableDir), "pc-kernel_5.snap")
	c.Check(core20Snap, testutil.FilePresent)
	c.Check(pcKernelSnap, testutil.FilePresent)
	c.Check(osutil.IsSymlink(core20Snap), Equals, false)
	c.Check(osutil.IsSymlink(pcKernelSnap), Equals, false)

	// ensure the bootvars got updated the right way
	mockSeedGrubenv := filepath.Join(mockSeedGrubDir, "grubenv")
	c.Check(mockSeedGrubenv, testutil.FilePresent)
	c.Check(mockSeedGrubenv, testutil.FileContains, "snapd_recovery_mode=run")
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
current_kernels=pc-kernel_5.snap
model=my-brand/my-model-uc20
grade=dangerous
`)
}

func (s *makeBootable20UbootSuite) TestUbootMakeBootable20TraditionalUbootenvFails(c *C) {
	dirs.SetRootDir("")

	model := makeMockUC20Model()

	unpackedGadgetDir := c.MkDir()
	ubootEnv := []byte("#uboot env")
	err := ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "uboot.conf"), ubootEnv, 0644)
	c.Assert(err, IsNil)

	rootdir := c.MkDir()
	// on uc20 the seed layout if different
	seedSnapsDirs := filepath.Join(rootdir, "/snaps")
	err = os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	baseFn, baseInfo := makeSnap(c, "core20", `name: core20
type: base
version: 5.0
`, snap.R(3))
	baseInSeed := filepath.Join(seedSnapsDirs, baseInfo.Filename())
	err = os.Rename(baseFn, baseInSeed)
	c.Assert(err, IsNil)
	kernelFn, kernelInfo := makeSnapWithFiles(c, "arm-kernel", `name: arm-kernel
type: kernel
version: 5.0
`, snap.R(5), [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"dtbs/foo.dtb", "foo dtb"},
		{"dtbs/bar.dto", "bar dtbo"},
	})
	kernelInSeed := filepath.Join(seedSnapsDirs, kernelInfo.Filename())
	err = os.Rename(kernelFn, kernelInSeed)
	c.Assert(err, IsNil)

	label := "20191209"
	recoverySystemDir := filepath.Join("/systems", label)
	bootWith := &boot.BootableSet{
		Base:                baseInfo,
		BasePath:            baseInSeed,
		Kernel:              kernelInfo,
		KernelPath:          kernelInSeed,
		RecoverySystemDir:   recoverySystemDir,
		RecoverySystemLabel: label,
		UnpackedGadgetDir:   unpackedGadgetDir,
		Recovery:            true,
	}

	// TODO:UC20: enable this use case
	err = boot.MakeBootable(model, rootdir, bootWith)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot find boot config in %q", unpackedGadgetDir))
}

func (s *makeBootable20UbootSuite) TestUbootMakeBootable20BootScr(c *C) {
	dirs.SetRootDir("")

	model := makeMockUC20Model()

	unpackedGadgetDir := c.MkDir()
	// the uboot.conf must be empty for this to work/do the right thing
	err := ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "uboot.conf"), nil, 0644)
	c.Assert(err, IsNil)

	rootdir := c.MkDir()
	// on uc20 the seed layout if different
	seedSnapsDirs := filepath.Join(rootdir, "/snaps")
	err = os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	baseFn, baseInfo := makeSnap(c, "core20", `name: core20
type: base
version: 5.0
`, snap.R(3))
	baseInSeed := filepath.Join(seedSnapsDirs, baseInfo.Filename())
	err = os.Rename(baseFn, baseInSeed)
	c.Assert(err, IsNil)
	kernelFn, kernelInfo := makeSnapWithFiles(c, "arm-kernel", `name: arm-kernel
type: kernel
version: 5.0
`, snap.R(5), [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"dtbs/foo.dtb", "foo dtb"},
		{"dtbs/bar.dto", "bar dtbo"},
	})
	kernelInSeed := filepath.Join(seedSnapsDirs, kernelInfo.Filename())
	err = os.Rename(kernelFn, kernelInSeed)
	c.Assert(err, IsNil)

	label := "20191209"
	recoverySystemDir := filepath.Join("/systems", label)
	bootWith := &boot.BootableSet{
		Base:                baseInfo,
		BasePath:            baseInSeed,
		Kernel:              kernelInfo,
		KernelPath:          kernelInSeed,
		RecoverySystemDir:   recoverySystemDir,
		RecoverySystemLabel: label,
		UnpackedGadgetDir:   unpackedGadgetDir,
		Recovery:            true,
	}

	err = boot.MakeBootable(model, rootdir, bootWith)
	c.Assert(err, IsNil)

	// since uboot.conf was absent, we won't have installed the uboot.env, as
	// it is expected that the gadget assets would have installed boot.scr
	// instead
	c.Check(filepath.Join(rootdir, "uboot.env"), testutil.FileAbsent)

	c.Check(s.bootloader.BootVars, DeepEquals, map[string]string{
		"snapd_recovery_system": label,
	})

	// ensure the correct recovery system configuration was set
	c.Check(
		s.bootloader.ExtractRecoveryKernelAssetsCalls,
		DeepEquals,
		[]bootloadertest.ExtractedRecoveryKernelCall{{
			RecoverySystemDir: recoverySystemDir,
			S:                 kernelInfo,
		}},
	)
}

func (s *makeBootable20UbootSuite) TestUbootMakeBootable20RunModeBootScr(c *C) {
	dirs.SetRootDir("")
	bootloader.Force(nil)

	model := makeMockUC20Model()
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)
	seedSnapsDirs := filepath.Join(rootdir, "/snaps")
	err := os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	// uboot on ubuntu-seed
	mockSeedUbootBootSel := filepath.Join(boot.InitramfsUbuntuSeedDir, "uboot/ubuntu/boot.sel")
	err = os.MkdirAll(filepath.Dir(mockSeedUbootBootSel), 0755)
	c.Assert(err, IsNil)
	env, err := ubootenv.Create(mockSeedUbootBootSel, 4096)
	c.Assert(err, IsNil)
	c.Assert(env.Save(), IsNil)

	// uboot on ubuntu-boot
	mockBootUbootBootSel := filepath.Join(boot.InitramfsUbuntuBootDir, "uboot/ubuntu/boot.sel")
	err = os.MkdirAll(filepath.Dir(mockBootUbootBootSel), 0755)
	c.Assert(err, IsNil)
	env, err = ubootenv.Create(mockBootUbootBootSel, 4096)
	c.Assert(err, IsNil)
	c.Assert(env.Save(), IsNil)

	baseFn, baseInfo := makeSnap(c, "core20", `name: core20
type: base
version: 5.0
`, snap.R(3))
	baseInSeed := filepath.Join(seedSnapsDirs, baseInfo.Filename())
	err = os.Rename(baseFn, baseInSeed)
	c.Assert(err, IsNil)
	kernelSnapFiles := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"dtbs/foo.dtb", "foo dtb"},
		{"dtbs/bar.dto", "bar dtbo"},
	}
	kernelFn, kernelInfo := makeSnapWithFiles(c, "arm-kernel", `name: arm-kernel
type: kernel
version: 5.0
`, snap.R(5), kernelSnapFiles)
	kernelInSeed := filepath.Join(seedSnapsDirs, kernelInfo.Filename())
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
	c.Check(filepath.Join(dirs.SnapBlobDirUnder(boot.InstallHostWritableDir), "core20_3.snap"), testutil.FilePresent)
	c.Check(filepath.Join(dirs.SnapBlobDirUnder(boot.InstallHostWritableDir), "arm-kernel_5.snap"), testutil.FilePresent)

	// ensure the bootvars on ubuntu-seed got updated the right way
	mockSeedUbootenv := filepath.Join(boot.InitramfsUbuntuSeedDir, "uboot/ubuntu/boot.sel")
	uenvSeed, err := ubootenv.Open(mockSeedUbootenv)
	c.Assert(err, IsNil)
	c.Assert(uenvSeed.Get("snapd_recovery_mode"), Equals, "run")

	// now check ubuntu-boot boot.sel
	mockBootUbootenv := filepath.Join(boot.InitramfsUbuntuBootDir, "uboot/ubuntu/boot.sel")
	uenvBoot, err := ubootenv.Open(mockBootUbootenv)
	c.Assert(err, IsNil)
	c.Assert(uenvBoot.Get("snap_try_kernel"), Equals, "")
	c.Assert(uenvBoot.Get("snap_kernel"), Equals, "arm-kernel_5.snap")
	c.Assert(uenvBoot.Get("kernel_status"), Equals, boot.DefaultStatus)

	// check that we have the extracted kernel in the right places, in the
	// old uc16/uc18 location
	for _, file := range kernelSnapFiles {
		fName := file[0]
		c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "uboot/ubuntu/arm-kernel_5.snap", fName), testutil.FilePresent)
	}

	// ensure modeenv looks correct
	ubuntuDataModeEnvPath := filepath.Join(rootdir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/modeenv")
	c.Check(ubuntuDataModeEnvPath, testutil.FileEquals, `mode=run
recovery_system=20191216
base=core20_3.snap
current_kernels=arm-kernel_5.snap
model=my-brand/my-model-uc20
grade=dangerous
`)
}
