// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022, 2024 Canonical Ltd
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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch/archtest"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/assets"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/bootloader/grubenv"
	"github.com/snapcore/snapd/bootloader/ubootenv"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
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

	s.AddCleanup(archtest.MockArchitecture("amd64"))
	snippets := []assets.ForEditions{
		{FirstEdition: 1, Snippet: []byte("console=ttyS0 console=tty1 panic=-1")},
	}
	s.AddCleanup(assets.MockSnippetsForEdition("grub.cfg:static-cmdline", snippets))
	s.AddCleanup(assets.MockSnippetsForEdition("grub-recovery.cfg:static-cmdline", snippets))
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

func (s *makeBootableSuite) TestMakeBootableImage(c *C) {
	bootloader.Force(nil)
	model := boottest.MakeMockModel()

	grubCfg := []byte("#grub cfg")
	unpackedGadgetDir := c.MkDir()
	err := os.WriteFile(filepath.Join(unpackedGadgetDir, "grub.conf"), grubCfg, 0644)
	c.Assert(err, IsNil)

	seedSnapsDirs := filepath.Join(s.rootdir, "/var/lib/snapd/seed", "snaps")
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

	err = boot.MakeBootableImage(model, s.rootdir, bootWith, nil)
	c.Assert(err, IsNil)

	// check the bootloader config
	seedGenv := grubenv.NewEnv(filepath.Join(s.rootdir, "boot/grub/grubenv"))
	c.Assert(seedGenv.Load(), IsNil)
	c.Check(seedGenv.Get("snap_kernel"), Equals, "pc-kernel_5.snap")
	c.Check(seedGenv.Get("snap_core"), Equals, "core18_3.snap")
	c.Check(seedGenv.Get("snap_menuentry"), Equals, "My Model")

	// check symlinks from snap blob dir
	kernelBlob := filepath.Join(dirs.SnapBlobDirUnder(s.rootdir), kernelInfo.Filename())
	dst, err := os.Readlink(filepath.Join(dirs.SnapBlobDirUnder(s.rootdir), kernelInfo.Filename()))
	c.Assert(err, IsNil)
	c.Check(dst, Equals, "../seed/snaps/pc-kernel_5.snap")
	c.Check(kernelBlob, testutil.FilePresent)

	baseBlob := filepath.Join(dirs.SnapBlobDirUnder(s.rootdir), baseInfo.Filename())
	dst, err = os.Readlink(filepath.Join(dirs.SnapBlobDirUnder(s.rootdir), baseInfo.Filename()))
	c.Assert(err, IsNil)
	c.Check(dst, Equals, "../seed/snaps/core18_3.snap")
	c.Check(baseBlob, testutil.FilePresent)

	// check that the bootloader (grub here) configuration was copied
	c.Check(filepath.Join(s.rootdir, "boot", "grub/grub.cfg"), testutil.FileEquals, grubCfg)
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
	s.AddCleanup(archtest.MockArchitecture("amd64"))
	snippets := []assets.ForEditions{
		{FirstEdition: 1, Snippet: []byte("console=ttyS0 console=tty1 panic=-1")},
	}
	s.AddCleanup(assets.MockSnippetsForEdition("grub.cfg:static-cmdline", snippets))
	s.AddCleanup(assets.MockSnippetsForEdition("grub-recovery.cfg:static-cmdline", snippets))
}

func (s *makeBootable20UbootSuite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)

	s.bootloader = bootloadertest.Mock("mock", c.MkDir()).ExtractedRecoveryKernelImage()
	s.forceBootloader(s.bootloader)
}

const gadgetYaml = `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: ubuntu-seed
        role: system-seed
        filesystem: vfat
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
      - name: ubuntu-boot
        role: system-boot
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 750M
      - name: ubuntu-data
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
`

func (s *makeBootable20Suite) TestMakeBootableImage20(c *C) {
	bootloader.Force(nil)
	model := boottest.MakeMockUC20Model()

	unpackedGadgetDir := c.MkDir()
	grubRecoveryCfg := "#grub-recovery cfg"
	grubRecoveryCfgAsset := "#grub-recovery cfg from assets"
	grubCfg := "#grub cfg"
	snaptest.PopulateDir(unpackedGadgetDir, [][]string{
		{"grub-recovery.conf", grubRecoveryCfg},
		{"grub.conf", grubCfg},
		{"meta/snap.yaml", gadgetSnapYaml},
		{"meta/gadget.yaml", gadgetYaml},
	})
	restore := assets.MockInternal("grub-recovery.cfg", []byte(grubRecoveryCfgAsset))
	defer restore()

	// on uc20 the seed layout if different
	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
	err := os.MkdirAll(seedSnapsDirs, 0755)
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

	err = boot.MakeBootableImage(model, s.rootdir, bootWith, nil)
	c.Assert(err, IsNil)

	// ensure only a single file got copied (the grub.cfg)
	files, err := filepath.Glob(filepath.Join(s.rootdir, "EFI/ubuntu/*"))
	c.Assert(err, IsNil)
	// grub.cfg and grubenv
	c.Check(files, HasLen, 2)
	// check that the recovery bootloader configuration was installed with
	// the correct content
	c.Check(filepath.Join(s.rootdir, "EFI/ubuntu/grub.cfg"), testutil.FileEquals, grubRecoveryCfgAsset)

	// ensure no /boot was setup
	c.Check(filepath.Join(s.rootdir, "boot"), testutil.FileAbsent)

	// ensure the correct recovery system configuration was set
	seedGenv := grubenv.NewEnv(filepath.Join(s.rootdir, "EFI/ubuntu/grubenv"))
	c.Assert(seedGenv.Load(), IsNil)
	c.Check(seedGenv.Get("snapd_recovery_system"), Equals, label)

	systemGenv := grubenv.NewEnv(filepath.Join(s.rootdir, recoverySystemDir, "grubenv"))
	c.Assert(systemGenv.Load(), IsNil)
	c.Check(systemGenv.Get("snapd_recovery_kernel"), Equals, "/snaps/pc-kernel_5.snap")
}

func (s *makeBootable20Suite) TestMakeBootableImage20BootFlags(c *C) {
	bootloader.Force(nil)
	model := boottest.MakeMockUC20Model()

	unpackedGadgetDir := c.MkDir()
	grubRecoveryCfg := "#grub-recovery cfg"
	grubRecoveryCfgAsset := "#grub-recovery cfg from assets"
	grubCfg := "#grub cfg"
	snaptest.PopulateDir(unpackedGadgetDir, [][]string{
		{"grub-recovery.conf", grubRecoveryCfg},
		{"grub.conf", grubCfg},
		{"meta/snap.yaml", gadgetSnapYaml},
		{"meta/gadget.yaml", gadgetYaml},
	})
	restore := assets.MockInternal("grub-recovery.cfg", []byte(grubRecoveryCfgAsset))
	defer restore()

	// on uc20 the seed layout if different
	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
	err := os.MkdirAll(seedSnapsDirs, 0755)
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
	bootFlags := []string{"factory"}

	err = boot.MakeBootableImage(model, s.rootdir, bootWith, bootFlags)
	c.Assert(err, IsNil)

	// ensure the correct recovery system configuration was set
	seedGenv := grubenv.NewEnv(filepath.Join(s.rootdir, "EFI/ubuntu/grubenv"))
	c.Assert(seedGenv.Load(), IsNil)
	c.Check(seedGenv.Get("snapd_recovery_system"), Equals, label)
	c.Check(seedGenv.Get("snapd_boot_flags"), Equals, "factory")

	systemGenv := grubenv.NewEnv(filepath.Join(s.rootdir, recoverySystemDir, "grubenv"))
	c.Assert(systemGenv.Load(), IsNil)
	c.Check(systemGenv.Get("snapd_recovery_kernel"), Equals, "/snaps/pc-kernel_5.snap")

}

func (s *makeBootable20Suite) testMakeBootableImage20CustomKernelArgs(c *C, whichFile, content, errMsg string) {
	bootloader.Force(nil)
	model := boottest.MakeMockUC20Model()

	unpackedGadgetDir := c.MkDir()
	grubCfg := "#grub cfg"
	snaptest.PopulateDir(unpackedGadgetDir, [][]string{
		{"grub.conf", grubCfg},
		{"meta/snap.yaml", gadgetSnapYaml},
		{"meta/gadget.yaml", gadgetYaml},
		{whichFile, content},
	})

	// on uc20 the seed layout if different
	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
	err := os.MkdirAll(seedSnapsDirs, 0755)
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

	err = boot.MakeBootableImage(model, s.rootdir, bootWith, nil)
	if errMsg != "" {
		c.Assert(err, ErrorMatches, errMsg)
		return
	}
	c.Assert(err, IsNil)

	// ensure the correct recovery system configuration was set
	seedGenv := grubenv.NewEnv(filepath.Join(s.rootdir, "EFI/ubuntu/grubenv"))
	c.Assert(seedGenv.Load(), IsNil)
	c.Check(seedGenv.Get("snapd_recovery_system"), Equals, label)
	// and kernel command line
	systemGenv := grubenv.NewEnv(filepath.Join(s.rootdir, recoverySystemDir, "grubenv"))
	c.Assert(systemGenv.Load(), IsNil)
	c.Check(systemGenv.Get("snapd_recovery_kernel"), Equals, "/snaps/pc-kernel_5.snap")
	switch whichFile {
	case "cmdline.extra":
		blopts := &bootloader.Options{
			Role: bootloader.RoleRecovery,
		}
		bl, err := bootloader.Find(s.rootdir, blopts)
		c.Assert(err, IsNil)
		tbl, ok := bl.(bootloader.TrustedAssetsBootloader)
		if ok {
			candidate := false
			defaultCmdLine, err := tbl.DefaultCommandLine(candidate)
			c.Assert(err, IsNil)
			c.Check(systemGenv.Get("snapd_extra_cmdline_args"), Equals, "")
			c.Check(systemGenv.Get("snapd_full_cmdline_args"), Equals, strutil.JoinNonEmpty([]string{defaultCmdLine, content}, " "))
		} else {
			c.Check(systemGenv.Get("snapd_extra_cmdline_args"), Equals, content)
			c.Check(systemGenv.Get("snapd_full_cmdline_args"), Equals, "")
		}
	case "cmdline.full":
		c.Check(systemGenv.Get("snapd_extra_cmdline_args"), Equals, "")
		c.Check(systemGenv.Get("snapd_full_cmdline_args"), Equals, content)
	}
}

func (s *makeBootable20Suite) TestMakeBootableImage20CustomKernelExtraArgs(c *C) {
	s.testMakeBootableImage20CustomKernelArgs(c, "cmdline.extra", "foo bar baz", "")
}

func (s *makeBootable20Suite) TestMakeBootableImage20CustomKernelFullArgs(c *C) {
	s.testMakeBootableImage20CustomKernelArgs(c, "cmdline.full", "foo bar baz", "")
}

func (s *makeBootable20Suite) TestMakeBootableImage20CustomKernelInvalidArgs(c *C) {
	errMsg := `cannot obtain recovery system command line: cannot use kernel command line from gadget: invalid kernel command line in cmdline.extra: disallowed kernel argument "snapd_foo=bar"`
	s.testMakeBootableImage20CustomKernelArgs(c, "cmdline.extra", "snapd_foo=bar", errMsg)
}

func (s *makeBootable20Suite) TestMakeBootableImage20UnsetRecoverySystemLabelError(c *C) {
	model := boottest.MakeMockUC20Model()

	unpackedGadgetDir := c.MkDir()
	grubRecoveryCfg := []byte("#grub-recovery cfg")
	err := os.WriteFile(filepath.Join(unpackedGadgetDir, "grub-recovery.conf"), grubRecoveryCfg, 0644)
	c.Assert(err, IsNil)
	grubCfg := []byte("#grub cfg")
	err = os.WriteFile(filepath.Join(unpackedGadgetDir, "grub.conf"), grubCfg, 0644)
	c.Assert(err, IsNil)

	label := "20191209"
	recoverySystemDir := filepath.Join("/systems", label)
	bootWith := &boot.BootableSet{
		RecoverySystemDir: recoverySystemDir,
		UnpackedGadgetDir: unpackedGadgetDir,
		Recovery:          true,
	}

	err = boot.MakeBootableImage(model, s.rootdir, bootWith, nil)
	c.Assert(err, ErrorMatches, "internal error: recovery system label unset")
}

func (s *makeBootable20Suite) TestMakeBootableImage20MultipleRecoverySystemsError(c *C) {
	model := boottest.MakeMockUC20Model()

	bootWith := &boot.BootableSet{Recovery: true}
	err := os.MkdirAll(filepath.Join(s.rootdir, "systems/20191204"), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(s.rootdir, "systems/20191205"), 0755)
	c.Assert(err, IsNil)

	err = boot.MakeBootableImage(model, s.rootdir, bootWith, nil)
	c.Assert(err, ErrorMatches, "cannot make multiple recovery systems bootable yet")
}

func (s *makeBootable20Suite) TestMakeSystemRunnable16Fails(c *C) {
	model := boottest.MakeMockModel()

	err := boot.MakeRunnableSystem(model, nil, nil)
	c.Assert(err, ErrorMatches, `internal error: cannot make pre-UC20 system runnable`)
}

func (s *makeBootable20Suite) testMakeSystemRunnable20(c *C, standalone, factoryReset, classic bool, fromInitrd bool) {
	restore := release.MockOnClassic(classic)
	defer restore()
	dirs.SetRootDir(dirs.GlobalRootDir)

	bootloader.Force(nil)

	uefiVariableSet := 0
	defer boot.MockSetEfiBootVariables(func(description string, assetPath string, optionalData []byte) error {
		uefiVariableSet += 1
		c.Check(description, Equals, "ubuntu")
		return nil
	})()

	var model *asserts.Model
	if classic {
		model = boottest.MakeMockUC20Model(map[string]interface{}{
			"classic":      "true",
			"distribution": "ubuntu",
		})
	} else {
		model = boottest.MakeMockUC20Model()
	}
	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
	err := os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	// grub on ubuntu-seed
	mockSeedGrubDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI", "ubuntu")
	mockSeedGrubCfg := filepath.Join(mockSeedGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockSeedGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(mockSeedGrubCfg, []byte("# Snapd-Boot-Config-Edition: 1\n"), 0644)
	c.Assert(err, IsNil)
	genv := grubenv.NewEnv(filepath.Join(mockSeedGrubDir, "grubenv"))
	c.Assert(genv.Save(), IsNil)

	// setup recovery boot assets
	err = os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot"), 0755)
	c.Assert(err, IsNil)
	// SHA3-384: 39efae6545f16e39633fbfbef0d5e9fdd45a25d7df8764978ce4d81f255b038046a38d9855e42e5c7c4024e153fd2e37
	err = os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot/bootx64.efi"),
		[]byte("recovery shim content"), 0644)
	c.Assert(err, IsNil)
	// SHA3-384: aa3c1a83e74bf6dd40dd64e5c5bd1971d75cdf55515b23b9eb379f66bf43d4661d22c4b8cf7d7a982d2013ab65c1c4c5
	err = os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot/grubx64.efi"),
		[]byte("recovery grub content"), 0644)
	c.Assert(err, IsNil)

	// grub on ubuntu-boot
	mockBootGrubDir := filepath.Join(boot.InitramfsUbuntuBootDir, "EFI", "ubuntu")
	mockBootGrubCfg := filepath.Join(mockBootGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockBootGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(mockBootGrubCfg, nil, 0644)
	c.Assert(err, IsNil)

	unpackedGadgetDir := c.MkDir()
	grubRecoveryCfg := []byte("#grub-recovery cfg")
	grubRecoveryCfgAsset := []byte("#grub-recovery cfg from assets")
	grubCfg := []byte("#grub cfg")
	grubCfgAsset := []byte("# Snapd-Boot-Config-Edition: 1\n#grub cfg from assets")
	snaptest.PopulateDir(unpackedGadgetDir, [][]string{
		{"grub-recovery.conf", string(grubRecoveryCfg)},
		{"grub.conf", string(grubCfg)},
		{"bootx64.efi", "shim content"},
		{"grubx64.efi", "grub content"},
		{"meta/snap.yaml", gadgetSnapYaml},
		{"meta/gadget.yaml", gadgetYaml},
	})
	restore = assets.MockInternal("grub-recovery.cfg", grubRecoveryCfgAsset)
	defer restore()
	restore = assets.MockInternal("grub.cfg", grubCfgAsset)
	defer restore()

	// make the snaps symlinks so that we can ensure that makebootable follows
	// the symlinks and copies the files and not the symlinks
	baseFn, baseInfo := makeSnap(c, "core20", `name: core20
type: base
version: 5.0
`, snap.R(3))
	baseInSeed := filepath.Join(seedSnapsDirs, baseInfo.Filename())
	err = os.Symlink(baseFn, baseInSeed)
	c.Assert(err, IsNil)
	gadgetFn, gadgetInfo := makeSnap(c, "pc", `name: pc
type: gadget
version: 5.0
`, snap.R(4))
	gadgetInSeed := filepath.Join(seedSnapsDirs, gadgetInfo.Filename())
	err = os.Symlink(gadgetFn, gadgetInSeed)
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
		RecoverySystemLabel: "20191216",
		BasePath:            baseInSeed,
		Base:                baseInfo,
		Gadget:              gadgetInfo,
		GadgetPath:          gadgetInSeed,
		KernelPath:          kernelInSeed,
		Kernel:              kernelInfo,
		Recovery:            false,
		UnpackedGadgetDir:   unpackedGadgetDir,
	}

	// set up observer state
	useEncryption := true
	obs, err := boot.TrustedAssetsInstallObserverForModel(model, unpackedGadgetDir, useEncryption)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	// only grubx64.efi gets installed to system-boot
	_, err = obs.Observe(gadget.ContentWrite, gadget.SystemBoot, boot.InitramfsUbuntuBootDir, "EFI/boot/grubx64.efi",
		&gadget.ContentChange{After: filepath.Join(unpackedGadgetDir, "grubx64.efi")})
	c.Assert(err, IsNil)

	// observe recovery assets
	err = obs.ObserveExistingTrustedRecoveryAssets(boot.InitramfsUbuntuSeedDir)
	c.Assert(err, IsNil)

	// set key resetter
	dataResetter := &secboot.MockKeyResetter{}
	saveResetter := &secboot.MockKeyResetter{}
	obs.ChosenEncryptionKeys(dataResetter, saveResetter)

	// set a mock recovery kernel
	readSystemEssentialCalls := 0
	restore = boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		if fromInitrd {
			c.Assert(seedDir, Equals, filepath.Join(boot.InitramfsRunMntDir, "ubuntu-seed"))
		} else {
			c.Assert(seedDir, Equals, dirs.SnapSeedDir)
		}
		readSystemEssentialCalls++
		return model, []*seed.Snap{mockKernelSeedSnap(snap.R(1)), mockGadgetSeedSnap(c, nil)}, nil
	})
	defer restore()

	provisionCalls := 0
	restore = boot.MockSecbootProvisionTPM(func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error {
		provisionCalls++
		c.Check(lockoutAuthFile, Equals, filepath.Join(boot.InstallHostFDESaveDir, "tpm-lockout-auth"))
		if factoryReset {
			c.Check(mode, Equals, secboot.TPMPartialReprovision)
		} else {
			c.Check(mode, Equals, secboot.TPMProvisionFull)
		}
		return nil
	})
	defer restore()

	pcrHandleOfKeyCalls := 0
	restore = boot.MockSecbootPCRHandleOfSealedKey(func(p string) (uint32, error) {
		pcrHandleOfKeyCalls++
		c.Check(provisionCalls, Equals, 0)
		if !factoryReset {
			c.Errorf("unexpected call in non-factory-reset scenario")
			return 0, fmt.Errorf("unexpected call")
		}
		c.Check(p, Equals,
			filepath.Join(s.rootdir, "/run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"))
		// trigger use of alt handles as current key is using the main handle
		return secboot.FallbackObjectPCRPolicyCounterHandle, nil
	})
	defer restore()

	releasePCRHandleCalls := 0
	restore = boot.MockSecbootReleasePCRResourceHandles(func(handles ...uint32) error {
		c.Check(factoryReset, Equals, true)
		releasePCRHandleCalls++
		c.Check(handles, DeepEquals, []uint32{
			secboot.AltRunObjectPCRPolicyCounterHandle,
			secboot.AltFallbackObjectPCRPolicyCounterHandle,
		})
		return nil
	})
	defer restore()

	hasFDESetupHookCalled := false
	restore = boot.MockHasFDESetupHook(func(kernel *snap.Info) (bool, error) {
		c.Check(kernel, Equals, kernelInfo)
		hasFDESetupHookCalled = true
		return false, nil
	})
	defer restore()

	// set mock key sealing
	sealKeysCalls := 0
	restore = boot.MockSecbootSealKeys(func(keys []secboot.SealKeyRequest, params *secboot.SealKeysParams) error {
		c.Assert(provisionCalls, Equals, 1, Commentf("TPM must have been provisioned before"))
		sealKeysCalls++
		switch sealKeysCalls {
		case 1:
			c.Check(keys, HasLen, 1)
			c.Check(keys[0].Resetter, Equals, dataResetter)
			if factoryReset {
				c.Check(params.PCRPolicyCounterHandle, Equals, secboot.AltRunObjectPCRPolicyCounterHandle)
			} else {
				c.Check(params.PCRPolicyCounterHandle, Equals, secboot.RunObjectPCRPolicyCounterHandle)
			}
		case 2:
			c.Check(keys, HasLen, 1)
			c.Check(keys[0].Resetter, Equals, saveResetter)
			if factoryReset {
				c.Check(params.PCRPolicyCounterHandle, Equals, secboot.AltFallbackObjectPCRPolicyCounterHandle)

			} else {
				c.Check(params.PCRPolicyCounterHandle, Equals, secboot.FallbackObjectPCRPolicyCounterHandle)
			}
		default:
			c.Errorf("unexpected additional call to secboot.SealKeys (call # %d)", sealKeysCalls)
		}
		c.Assert(params.ModelParams, HasLen, 1)

		shim := bootloader.NewBootFile("", filepath.Join(s.rootdir,
			"var/lib/snapd/boot-assets/grub/bootx64.efi-39efae6545f16e39633fbfbef0d5e9fdd45a25d7df8764978ce4d81f255b038046a38d9855e42e5c7c4024e153fd2e37"),
			bootloader.RoleRecovery)
		grub := bootloader.NewBootFile("", filepath.Join(s.rootdir,
			"var/lib/snapd/boot-assets/grub/grubx64.efi-aa3c1a83e74bf6dd40dd64e5c5bd1971d75cdf55515b23b9eb379f66bf43d4661d22c4b8cf7d7a982d2013ab65c1c4c5"),
			bootloader.RoleRecovery)
		runGrub := bootloader.NewBootFile("", filepath.Join(s.rootdir,
			"var/lib/snapd/boot-assets/grub/grubx64.efi-5ee042c15e104b825d6bc15c41cdb026589f1ec57ed966dd3f29f961d4d6924efc54b187743fa3a583b62722882d405d"),
			bootloader.RoleRunMode)
		kernel := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
		var runKernelPath string
		var runKernel bootloader.BootFile
		switch {
		case !standalone:
			runKernelPath = "/var/lib/snapd/snaps/pc-kernel_5.snap"
		case classic:
			runKernelPath = "/run/mnt/ubuntu-data/var/lib/snapd/snaps/pc-kernel_5.snap"
		case !classic:
			runKernelPath = "/run/mnt/ubuntu-data/system-data/var/lib/snapd/snaps/pc-kernel_5.snap"
		}
		runKernel = bootloader.NewBootFile(filepath.Join(s.rootdir, runKernelPath), "kernel.efi", bootloader.RoleRunMode)
		switch sealKeysCalls {
		case 1:
			c.Assert(params.ModelParams[0].EFILoadChains, DeepEquals, []*secboot.LoadChain{
				secboot.NewLoadChain(shim, secboot.NewLoadChain(grub, secboot.NewLoadChain(kernel))),
				secboot.NewLoadChain(shim, secboot.NewLoadChain(grub, secboot.NewLoadChain(runGrub, secboot.NewLoadChain(runKernel)))),
			})
			c.Assert(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20191216 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=recover snapd_recovery_system=20191216 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
			})
		case 2:
			c.Assert(params.ModelParams[0].EFILoadChains, DeepEquals, []*secboot.LoadChain{
				secboot.NewLoadChain(shim, secboot.NewLoadChain(grub, secboot.NewLoadChain(kernel))),
			})
			c.Assert(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
				"snapd_recovery_mode=factory-reset snapd_recovery_system=20191216 console=ttyS0 console=tty1 panic=-1",
				"snapd_recovery_mode=recover snapd_recovery_system=20191216 console=ttyS0 console=tty1 panic=-1",
			})
		default:
			c.Errorf("unexpected additional call to secboot.SealKeys (call # %d)", sealKeysCalls)
		}

		c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")

		return nil
	})
	defer restore()

	switch {
	case standalone && fromInitrd:
		err = boot.MakeRunnableStandaloneSystemFromInitrd(model, bootWith, obs)
	case standalone && !fromInitrd:
		u := mockUnlocker{}
		err = boot.MakeRunnableStandaloneSystem(model, bootWith, obs, u.unlocker)
		c.Check(u.unlocked, Equals, 1)
	case factoryReset && !fromInitrd:
		err = boot.MakeRunnableSystemAfterDataReset(model, bootWith, obs)
	default:
		err = boot.MakeRunnableSystem(model, bootWith, obs)
	}
	c.Assert(err, IsNil)

	// also do the logical thing and make the next boot go to run mode
	err = boot.EnsureNextBootToRunMode("20191216")
	c.Assert(err, IsNil)

	c.Check(uefiVariableSet, Equals, 1)

	// ensure grub.cfg in boot was installed from internal assets
	c.Check(mockBootGrubCfg, testutil.FileEquals, string(grubCfgAsset))

	var installHostWritableDir string
	if classic {
		installHostWritableDir = filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data")
	} else {
		installHostWritableDir = filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")
	}

	// ensure base/gadget/kernel got copied to /var/lib/snapd/snaps
	core20Snap := filepath.Join(dirs.SnapBlobDirUnder(installHostWritableDir), "core20_3.snap")
	gadgetSnap := filepath.Join(dirs.SnapBlobDirUnder(installHostWritableDir), "pc_4.snap")
	pcKernelSnap := filepath.Join(dirs.SnapBlobDirUnder(installHostWritableDir), "pc-kernel_5.snap")
	c.Check(core20Snap, testutil.FilePresent)
	c.Check(gadgetSnap, testutil.FilePresent)
	c.Check(pcKernelSnap, testutil.FilePresent)
	c.Check(osutil.IsSymlink(core20Snap), Equals, false)
	c.Check(osutil.IsSymlink(pcKernelSnap), Equals, false)

	// ensure the bootvars got updated the right way
	mockSeedGrubenv := filepath.Join(mockSeedGrubDir, "grubenv")
	c.Assert(mockSeedGrubenv, testutil.FilePresent)
	c.Check(mockSeedGrubenv, testutil.FileContains, "snapd_recovery_mode=run")
	c.Check(mockSeedGrubenv, testutil.FileContains, "snapd_good_recovery_systems=20191216")
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
	var ubuntuDataModeEnvPath, classicLine, base string
	if classic {
		base = ""
		ubuntuDataModeEnvPath = filepath.Join(s.rootdir, "/run/mnt/ubuntu-data/var/lib/snapd/modeenv")
		classicLine = "\nclassic=true"
	} else {
		base = "\nbase=core20_3.snap"
		ubuntuDataModeEnvPath = filepath.Join(s.rootdir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/modeenv")
	}
	expectedModeenv := fmt.Sprintf(`mode=run
recovery_system=20191216
current_recovery_systems=20191216
good_recovery_systems=20191216%s
gadget=pc_4.snap
current_kernels=pc-kernel_5.snap
model=my-brand/my-model-uc20%s
grade=dangerous
model_sign_key_id=Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij
current_trusted_boot_assets={"grubx64.efi":["5ee042c15e104b825d6bc15c41cdb026589f1ec57ed966dd3f29f961d4d6924efc54b187743fa3a583b62722882d405d"]}
current_trusted_recovery_boot_assets={"bootx64.efi":["39efae6545f16e39633fbfbef0d5e9fdd45a25d7df8764978ce4d81f255b038046a38d9855e42e5c7c4024e153fd2e37"],"grubx64.efi":["aa3c1a83e74bf6dd40dd64e5c5bd1971d75cdf55515b23b9eb379f66bf43d4661d22c4b8cf7d7a982d2013ab65c1c4c5"]}
current_kernel_command_lines=["snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1"]
`, base, classicLine)
	c.Check(ubuntuDataModeEnvPath, testutil.FileEquals, expectedModeenv)
	copiedGrubBin := filepath.Join(
		dirs.SnapBootAssetsDirUnder(installHostWritableDir),
		"grub",
		"grubx64.efi-5ee042c15e104b825d6bc15c41cdb026589f1ec57ed966dd3f29f961d4d6924efc54b187743fa3a583b62722882d405d",
	)
	copiedRecoveryGrubBin := filepath.Join(
		dirs.SnapBootAssetsDirUnder(installHostWritableDir),
		"grub",
		"grubx64.efi-aa3c1a83e74bf6dd40dd64e5c5bd1971d75cdf55515b23b9eb379f66bf43d4661d22c4b8cf7d7a982d2013ab65c1c4c5",
	)
	copiedRecoveryShimBin := filepath.Join(
		dirs.SnapBootAssetsDirUnder(installHostWritableDir),
		"grub",
		"bootx64.efi-39efae6545f16e39633fbfbef0d5e9fdd45a25d7df8764978ce4d81f255b038046a38d9855e42e5c7c4024e153fd2e37",
	)

	// only one file in the cache under new root
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDirUnder(installHostWritableDir), "grub", "*"), []string{
		copiedRecoveryShimBin,
		copiedGrubBin,
		copiedRecoveryGrubBin,
	})
	// with the right content
	c.Check(copiedGrubBin, testutil.FileEquals, "grub content")
	c.Check(copiedRecoveryGrubBin, testutil.FileEquals, "recovery grub content")
	c.Check(copiedRecoveryShimBin, testutil.FileEquals, "recovery shim content")

	// we checked for fde-setup-hook
	c.Check(hasFDESetupHookCalled, Equals, true)
	// make sure TPM was provisioned
	c.Check(provisionCalls, Equals, 1)
	// make sure SealKey was called for the run object and the fallback object
	c.Check(sealKeysCalls, Equals, 2)
	// PCR handle checks
	if factoryReset {
		c.Check(pcrHandleOfKeyCalls, Equals, 1)
		c.Check(releasePCRHandleCalls, Equals, 1)
	} else {
		c.Check(pcrHandleOfKeyCalls, Equals, 0)
		c.Check(releasePCRHandleCalls, Equals, 0)
	}

	// make sure the marker file for sealed key was created
	c.Check(filepath.Join(installHostWritableDir, "/var/lib/snapd/device/fde/sealed-keys"), testutil.FilePresent)

	// make sure we wrote the boot chains data file
	c.Check(filepath.Join(installHostWritableDir, "/var/lib/snapd/device/fde/boot-chains"), testutil.FilePresent)
}

func (s *makeBootable20Suite) TestMakeSystemRunnable20Install(c *C) {
	const standalone = false
	const factoryReset = false
	const classic = false
	const fromInitrd = false
	s.testMakeSystemRunnable20(c, standalone, factoryReset, classic, fromInitrd)
}

func (s *makeBootable20Suite) TestMakeSystemRunnable20InstallOnClassic(c *C) {
	const standalone = false
	const factoryReset = false
	const classic = true
	const fromInitrd = false
	s.testMakeSystemRunnable20(c, standalone, factoryReset, classic, fromInitrd)
}

func (s *makeBootable20Suite) TestMakeSystemRunnable20FactoryReset(c *C) {
	const standalone = false
	const factoryReset = true
	const classic = false
	const fromInitrd = false
	s.testMakeSystemRunnable20(c, standalone, factoryReset, classic, fromInitrd)
}

func (s *makeBootable20Suite) TestMakeSystemRunnable20FactoryResetOnClassic(c *C) {
	const standalone = false
	const factoryReset = true
	const classic = true
	const fromInitrd = false
	s.testMakeSystemRunnable20(c, standalone, factoryReset, classic, fromInitrd)
}

func (s *makeBootable20Suite) TestMakeSystemRunnable20InstallFromInitrd(c *C) {
	const standalone = true
	const factoryReset = false
	const classic = false
	const fromInitrd = true
	s.testMakeSystemRunnable20(c, standalone, factoryReset, classic, fromInitrd)
}

func (s *makeBootable20Suite) TestMakeRunnableSystem20ModeInstallBootConfigErr(c *C) {
	bootloader.Force(nil)

	model := boottest.MakeMockUC20Model()
	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
	err := os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	// grub on ubuntu-seed
	mockSeedGrubDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI", "ubuntu")
	err = os.MkdirAll(mockSeedGrubDir, 0755)
	c.Assert(err, IsNil)
	// no recovery grub.cfg so that test fails if it ever reaches that point

	// grub on ubuntu-boot
	mockBootGrubDir := filepath.Join(boot.InitramfsUbuntuBootDir, "EFI", "ubuntu")
	mockBootGrubCfg := filepath.Join(mockBootGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockBootGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(mockBootGrubCfg, nil, 0644)
	c.Assert(err, IsNil)

	unpackedGadgetDir := c.MkDir()

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
	gadgetFn, gadgetInfo := makeSnap(c, "pc", `name: pc
type: gadget
version: 5.0
`, snap.R(4))
	gadgetInSeed := filepath.Join(seedSnapsDirs, gadgetInfo.Filename())
	err = os.Symlink(gadgetFn, gadgetInSeed)
	c.Assert(err, IsNil)

	bootWith := &boot.BootableSet{
		RecoverySystemLabel: "20191216",
		BasePath:            baseInSeed,
		Base:                baseInfo,
		KernelPath:          kernelInSeed,
		Kernel:              kernelInfo,
		Gadget:              gadgetInfo,
		GadgetPath:          gadgetInSeed,
		Recovery:            false,
		UnpackedGadgetDir:   unpackedGadgetDir,
	}

	// no grub marker in gadget directory raises an error
	err = boot.MakeRunnableSystem(model, bootWith, nil)
	c.Assert(err, ErrorMatches, "internal error: cannot identify run system bootloader: cannot determine bootloader")

	// set up grub.cfg in gadget
	grubCfg := []byte("#grub cfg")
	err = os.WriteFile(filepath.Join(unpackedGadgetDir, "grub.conf"), grubCfg, 0644)
	c.Assert(err, IsNil)

	// no write access to destination directory
	restore := assets.MockInternal("grub.cfg", nil)
	defer restore()
	err = boot.MakeRunnableSystem(model, bootWith, nil)
	c.Assert(err, ErrorMatches, `cannot install managed bootloader assets: internal error: no boot asset for "grub.cfg"`)
}

func (s *makeBootable20Suite) TestMakeRunnableSystem20RunModeSealKeyErr(c *C) {
	bootloader.Force(nil)

	model := boottest.MakeMockUC20Model()
	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
	err := os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	// grub on ubuntu-seed
	mockSeedGrubDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI", "ubuntu")
	mockSeedGrubCfg := filepath.Join(mockSeedGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockSeedGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(mockSeedGrubCfg, []byte("# Snapd-Boot-Config-Edition: 1\n"), 0644)
	c.Assert(err, IsNil)

	// setup recovery boot assets
	err = os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot"), 0755)
	c.Assert(err, IsNil)
	// SHA3-384: 39efae6545f16e39633fbfbef0d5e9fdd45a25d7df8764978ce4d81f255b038046a38d9855e42e5c7c4024e153fd2e37
	err = os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot/bootx64.efi"),
		[]byte("recovery shim content"), 0644)
	c.Assert(err, IsNil)
	// SHA3-384: aa3c1a83e74bf6dd40dd64e5c5bd1971d75cdf55515b23b9eb379f66bf43d4661d22c4b8cf7d7a982d2013ab65c1c4c5
	err = os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot/grubx64.efi"),
		[]byte("recovery grub content"), 0644)
	c.Assert(err, IsNil)

	// grub on ubuntu-boot
	mockBootGrubDir := filepath.Join(boot.InitramfsUbuntuBootDir, "EFI", "ubuntu")
	mockBootGrubCfg := filepath.Join(mockBootGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockBootGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(mockBootGrubCfg, nil, 0644)
	c.Assert(err, IsNil)

	unpackedGadgetDir := c.MkDir()
	grubRecoveryCfg := []byte("#grub-recovery cfg")
	grubRecoveryCfgAsset := []byte("#grub-recovery cfg from assets")
	grubCfg := []byte("#grub cfg")
	grubCfgAsset := []byte("# Snapd-Boot-Config-Edition: 1\n#grub cfg from assets")
	snaptest.PopulateDir(unpackedGadgetDir, [][]string{
		{"grub-recovery.conf", string(grubRecoveryCfg)},
		{"grub.conf", string(grubCfg)},
		{"bootx64.efi", "shim content"},
		{"grubx64.efi", "grub content"},
		{"meta/snap.yaml", gadgetSnapYaml},
		{"meta/gadget.yaml", gadgetYaml},
	})
	restore := assets.MockInternal("grub-recovery.cfg", grubRecoveryCfgAsset)
	defer restore()
	restore = assets.MockInternal("grub.cfg", grubCfgAsset)
	defer restore()

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
	gadgetFn, gadgetInfo := makeSnap(c, "pc", `name: pc
type: gadget
version: 5.0
`, snap.R(4))
	gadgetInSeed := filepath.Join(seedSnapsDirs, gadgetInfo.Filename())
	err = os.Symlink(gadgetFn, gadgetInSeed)
	c.Assert(err, IsNil)

	bootWith := &boot.BootableSet{
		RecoverySystemLabel: "20191216",
		BasePath:            baseInSeed,
		Base:                baseInfo,
		KernelPath:          kernelInSeed,
		Kernel:              kernelInfo,
		Gadget:              gadgetInfo,
		GadgetPath:          gadgetInSeed,
		Recovery:            false,
		UnpackedGadgetDir:   unpackedGadgetDir,
	}

	// set up observer state
	useEncryption := true
	obs, err := boot.TrustedAssetsInstallObserverForModel(model, unpackedGadgetDir, useEncryption)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	// only grubx64.efi gets installed to system-boot
	_, err = obs.Observe(gadget.ContentWrite, gadget.SystemBoot, boot.InitramfsUbuntuBootDir, "EFI/boot/grubx64.efi",
		&gadget.ContentChange{After: filepath.Join(unpackedGadgetDir, "grubx64.efi")})
	c.Assert(err, IsNil)

	// observe recovery assets
	err = obs.ObserveExistingTrustedRecoveryAssets(boot.InitramfsUbuntuSeedDir)
	c.Assert(err, IsNil)

	// set key resetter
	dataResetter := &secboot.MockKeyResetter{}
	saveResetter := &secboot.MockKeyResetter{}
	obs.ChosenEncryptionKeys(dataResetter, saveResetter)

	// set a mock recovery kernel
	readSystemEssentialCalls := 0
	restore = boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		readSystemEssentialCalls++
		return model, []*seed.Snap{mockKernelSeedSnap(snap.R(1)), mockGadgetSeedSnap(c, nil)}, nil
	})
	defer restore()

	provisionCalls := 0
	restore = boot.MockSecbootProvisionTPM(func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error {
		provisionCalls++
		c.Check(lockoutAuthFile, Equals, filepath.Join(boot.InstallHostFDESaveDir, "tpm-lockout-auth"))
		c.Check(mode, Equals, secboot.TPMProvisionFull)
		return nil
	})
	defer restore()
	// set mock key sealing
	sealKeysCalls := 0
	restore = boot.MockSecbootSealKeys(func(keys []secboot.SealKeyRequest, params *secboot.SealKeysParams) error {
		sealKeysCalls++
		switch sealKeysCalls {
		case 1:
			c.Check(keys, HasLen, 1)
			c.Check(keys[0].Resetter, Equals, dataResetter)
		case 2:
			c.Check(keys, HasLen, 1)
			c.Check(keys[0].Resetter, Equals, saveResetter)
		default:
			c.Errorf("unexpected additional call to secboot.SealKeys (call # %d)", sealKeysCalls)
		}
		c.Assert(params.ModelParams, HasLen, 1)

		shim := bootloader.NewBootFile("", filepath.Join(s.rootdir,
			"var/lib/snapd/boot-assets/grub/bootx64.efi-39efae6545f16e39633fbfbef0d5e9fdd45a25d7df8764978ce4d81f255b038046a38d9855e42e5c7c4024e153fd2e37"),
			bootloader.RoleRecovery)
		grub := bootloader.NewBootFile("", filepath.Join(s.rootdir,
			"var/lib/snapd/boot-assets/grub/grubx64.efi-aa3c1a83e74bf6dd40dd64e5c5bd1971d75cdf55515b23b9eb379f66bf43d4661d22c4b8cf7d7a982d2013ab65c1c4c5"),
			bootloader.RoleRecovery)
		runGrub := bootloader.NewBootFile("", filepath.Join(s.rootdir,
			"var/lib/snapd/boot-assets/grub/grubx64.efi-5ee042c15e104b825d6bc15c41cdb026589f1ec57ed966dd3f29f961d4d6924efc54b187743fa3a583b62722882d405d"),
			bootloader.RoleRunMode)
		kernel := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
		runKernel := bootloader.NewBootFile(filepath.Join(s.rootdir, "var/lib/snapd/snaps/pc-kernel_5.snap"), "kernel.efi", bootloader.RoleRunMode)

		c.Assert(params.ModelParams[0].EFILoadChains, DeepEquals, []*secboot.LoadChain{
			secboot.NewLoadChain(shim, secboot.NewLoadChain(grub, secboot.NewLoadChain(kernel))),
			secboot.NewLoadChain(shim, secboot.NewLoadChain(grub, secboot.NewLoadChain(runGrub, secboot.NewLoadChain(runKernel)))),
		})
		c.Assert(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{
			"snapd_recovery_mode=factory-reset snapd_recovery_system=20191216 console=ttyS0 console=tty1 panic=-1",
			"snapd_recovery_mode=recover snapd_recovery_system=20191216 console=ttyS0 console=tty1 panic=-1",
			"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		})
		c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")

		return fmt.Errorf("seal error")
	})
	defer restore()

	err = boot.MakeRunnableSystem(model, bootWith, obs)
	c.Assert(err, ErrorMatches, "cannot seal the encryption keys: seal error")
	// the TPM was provisioned
	c.Check(provisionCalls, Equals, 1)
}

func (s *makeBootable20Suite) testMakeSystemRunnable20WithCustomKernelArgs(c *C, whichFile, content, errMsg string, cmdlines map[string]string) {
	if cmdlines == nil {
		cmdlines = map[string]string{}
	}
	bootloader.Force(nil)

	uefiVariableSet := 0
	defer boot.MockSetEfiBootVariables(func(description string, assetPath string, optionalData []byte) error {
		uefiVariableSet += 1
		c.Check(description, Equals, "ubuntu")
		return nil
	})()

	model := boottest.MakeMockUC20Model()
	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
	err := os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	// grub on ubuntu-seed
	mockSeedGrubDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI", "ubuntu")
	mockSeedGrubCfg := filepath.Join(mockSeedGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockSeedGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(mockSeedGrubCfg, []byte("# Snapd-Boot-Config-Edition: 1\n"), 0644)
	c.Assert(err, IsNil)
	genv := grubenv.NewEnv(filepath.Join(mockSeedGrubDir, "grubenv"))
	c.Assert(genv.Save(), IsNil)

	// setup recovery boot assets
	err = os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot"), 0755)
	c.Assert(err, IsNil)
	// SHA3-384: 39efae6545f16e39633fbfbef0d5e9fdd45a25d7df8764978ce4d81f255b038046a38d9855e42e5c7c4024e153fd2e37
	err = os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot/bootx64.efi"),
		[]byte("recovery shim content"), 0644)
	c.Assert(err, IsNil)
	// SHA3-384: aa3c1a83e74bf6dd40dd64e5c5bd1971d75cdf55515b23b9eb379f66bf43d4661d22c4b8cf7d7a982d2013ab65c1c4c5
	err = os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot/grubx64.efi"),
		[]byte("recovery grub content"), 0644)
	c.Assert(err, IsNil)

	// grub on ubuntu-boot
	mockBootGrubDir := filepath.Join(boot.InitramfsUbuntuBootDir, "EFI", "ubuntu")
	mockBootGrubCfg := filepath.Join(mockBootGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockBootGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(mockBootGrubCfg, nil, 0644)
	c.Assert(err, IsNil)

	unpackedGadgetDir := c.MkDir()
	grubRecoveryCfg := []byte("#grub-recovery cfg")
	grubRecoveryCfgAsset := []byte("#grub-recovery cfg from assets")
	grubCfg := []byte("#grub cfg")
	grubCfgAsset := []byte("# Snapd-Boot-Config-Edition: 1\n#grub cfg from assets")
	gadgetFiles := [][]string{
		{"grub-recovery.conf", string(grubRecoveryCfg)},
		{"grub.conf", string(grubCfg)},
		{"bootx64.efi", "shim content"},
		{"grubx64.efi", "grub content"},
		{"meta/snap.yaml", gadgetSnapYaml},
		{"meta/gadget.yaml", gadgetYaml},
		{whichFile, content},
	}
	snaptest.PopulateDir(unpackedGadgetDir, gadgetFiles)
	restore := assets.MockInternal("grub-recovery.cfg", grubRecoveryCfgAsset)
	defer restore()
	restore = assets.MockInternal("grub.cfg", grubCfgAsset)
	defer restore()

	// make the snaps symlinks so that we can ensure that makebootable follows
	// the symlinks and copies the files and not the symlinks
	baseFn, baseInfo := makeSnap(c, "core20", `name: core20
type: base
version: 5.0
`, snap.R(3))
	baseInSeed := filepath.Join(seedSnapsDirs, baseInfo.Filename())
	err = os.Symlink(baseFn, baseInSeed)
	c.Assert(err, IsNil)
	gadgetFn, gadgetInfo := makeSnap(c, "pc", `name: pc
type: gadget
version: 5.0
`, snap.R(4))
	gadgetInSeed := filepath.Join(seedSnapsDirs, gadgetInfo.Filename())
	err = os.Symlink(gadgetFn, gadgetInSeed)
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
		RecoverySystemLabel: "20191216",
		BasePath:            baseInSeed,
		Base:                baseInfo,
		Gadget:              gadgetInfo,
		GadgetPath:          gadgetInSeed,
		KernelPath:          kernelInSeed,
		Kernel:              kernelInfo,
		Recovery:            false,
		UnpackedGadgetDir:   unpackedGadgetDir,
	}

	// set up observer state
	useEncryption := true
	obs, err := boot.TrustedAssetsInstallObserverForModel(model, unpackedGadgetDir, useEncryption)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	// only grubx64.efi gets installed to system-boot
	_, err = obs.Observe(gadget.ContentWrite, gadget.SystemBoot, boot.InitramfsUbuntuBootDir, "EFI/boot/grubx64.efi",
		&gadget.ContentChange{After: filepath.Join(unpackedGadgetDir, "grubx64.efi")})
	c.Assert(err, IsNil)

	// observe recovery assets
	err = obs.ObserveExistingTrustedRecoveryAssets(boot.InitramfsUbuntuSeedDir)
	c.Assert(err, IsNil)

	obs.ChosenEncryptionKeys(keys.EncryptionKey{}, keys.EncryptionKey{})

	// set a mock recovery kernel
	readSystemEssentialCalls := 0
	restore = boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		readSystemEssentialCalls++
		return model, []*seed.Snap{mockKernelSeedSnap(snap.R(1)), mockGadgetSeedSnap(c, gadgetFiles)}, nil
	})
	defer restore()

	provisionCalls := 0
	restore = boot.MockSecbootProvisionTPM(func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error {
		provisionCalls++
		c.Check(lockoutAuthFile, Equals, filepath.Join(boot.InstallHostFDESaveDir, "tpm-lockout-auth"))
		c.Check(mode, Equals, secboot.TPMProvisionFull)
		return nil
	})
	defer restore()
	// set mock key sealing
	sealKeysCalls := 0
	restore = boot.MockSecbootSealKeys(func(keys []secboot.SealKeyRequest, params *secboot.SealKeysParams) error {
		sealKeysCalls++
		switch sealKeysCalls {
		case 1, 2:
			// expecting only 2 calls
		default:
			c.Errorf("unexpected additional call to secboot.SealKeys (call # %d)", sealKeysCalls)
		}
		c.Assert(params.ModelParams, HasLen, 1)

		switch sealKeysCalls {
		case 1:
			c.Assert(params.ModelParams[0].KernelCmdlines, HasLen, 3)
			c.Assert(params.ModelParams[0].KernelCmdlines, testutil.Contains, cmdlines["recover"])
			c.Assert(params.ModelParams[0].KernelCmdlines, testutil.Contains, cmdlines["factory-reset"])
			c.Assert(params.ModelParams[0].KernelCmdlines, testutil.Contains, cmdlines["run"])
		case 2:
			c.Assert(params.ModelParams[0].KernelCmdlines, DeepEquals, []string{cmdlines["factory-reset"], cmdlines["recover"]})
		default:
			c.Errorf("unexpected additional call to secboot.SealKeys (call # %d)", sealKeysCalls)
		}

		c.Assert(params.ModelParams[0].Model.Model(), Equals, "my-model-uc20")

		return nil
	})
	defer restore()

	err = boot.MakeRunnableSystem(model, bootWith, obs)
	if errMsg != "" {
		c.Assert(err, ErrorMatches, errMsg)
		return
	}
	c.Assert(err, IsNil)

	// also do the logical thing and make the next boot go to run mode
	err = boot.EnsureNextBootToRunMode("20191216")
	c.Assert(err, IsNil)

	c.Check(uefiVariableSet, Equals, 1)

	// ensure grub.cfg in boot was installed from internal assets
	c.Check(mockBootGrubCfg, testutil.FileEquals, string(grubCfgAsset))

	// ensure the bootvars got updated the right way
	mockSeedGrubenv := filepath.Join(mockSeedGrubDir, "grubenv")
	c.Assert(mockSeedGrubenv, testutil.FilePresent)
	c.Check(mockSeedGrubenv, testutil.FileContains, "snapd_recovery_mode=run")
	c.Check(mockSeedGrubenv, testutil.FileContains, "snapd_good_recovery_systems=20191216")
	mockBootGrubenv := filepath.Join(mockBootGrubDir, "grubenv")
	c.Check(mockBootGrubenv, testutil.FilePresent)
	systemGenv := grubenv.NewEnv(mockBootGrubenv)
	c.Assert(systemGenv.Load(), IsNil)

	switch whichFile {
	case "cmdline.extra":
		blopts := &bootloader.Options{
			Role:        bootloader.RoleRunMode,
			NoSlashBoot: true,
		}
		bl, err := bootloader.Find(boot.InitramfsUbuntuBootDir, blopts)
		c.Assert(err, IsNil)
		tbl, ok := bl.(bootloader.TrustedAssetsBootloader)
		if ok {
			candidate := false
			defaultCmdLine, err := tbl.DefaultCommandLine(candidate)
			c.Assert(err, IsNil)
			c.Check(systemGenv.Get("snapd_extra_cmdline_args"), Equals, "")
			c.Check(systemGenv.Get("snapd_full_cmdline_args"), Equals, strutil.JoinNonEmpty([]string{defaultCmdLine, content}, " "))
		} else {
			c.Check(systemGenv.Get("snapd_extra_cmdline_args"), Equals, content)
			c.Check(systemGenv.Get("snapd_full_cmdline_args"), Equals, "")
		}
	case "cmdline.full":
		c.Check(systemGenv.Get("snapd_extra_cmdline_args"), Equals, "")
		c.Check(systemGenv.Get("snapd_full_cmdline_args"), Equals, content)
	}

	// ensure modeenv looks correct
	ubuntuDataModeEnvPath := filepath.Join(s.rootdir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/modeenv")
	c.Check(ubuntuDataModeEnvPath, testutil.FileEquals, fmt.Sprintf(`mode=run
recovery_system=20191216
current_recovery_systems=20191216
good_recovery_systems=20191216
base=core20_3.snap
gadget=pc_4.snap
current_kernels=pc-kernel_5.snap
model=my-brand/my-model-uc20
grade=dangerous
model_sign_key_id=Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij
current_trusted_boot_assets={"grubx64.efi":["5ee042c15e104b825d6bc15c41cdb026589f1ec57ed966dd3f29f961d4d6924efc54b187743fa3a583b62722882d405d"]}
current_trusted_recovery_boot_assets={"bootx64.efi":["39efae6545f16e39633fbfbef0d5e9fdd45a25d7df8764978ce4d81f255b038046a38d9855e42e5c7c4024e153fd2e37"],"grubx64.efi":["aa3c1a83e74bf6dd40dd64e5c5bd1971d75cdf55515b23b9eb379f66bf43d4661d22c4b8cf7d7a982d2013ab65c1c4c5"]}
current_kernel_command_lines=["%v"]
`, cmdlines["run"]))
	// make sure the TPM was provisioned
	c.Check(provisionCalls, Equals, 1)
	// make sure SealKey was called for the run object and the fallback object
	c.Check(sealKeysCalls, Equals, 2)

	// make sure the marker file for sealed key was created
	c.Check(filepath.Join(dirs.SnapFDEDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), "sealed-keys"), testutil.FilePresent)

	// make sure we wrote the boot chains data file
	c.Check(filepath.Join(dirs.SnapFDEDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), "boot-chains"), testutil.FilePresent)
}

func (s *makeBootable20Suite) TestMakeSystemRunnable20WithCustomKernelExtraArgs(c *C) {
	cmdlines := map[string]string{
		"run":           "snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 foo bar baz",
		"recover":       "snapd_recovery_mode=recover snapd_recovery_system=20191216 console=ttyS0 console=tty1 panic=-1 foo bar baz",
		"factory-reset": "snapd_recovery_mode=factory-reset snapd_recovery_system=20191216 console=ttyS0 console=tty1 panic=-1 foo bar baz",
	}
	s.testMakeSystemRunnable20WithCustomKernelArgs(c, "cmdline.extra", "foo bar baz", "", cmdlines)
}

func (s *makeBootable20Suite) TestMakeSystemRunnable20WithCustomKernelFullArgs(c *C) {
	cmdlines := map[string]string{
		"run":           "snapd_recovery_mode=run foo bar baz",
		"recover":       "snapd_recovery_mode=recover snapd_recovery_system=20191216 foo bar baz",
		"factory-reset": "snapd_recovery_mode=factory-reset snapd_recovery_system=20191216 foo bar baz",
	}
	s.testMakeSystemRunnable20WithCustomKernelArgs(c, "cmdline.full", "foo bar baz", "", cmdlines)
}

func (s *makeBootable20Suite) TestMakeSystemRunnable20WithCustomKernelInvalidArgs(c *C) {
	errMsg := `cannot compose the candidate command line: cannot use kernel command line from gadget: invalid kernel command line in cmdline.extra: disallowed kernel argument "snapd=unhappy"`
	s.testMakeSystemRunnable20WithCustomKernelArgs(c, "cmdline.extra", "foo bar snapd=unhappy", errMsg, nil)
}

func (s *makeBootable20Suite) TestMakeSystemRunnable20UnhappyMarkRecoveryCapable(c *C) {
	bootloader.Force(nil)

	model := boottest.MakeMockUC20Model()
	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
	err := os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	// grub on ubuntu-seed
	mockSeedGrubDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI", "ubuntu")
	mockSeedGrubCfg := filepath.Join(mockSeedGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockSeedGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(mockSeedGrubCfg, []byte("# Snapd-Boot-Config-Edition: 1\n"), 0644)
	c.Assert(err, IsNil)
	// there is no grubenv in ubuntu-seed so loading from it will fail

	// setup recovery boot assets
	err = os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot"), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot/bootx64.efi"),
		[]byte("recovery shim content"), 0644)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot/grubx64.efi"),
		[]byte("recovery grub content"), 0644)
	c.Assert(err, IsNil)

	// grub on ubuntu-boot
	mockBootGrubDir := filepath.Join(boot.InitramfsUbuntuBootDir, "EFI", "ubuntu")
	mockBootGrubCfg := filepath.Join(mockBootGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockBootGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(mockBootGrubCfg, nil, 0644)
	c.Assert(err, IsNil)

	unpackedGadgetDir := c.MkDir()
	grubRecoveryCfg := []byte("#grub-recovery cfg")
	grubRecoveryCfgAsset := []byte("#grub-recovery cfg from assets")
	grubCfg := []byte("#grub cfg")
	grubCfgAsset := []byte("# Snapd-Boot-Config-Edition: 1\n#grub cfg from assets")
	snaptest.PopulateDir(unpackedGadgetDir, [][]string{
		{"grub-recovery.conf", string(grubRecoveryCfg)},
		{"grub.conf", string(grubCfg)},
		{"bootx64.efi", "shim content"},
		{"grubx64.efi", "grub content"},
		{"meta/snap.yaml", gadgetSnapYaml},
		{"meta/gadget.yaml", gadgetYaml},
	})
	restore := assets.MockInternal("grub-recovery.cfg", grubRecoveryCfgAsset)
	defer restore()
	restore = assets.MockInternal("grub.cfg", grubCfgAsset)
	defer restore()

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
	gadgetFn, gadgetInfo := makeSnap(c, "pc", `name: pc
type: gadget
version: 5.0
`, snap.R(4))
	gadgetInSeed := filepath.Join(seedSnapsDirs, gadgetInfo.Filename())
	err = os.Symlink(gadgetFn, gadgetInSeed)
	c.Assert(err, IsNil)

	bootWith := &boot.BootableSet{
		RecoverySystemLabel: "20191216",
		BasePath:            baseInSeed,
		Base:                baseInfo,
		KernelPath:          kernelInSeed,
		Kernel:              kernelInfo,
		Gadget:              gadgetInfo,
		GadgetPath:          gadgetInSeed,
		Recovery:            false,
		UnpackedGadgetDir:   unpackedGadgetDir,
	}

	// set a mock recovery kernel
	readSystemEssentialCalls := 0
	restore = boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		readSystemEssentialCalls++
		return model, []*seed.Snap{mockKernelSeedSnap(snap.R(1)), mockGadgetSeedSnap(c, nil)}, nil
	})
	defer restore()

	err = boot.MakeRunnableSystem(model, bootWith, nil)
	c.Assert(err, ErrorMatches, `cannot record "20191216" as a recovery capable system: open .*/run/mnt/ubuntu-seed/EFI/ubuntu/grubenv: no such file or directory`)

}

func (s *makeBootable20UbootSuite) TestUbootMakeBootableImage20TraditionalUbootenvFails(c *C) {
	bootloader.Force(nil)
	model := boottest.MakeMockUC20Model()

	unpackedGadgetDir := c.MkDir()
	ubootEnv := []byte("#uboot env")
	err := os.WriteFile(filepath.Join(unpackedGadgetDir, "uboot.conf"), ubootEnv, 0644)
	c.Assert(err, IsNil)

	// on uc20 the seed layout if different
	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
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
	err = boot.MakeBootableImage(model, s.rootdir, bootWith, nil)
	c.Assert(err, ErrorMatches, `cannot install bootloader: non-empty uboot.env not supported on UC20\+ yet`)
}

func (s *makeBootable20UbootSuite) TestUbootMakeBootableImage20BootScr(c *C) {
	model := boottest.MakeMockUC20Model()

	unpackedGadgetDir := c.MkDir()
	// the uboot.conf must be empty for this to work/do the right thing
	err := os.WriteFile(filepath.Join(unpackedGadgetDir, "uboot.conf"), nil, 0644)
	c.Assert(err, IsNil)

	// on uc20 the seed layout if different
	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
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

	err = boot.MakeBootableImage(model, s.rootdir, bootWith, nil)
	c.Assert(err, IsNil)

	// since uboot.conf was absent, we won't have installed the uboot.env, as
	// it is expected that the gadget assets would have installed boot.scr
	// instead
	c.Check(filepath.Join(s.rootdir, "uboot.env"), testutil.FileAbsent)

	c.Check(s.bootloader.BootVars, DeepEquals, map[string]string{
		"snapd_recovery_system": label,
		"snapd_recovery_mode":   "install",
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

func (s *makeBootable20UbootSuite) TestUbootMakeBootableImage20BootSelNoHeaderFlagByte(c *C) {
	bootloader.Force(nil)
	model := boottest.MakeMockUC20Model()

	unpackedGadgetDir := c.MkDir()
	// the uboot.conf must be empty for this to work/do the right thing
	err := os.WriteFile(filepath.Join(unpackedGadgetDir, "uboot.conf"), nil, 0644)
	c.Assert(err, IsNil)

	sampleEnv, err := ubootenv.Create(filepath.Join(unpackedGadgetDir, "boot.sel"), 4096, ubootenv.CreateOptions{HeaderFlagByte: false})
	c.Assert(err, IsNil)
	err = sampleEnv.Save()
	c.Assert(err, IsNil)

	// on uc20 the seed layout if different
	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
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

	err = boot.MakeBootableImage(model, s.rootdir, bootWith, nil)
	c.Assert(err, IsNil)

	// since uboot.conf was absent, we won't have installed the uboot.env, as
	// it is expected that the gadget assets would have installed boot.scr
	// instead
	c.Check(filepath.Join(s.rootdir, "uboot.env"), testutil.FileAbsent)

	env, err := ubootenv.Open(filepath.Join(s.rootdir, "/uboot/ubuntu/boot.sel"))
	c.Assert(err, IsNil)

	// Since we have a boot.sel w/o a header flag byte in our gadget,
	// our recovery boot sel also should not have a header flag byte
	c.Check(env.HeaderFlagByte(), Equals, false)
}

func (s *makeBootable20UbootSuite) TestUbootMakeRunnableSystem20RunModeBootSel(c *C) {
	bootloader.Force(nil)

	model := boottest.MakeMockUC20Model()
	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
	err := os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	// uboot on ubuntu-seed
	mockSeedUbootBootSel := filepath.Join(boot.InitramfsUbuntuSeedDir, "uboot/ubuntu/boot.sel")
	err = os.MkdirAll(filepath.Dir(mockSeedUbootBootSel), 0755)
	c.Assert(err, IsNil)
	env, err := ubootenv.Create(mockSeedUbootBootSel, 4096, ubootenv.CreateOptions{HeaderFlagByte: true})
	c.Assert(err, IsNil)
	c.Assert(env.Save(), IsNil)

	// uboot on ubuntu-boot (as if it was installed when creating the partition)
	mockBootUbootBootSel := filepath.Join(boot.InitramfsUbuntuBootDir, "uboot/ubuntu/boot.sel")
	err = os.MkdirAll(filepath.Dir(mockBootUbootBootSel), 0755)
	c.Assert(err, IsNil)
	env, err = ubootenv.Create(mockBootUbootBootSel, 4096, ubootenv.CreateOptions{HeaderFlagByte: true})
	c.Assert(err, IsNil)
	c.Assert(env.Save(), IsNil)

	unpackedGadgetDir := c.MkDir()
	c.Assert(os.WriteFile(filepath.Join(unpackedGadgetDir, "uboot.conf"), nil, 0644), IsNil)

	baseFn, baseInfo := makeSnap(c, "core20", `name: core20
type: base
version: 5.0
`, snap.R(3))
	baseInSeed := filepath.Join(seedSnapsDirs, baseInfo.Filename())
	err = os.Rename(baseFn, baseInSeed)
	c.Assert(err, IsNil)
	gadgetFn, gadgetInfo := makeSnap(c, "pc", `name: pc
type: gadget
version: 5.0
`, snap.R(4))
	gadgetInSeed := filepath.Join(seedSnapsDirs, gadgetInfo.Filename())
	err = os.Symlink(gadgetFn, gadgetInSeed)
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
		RecoverySystemLabel: "20191216",
		BasePath:            baseInSeed,
		Base:                baseInfo,
		Gadget:              gadgetInfo,
		GadgetPath:          gadgetInSeed,
		KernelPath:          kernelInSeed,
		Kernel:              kernelInfo,
		Recovery:            false,
		UnpackedGadgetDir:   unpackedGadgetDir,
	}
	err = boot.MakeRunnableSystem(model, bootWith, nil)
	c.Assert(err, IsNil)

	// also do the logical next thing which is to ensure that the system
	// reboots into run mode
	err = boot.EnsureNextBootToRunMode("20191216")
	c.Assert(err, IsNil)

	// ensure base/kernel got copied to /var/lib/snapd/snaps
	c.Check(filepath.Join(dirs.SnapBlobDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), "core20_3.snap"), testutil.FilePresent)
	c.Check(filepath.Join(dirs.SnapBlobDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")), "arm-kernel_5.snap"), testutil.FilePresent)

	// ensure the bootvars on ubuntu-seed got updated the right way
	mockSeedUbootenv := filepath.Join(boot.InitramfsUbuntuSeedDir, "uboot/ubuntu/boot.sel")
	uenvSeed, err := ubootenv.Open(mockSeedUbootenv)
	c.Assert(err, IsNil)
	c.Assert(uenvSeed.Get("snapd_recovery_mode"), Equals, "run")
	c.Assert(uenvSeed.HeaderFlagByte(), Equals, true)

	// now check ubuntu-boot boot.sel
	mockBootUbootenv := filepath.Join(boot.InitramfsUbuntuBootDir, "uboot/ubuntu/boot.sel")
	uenvBoot, err := ubootenv.Open(mockBootUbootenv)
	c.Assert(err, IsNil)
	c.Assert(uenvBoot.Get("snap_try_kernel"), Equals, "")
	c.Assert(uenvBoot.Get("snap_kernel"), Equals, "arm-kernel_5.snap")
	c.Assert(uenvBoot.Get("kernel_status"), Equals, boot.DefaultStatus)
	c.Assert(uenvBoot.HeaderFlagByte(), Equals, true)

	// check that we have the extracted kernel in the right places, in the
	// old uc16/uc18 location
	for _, file := range kernelSnapFiles {
		fName := file[0]
		c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "uboot/ubuntu/arm-kernel_5.snap", fName), testutil.FilePresent)
	}

	// ensure modeenv looks correct
	ubuntuDataModeEnvPath := filepath.Join(s.rootdir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/modeenv")
	c.Check(ubuntuDataModeEnvPath, testutil.FileEquals, `mode=run
recovery_system=20191216
current_recovery_systems=20191216
good_recovery_systems=20191216
base=core20_3.snap
gadget=pc_4.snap
current_kernels=arm-kernel_5.snap
model=my-brand/my-model-uc20
grade=dangerous
model_sign_key_id=Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij
`)
}

func (s *makeBootable20Suite) TestMakeRecoverySystemBootableAtRuntime20(c *C) {
	bootloader.Force(nil)
	model := boottest.MakeMockUC20Model()

	// on uc20 the seed layout if different
	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
	err := os.MkdirAll(seedSnapsDirs, 0755)
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

	gadgets := map[string]string{}
	for _, rev := range []snap.Revision{snap.R(1), snap.R(5)} {
		gadgetFn, gadgetInfo := makeSnapWithFiles(c, "pc", gadgetSnapYaml, rev, [][]string{
			{"grub.conf", ""},
			{"meta/snap.yaml", gadgetSnapYaml},
			{"cmdline.full", fmt.Sprintf("args from gadget rev %s", rev.String())},
			{"meta/gadget.yaml", gadgetYaml},
		})
		gadgetInSeed := filepath.Join(seedSnapsDirs, gadgetInfo.Filename())
		err = os.Rename(gadgetFn, gadgetInSeed)
		c.Assert(err, IsNil)
		// keep track of the gadgets
		gadgets[rev.String()] = gadgetInSeed
	}

	snaptest.PopulateDir(s.rootdir, [][]string{
		{"EFI/ubuntu/grub.cfg", "this is grub"},
		{"EFI/ubuntu/grubenv", "canary"},
	})

	label := "20191209"
	recoverySystemDir := filepath.Join("/systems", label)
	err = boot.MakeRecoverySystemBootable(model, s.rootdir, recoverySystemDir, &boot.RecoverySystemBootableSet{
		Kernel:     kernelInfo,
		KernelPath: kernelInSeed,
		// use gadget revision 1
		GadgetSnapOrDir: gadgets["1"],
		// like it's called when creating a new recovery system
		PrepareImageTime: false,
	})
	c.Assert(err, IsNil)
	// the recovery partition grubenv was not modified
	c.Check(filepath.Join(s.rootdir, "EFI/ubuntu/grubenv"), testutil.FileEquals, "canary")

	systemGenv := grubenv.NewEnv(filepath.Join(s.rootdir, recoverySystemDir, "grubenv"))
	c.Assert(systemGenv.Load(), IsNil)
	c.Check(systemGenv.Get("snapd_recovery_kernel"), Equals, "/snaps/pc-kernel_5.snap")
	c.Check(systemGenv.Get("snapd_extra_cmdline_args"), Equals, "")
	c.Check(systemGenv.Get("snapd_full_cmdline_args"), Equals, "args from gadget rev 1")

	// create another system under a new label
	newLabel := "20210420"
	newRecoverySystemDir := filepath.Join("/systems", newLabel)
	// with a different gadget revision, but same kernel
	err = boot.MakeRecoverySystemBootable(model, s.rootdir, newRecoverySystemDir, &boot.RecoverySystemBootableSet{
		Kernel:          kernelInfo,
		KernelPath:      kernelInSeed,
		GadgetSnapOrDir: gadgets["5"],
		// like it's called when creating a new recovery system
		PrepareImageTime: false,
	})
	c.Assert(err, IsNil)

	systemGenv = grubenv.NewEnv(filepath.Join(s.rootdir, newRecoverySystemDir, "grubenv"))
	c.Assert(systemGenv.Load(), IsNil)
	c.Check(systemGenv.Get("snapd_recovery_kernel"), Equals, "/snaps/pc-kernel_5.snap")
	c.Check(systemGenv.Get("snapd_extra_cmdline_args"), Equals, "")
	c.Check(systemGenv.Get("snapd_full_cmdline_args"), Equals, "args from gadget rev 5")
}

func (s *makeBootable20Suite) TestMakeBootablePartition(c *C) {
	bootloader.Force(nil)

	unpackedGadgetDir := c.MkDir()
	grubRecoveryCfg := "#grub-recovery cfg"
	grubRecoveryCfgAsset := "#grub-recovery cfg from assets"
	grubCfg := "#grub cfg"
	snaptest.PopulateDir(unpackedGadgetDir, [][]string{
		{"grub-recovery.conf", grubRecoveryCfg},
		{"grub.conf", grubCfg},
		{"meta/snap.yaml", gadgetSnapYaml},
	})
	restore := assets.MockInternal("grub-recovery.cfg", []byte(grubRecoveryCfgAsset))
	defer restore()

	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
	err := os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	gadgetFn, gadgetInfo := makeSnap(c, "pc", `name: pc
type: gadget
version: 5.0
`, snap.R(4))
	gadgetInSeed := filepath.Join(seedSnapsDirs, gadgetInfo.Filename())
	err = os.Symlink(gadgetFn, gadgetInSeed)
	c.Assert(err, IsNil)

	baseFn, baseInfo := makeSnap(c, "core22", `name: core22
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

	bootWith := &boot.BootableSet{
		Base:                baseInfo,
		BasePath:            baseInSeed,
		Kernel:              kernelInfo,
		KernelPath:          kernelInSeed,
		Gadget:              gadgetInfo,
		GadgetPath:          gadgetInSeed,
		RecoverySystemLabel: "",
		UnpackedGadgetDir:   unpackedGadgetDir,
		Recovery:            false,
	}

	opts := &bootloader.Options{
		PrepareImageTime: false,
		// We need the same configuration that a recovery partition,
		// as we will chainload to grub in the boot partition.
		Role: bootloader.RoleRecovery,
	}
	partMntDir := filepath.Join(s.rootdir, "/partition")
	err = os.MkdirAll(partMntDir, 0755)
	c.Assert(err, IsNil)
	err = boot.MakeBootablePartition(partMntDir, opts, bootWith, boot.ModeRun, []string{})
	c.Assert(err, IsNil)

	// ensure we have only grub.cfg and grubenv
	files, err := filepath.Glob(filepath.Join(partMntDir, "EFI/ubuntu/*"))
	c.Assert(err, IsNil)
	c.Check(files, HasLen, 2)
	// and nothing else
	files, err = filepath.Glob(filepath.Join(partMntDir, "EFI/*"))
	c.Assert(err, IsNil)
	c.Check(files, HasLen, 1)
	files, err = filepath.Glob(filepath.Join(partMntDir, "*"))
	c.Assert(err, IsNil)
	c.Check(files, HasLen, 1)
	// check that the recovery bootloader configuration was installed with
	// the correct content
	c.Check(filepath.Join(partMntDir, "EFI/ubuntu/grub.cfg"), testutil.FileEquals, grubRecoveryCfgAsset)

	// ensure the correct recovery system configuration was set
	seedGenv := grubenv.NewEnv(filepath.Join(partMntDir, "EFI/ubuntu/grubenv"))
	c.Assert(seedGenv.Load(), IsNil)
	c.Check(seedGenv.Get("snapd_recovery_system"), Equals, "")
	c.Check(seedGenv.Get("snapd_recovery_mode"), Equals, boot.ModeRun)
	c.Check(seedGenv.Get("snapd_good_recovery_systems"), Equals, "")
}

func (s *makeBootable20Suite) TestMakeRunnableSystemNoGoodRecoverySystems(c *C) {
	bootloader.Force(nil)
	model := boottest.MakeMockUC20Model()
	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
	err := os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	// grub on ubuntu-seed
	mockSeedGrubDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI", "ubuntu")
	mockSeedGrubCfg := filepath.Join(mockSeedGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockSeedGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(mockSeedGrubCfg, []byte("# Snapd-Boot-Config-Edition: 1\n"), 0644)
	c.Assert(err, IsNil)
	genv := grubenv.NewEnv(filepath.Join(mockSeedGrubDir, "grubenv"))
	c.Assert(genv.Save(), IsNil)

	// mock grub so it is detected as the current bootloader
	unpackedGadgetDir := c.MkDir()
	grubRecoveryCfg := []byte("#grub-recovery cfg")
	grubRecoveryCfgAsset := []byte("#grub-recovery cfg from assets")
	grubCfg := []byte("#grub cfg")
	grubCfgAsset := []byte("# Snapd-Boot-Config-Edition: 1\n#grub cfg from assets")
	snaptest.PopulateDir(unpackedGadgetDir, [][]string{
		{"grub-recovery.conf", string(grubRecoveryCfg)},
		{"grub.conf", string(grubCfg)},
		{"bootx64.efi", "shim content"},
		{"grubx64.efi", "grub content"},
		{"meta/snap.yaml", gadgetSnapYaml},
		{"meta/gadget.yaml", gadgetYaml},
	})
	restore := assets.MockInternal("grub-recovery.cfg", grubRecoveryCfgAsset)
	defer restore()
	restore = assets.MockInternal("grub.cfg", grubCfgAsset)
	defer restore()

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
	gadgetFn, gadgetInfo := makeSnap(c, "pc", `name: pc
type: gadget
version: 5.0
`, snap.R(4))
	gadgetInSeed := filepath.Join(seedSnapsDirs, gadgetInfo.Filename())
	err = os.Symlink(gadgetFn, gadgetInSeed)
	c.Assert(err, IsNil)

	bootWith := &boot.BootableSet{
		BasePath:          baseInSeed,
		Base:              baseInfo,
		KernelPath:        kernelInSeed,
		Kernel:            kernelInfo,
		Gadget:            gadgetInfo,
		GadgetPath:        gadgetInSeed,
		Recovery:          false,
		UnpackedGadgetDir: unpackedGadgetDir,
	}

	err = boot.MakeRunnableSystem(model, bootWith, nil)
	c.Assert(err, IsNil)

	// ensure that there are no good recovery systems as RecoverySystemLabel was empty
	mockSeedGrubenv := filepath.Join(mockSeedGrubDir, "grubenv")
	c.Check(mockSeedGrubenv, testutil.FilePresent)
	systemGenv := grubenv.NewEnv(mockSeedGrubenv)
	c.Assert(systemGenv.Load(), IsNil)
	c.Check(systemGenv.Get("snapd_good_recovery_systems"), Equals, "")
}

func (s *makeBootable20Suite) TestMakeRunnableSystemStandaloneSnapsCopy(c *C) {
	bootloader.Force(nil)
	model := boottest.MakeMockUC20Model()
	snapsDirs := filepath.Join(s.rootdir, "/somewhere")
	err := os.MkdirAll(snapsDirs, 0755)
	c.Assert(err, IsNil)

	// grub on ubuntu-seed
	mockSeedGrubDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI", "ubuntu")
	mockSeedGrubCfg := filepath.Join(mockSeedGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockSeedGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(mockSeedGrubCfg, []byte("# Snapd-Boot-Config-Edition: 1\n"), 0644)
	c.Assert(err, IsNil)
	genv := grubenv.NewEnv(filepath.Join(mockSeedGrubDir, "grubenv"))
	c.Assert(genv.Save(), IsNil)

	// mock grub so it is detected as the current bootloader
	unpackedGadgetDir := c.MkDir()
	grubRecoveryCfg := []byte("#grub-recovery cfg")
	grubRecoveryCfgAsset := []byte("#grub-recovery cfg from assets")
	grubCfg := []byte("#grub cfg")
	grubCfgAsset := []byte("# Snapd-Boot-Config-Edition: 1\n#grub cfg from assets")
	snaptest.PopulateDir(unpackedGadgetDir, [][]string{
		{"grub-recovery.conf", string(grubRecoveryCfg)},
		{"grub.conf", string(grubCfg)},
		{"bootx64.efi", "shim content"},
		{"grubx64.efi", "grub content"},
		{"meta/snap.yaml", gadgetSnapYaml},
		{"meta/gadget.yaml", gadgetYaml},
	})
	restore := assets.MockInternal("grub-recovery.cfg", grubRecoveryCfgAsset)
	defer restore()
	restore = assets.MockInternal("grub.cfg", grubCfgAsset)
	defer restore()

	// make the snaps symlinks so that we can ensure that makebootable follows
	// the symlinks and copies the files and not the symlinks
	baseFn, baseInfo := makeSnap(c, "core20", `name: core20
type: base
version: 5.0
`, snap.R(3))
	baseInSeed := filepath.Join(snapsDirs, "core20")
	err = os.Symlink(baseFn, baseInSeed)
	c.Assert(err, IsNil)
	kernelFn, kernelInfo := makeSnapWithFiles(c, "pc-kernel", `name: pc-kernel
type: kernel
version: 4.1
`, snap.R(5),
		[][]string{
			{"kernel.efi", "I'm a kernel.efi"},
		},
	)
	kernelInSeed := filepath.Join(snapsDirs, "pc-kernel_4.1.snap")
	err = os.Symlink(kernelFn, kernelInSeed)
	c.Assert(err, IsNil)
	gadgetFn, gadgetInfo := makeSnap(c, "pc", `name: pc
type: gadget
version: 3.0
`, snap.R(4))
	gadgetInSeed := filepath.Join(snapsDirs, "pc_3.0.snap")
	err = os.Symlink(gadgetFn, gadgetInSeed)
	c.Assert(err, IsNil)

	bootWith := &boot.BootableSet{
		RecoverySystemLabel: "20221004",
		BasePath:            baseInSeed,
		Base:                baseInfo,
		KernelPath:          kernelInSeed,
		Kernel:              kernelInfo,
		Gadget:              gadgetInfo,
		GadgetPath:          gadgetInSeed,
		Recovery:            false,
		UnpackedGadgetDir:   unpackedGadgetDir,
	}

	err = boot.MakeRunnableSystem(model, bootWith, nil)
	c.Assert(err, IsNil)

	installHostWritableDir := filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data")
	// ensure base/gadget/kernel got copied to /var/lib/snapd/snaps
	core20Snap := filepath.Join(dirs.SnapBlobDirUnder(installHostWritableDir), "core20_3.snap")
	gadgetSnap := filepath.Join(dirs.SnapBlobDirUnder(installHostWritableDir), "pc_4.snap")
	pcKernelSnap := filepath.Join(dirs.SnapBlobDirUnder(installHostWritableDir), "pc-kernel_5.snap")
	c.Check(core20Snap, testutil.FilePresent)
	c.Check(gadgetSnap, testutil.FilePresent)
	c.Check(pcKernelSnap, testutil.FilePresent)
	c.Check(osutil.IsSymlink(core20Snap), Equals, false)
	c.Check(osutil.IsSymlink(pcKernelSnap), Equals, false)
	c.Check(osutil.IsSymlink(gadgetSnap), Equals, false)

	// check modeenv
	ubuntuDataModeEnvPath := filepath.Join(s.rootdir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/modeenv")
	expectedModeenv := `mode=run
recovery_system=20221004
current_recovery_systems=20221004
good_recovery_systems=20221004
base=core20_3.snap
gadget=pc_4.snap
current_kernels=pc-kernel_5.snap
model=my-brand/my-model-uc20
grade=dangerous
model_sign_key_id=Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij
current_kernel_command_lines=["snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1"]
`
	c.Check(ubuntuDataModeEnvPath, testutil.FileEquals, expectedModeenv)
}

func (s *makeBootable20Suite) TestMakeStandaloneSystemRunnable20Install(c *C) {
	const standalone = true
	const factoryReset = false
	const classic = false
	const fromInitrd = false
	s.testMakeSystemRunnable20(c, standalone, factoryReset, classic, fromInitrd)
}

func (s *makeBootable20Suite) TestMakeStandaloneSystemRunnable20InstallOnClassic(c *C) {
	const standalone = true
	const factoryReset = false
	const classic = true
	const fromInitrd = false
	s.testMakeSystemRunnable20(c, standalone, factoryReset, classic, fromInitrd)
}

func (s *makeBootable20Suite) testMakeBootableImageOptionalKernelArgs(c *C, model *asserts.Model, options map[string]string, expectedCmdline, errMsg string) {
	bootloader.Force(nil)

	defaults := "defaults:\n  system:\n"
	for k, v := range options {
		defaults += fmt.Sprintf("    %s: %s\n", k, v)
	}

	unpackedGadgetDir := c.MkDir()
	grubCfg := "#grub cfg"
	snaptest.PopulateDir(unpackedGadgetDir, [][]string{
		{"grub.conf", grubCfg},
		{"meta/snap.yaml", gadgetSnapYaml},
		{"meta/gadget.yaml", gadgetYaml + defaults},
	})

	// on uc20 the seed layout is different
	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
	err := os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	baseFn, baseInfo := makeSnap(c, "core22", `name: core22
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

	err = boot.MakeBootableImage(model, s.rootdir, bootWith, nil)
	if errMsg != "" {
		c.Assert(err, ErrorMatches, errMsg)
		return
	}
	c.Assert(err, IsNil)

	// ensure the correct recovery system configuration was set
	seedGenv := grubenv.NewEnv(filepath.Join(s.rootdir, "EFI/ubuntu/grubenv"))
	c.Assert(seedGenv.Load(), IsNil)
	c.Check(seedGenv.Get("snapd_recovery_system"), Equals, label)
	// and kernel command line
	systemGenv := grubenv.NewEnv(filepath.Join(s.rootdir, recoverySystemDir, "grubenv"))
	c.Assert(systemGenv.Load(), IsNil)
	c.Check(systemGenv.Get("snapd_recovery_kernel"), Equals, "/snaps/pc-kernel_5.snap")
	blopts := &bootloader.Options{
		Role: bootloader.RoleRecovery,
	}
	bl, err := bootloader.Find(s.rootdir, blopts)
	c.Assert(err, IsNil)
	tbl, ok := bl.(bootloader.TrustedAssetsBootloader)
	if ok {
		candidate := false
		defaultCmdLine, err := tbl.DefaultCommandLine(candidate)
		c.Assert(err, IsNil)
		c.Check(systemGenv.Get("snapd_full_cmdline_args"), Equals, strutil.JoinNonEmpty([]string{defaultCmdLine, expectedCmdline}, " "))
		c.Check(systemGenv.Get("snapd_extra_cmdline_args"), Equals, "")
	} else {
		c.Check(systemGenv.Get("snapd_extra_cmdline_args"), Equals, expectedCmdline)
		c.Check(systemGenv.Get("snapd_full_cmdline_args"), Equals, "")
	}
}

func (s *makeBootable20Suite) TestMakeBootableImageOptionalKernelArgsHappy(c *C) {
	model := boottest.MakeMockUC20Model()
	const cmdline = "param1=val param2"
	for _, opt := range []string{"system.kernel.cmdline-append", "system.kernel.dangerous-cmdline-append"} {
		options := map[string]string{
			opt: cmdline,
		}
		s.testMakeBootableImageOptionalKernelArgs(c, model, options, cmdline, "")
	}
}

func (s *makeBootable20Suite) TestMakeBootableImageOptionalKernelArgsBothBootOptsSet(c *C) {
	model := boottest.MakeMockUC20Model()
	const cmdline = "param1=val param2"
	const cmdlineDanger = "param3=val param4"
	options := map[string]string{
		"system.kernel.cmdline-append":           cmdline,
		"system.kernel.dangerous-cmdline-append": cmdlineDanger,
	}
	s.testMakeBootableImageOptionalKernelArgs(c, model, options, cmdline+" "+cmdlineDanger, "")
}

func (s *makeBootable20Suite) TestMakeBootableImageOptionalKernelArgsSignedAndDangerous(c *C) {
	model := boottest.MakeMockUC20Model(map[string]interface{}{
		"grade": "signed",
	})
	const cmdline = "param1=val param2"
	options := map[string]string{
		"system.kernel.dangerous-cmdline-append": cmdline,
	}
	// The option is ignored if non-dangerous model
	s.testMakeBootableImageOptionalKernelArgs(c, model, options, "", "")
}
