// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
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

func makeMockModel() *asserts.Model {
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
	return assertstest.FakeAssertion(headers).(*asserts.Model)
}

func (s *makeBootableSuite) TestMakeBootable(c *C) {
	model := makeMockModel()

	grubCfg := []byte("#grub cfg")
	unpackedGadgetDir := c.MkDir()
	err := ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "grub.conf"), grubCfg, 0644)
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

	err = boot.MakeBootable(model, s.rootdir, bootWith, nil)
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

	err = boot.MakeBootable(model, s.rootdir, bootWith, nil)
	c.Assert(err, IsNil)

	// ensure only a single file got copied (the grub.cfg)
	files, err := filepath.Glob(filepath.Join(s.rootdir, "EFI/ubuntu/*"))
	c.Assert(err, IsNil)
	c.Check(files, HasLen, 1)
	// check that the recovery bootloader configuration was installed with
	// the correct content
	c.Check(filepath.Join(s.rootdir, "EFI/ubuntu/grub.cfg"), testutil.FileEquals, grubRecoveryCfgAsset)

	// ensure no /boot was setup
	c.Check(filepath.Join(s.rootdir, "boot"), testutil.FileAbsent)

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
	model := makeMockUC20Model()

	unpackedGadgetDir := c.MkDir()
	grubRecoveryCfg := []byte("#grub-recovery cfg")
	err := ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "grub-recovery.conf"), grubRecoveryCfg, 0644)
	c.Assert(err, IsNil)
	grubCfg := []byte("#grub cfg")
	err = ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "grub.conf"), grubCfg, 0644)
	c.Assert(err, IsNil)

	label := "20191209"
	recoverySystemDir := filepath.Join("/systems", label)
	bootWith := &boot.BootableSet{
		RecoverySystemDir: recoverySystemDir,
		UnpackedGadgetDir: unpackedGadgetDir,
		Recovery:          true,
	}

	err = boot.MakeBootable(model, s.rootdir, bootWith, nil)
	c.Assert(err, ErrorMatches, "internal error: recovery system label unset")
}

func (s *makeBootable20Suite) TestMakeBootable20MultipleRecoverySystemsError(c *C) {
	model := makeMockUC20Model()

	bootWith := &boot.BootableSet{Recovery: true}
	err := os.MkdirAll(filepath.Join(s.rootdir, "systems/20191204"), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(s.rootdir, "systems/20191205"), 0755)
	c.Assert(err, IsNil)

	err = boot.MakeBootable(model, s.rootdir, bootWith, nil)
	c.Assert(err, ErrorMatches, "cannot make multiple recovery systems bootable yet")
}

func (s *makeBootable20Suite) TestMakeBootable20RunMode(c *C) {
	bootloader.Force(nil)

	model := makeMockUC20Model()
	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
	err := os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	// grub on ubuntu-seed
	mockSeedGrubDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI", "ubuntu")
	mockSeedGrubCfg := filepath.Join(mockSeedGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockSeedGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockSeedGrubCfg, []byte("# Snapd-Boot-Config-Edition: 1\n"), 0644)
	c.Assert(err, IsNil)

	// setup recovery boot assets
	err = os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot"), 0755)
	c.Assert(err, IsNil)
	// SHA3-384: 39efae6545f16e39633fbfbef0d5e9fdd45a25d7df8764978ce4d81f255b038046a38d9855e42e5c7c4024e153fd2e37
	err = ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot/bootx64.efi"),
		[]byte("recovery shim content"), 0644)
	c.Assert(err, IsNil)
	// SHA3-384: aa3c1a83e74bf6dd40dd64e5c5bd1971d75cdf55515b23b9eb379f66bf43d4661d22c4b8cf7d7a982d2013ab65c1c4c5
	err = ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot/grubx64.efi"),
		[]byte("recovery grub content"), 0644)
	c.Assert(err, IsNil)

	// grub on ubuntu-boot
	mockBootGrubDir := filepath.Join(boot.InitramfsUbuntuBootDir, "EFI", "ubuntu")
	mockBootGrubCfg := filepath.Join(mockBootGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockBootGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockBootGrubCfg, nil, 0644)
	c.Assert(err, IsNil)

	unpackedGadgetDir := c.MkDir()
	grubRecoveryCfg := []byte("#grub-recovery cfg")
	grubRecoveryCfgAsset := []byte("#grub-recovery cfg from assets")
	err = ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "grub-recovery.conf"), grubRecoveryCfg, 0644)
	c.Assert(err, IsNil)
	restore := assets.MockInternal("grub-recovery.cfg", grubRecoveryCfgAsset)
	defer restore()
	grubCfg := []byte("#grub cfg")
	err = ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "grub.conf"), grubCfg, 0644)
	c.Assert(err, IsNil)
	grubCfgAsset := []byte("# Snapd-Boot-Config-Edition: 1\n#grub cfg from assets")
	restore = assets.MockInternal("grub.cfg", grubCfgAsset)
	defer restore()

	err = ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "bootx64.efi"), []byte("shim content"), 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "grubx64.efi"), []byte("grub content"), 0644)
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
		UnpackedGadgetDir: unpackedGadgetDir,
	}

	// set up observer state
	obs, err := boot.TrustedAssetsInstallObserverForModel(model, unpackedGadgetDir)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)
	runBootStruct := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Role: gadget.SystemBoot,
		},
	}

	// only grubx64.efi gets installed to system-boot
	_, err = obs.Observe(gadget.ContentWrite, runBootStruct, boot.InitramfsUbuntuBootDir, "EFI/boot/grubx64.efi",
		&gadget.ContentChange{After: filepath.Join(unpackedGadgetDir, "grubx64.efi")})
	c.Assert(err, IsNil)

	// observe recovery assets
	err = obs.ObserveExistingTrustedRecoveryAssets(boot.InitramfsUbuntuSeedDir)
	c.Assert(err, IsNil)

	// set encryption key
	myKey := secboot.EncryptionKey{}
	for i := range myKey {
		myKey[i] = byte(i)
	}
	obs.ChosenEncryptionKey(myKey)

	// set a mock recovery kernel
	readSystemEssentialCalls := 0
	restore = boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		readSystemEssentialCalls++
		kernelSnap := &seed.Snap{
			Path: "/var/lib/snapd/seed/snaps/pc-kernel_1.snap",
			SideInfo: &snap.SideInfo{
				Revision: snap.Revision{N: 1},
				RealName: "pc-kernel",
			},
		}
		return model, []*seed.Snap{kernelSnap}, nil
	})
	defer restore()

	// set mock key sealing
	sealKeyCalls := 0
	restore = boot.MockSecbootSealKey(func(key secboot.EncryptionKey, params *secboot.SealKeyParams) error {
		sealKeyCalls++
		c.Check(key, DeepEquals, myKey)
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
			"snapd_recovery_mode=recover snapd_recovery_system=20191216 console=ttyS0 console=tty1 panic=-1",
			"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		})
		c.Assert(params.ModelParams[0].Model.DisplayName(), Equals, "My Model")

		return nil
	})
	defer restore()

	err = boot.MakeBootable(model, s.rootdir, bootWith, obs)
	c.Assert(err, IsNil)

	// ensure grub.cfg in boot was installed from internal assets
	c.Check(mockBootGrubCfg, testutil.FileEquals, string(grubCfgAsset))

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
	ubuntuDataModeEnvPath := filepath.Join(s.rootdir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/modeenv")
	c.Check(ubuntuDataModeEnvPath, testutil.FileEquals, `mode=run
recovery_system=20191216
current_recovery_systems=20191216
base=core20_3.snap
current_kernels=pc-kernel_5.snap
model=my-brand/my-model-uc20
grade=dangerous
current_trusted_boot_assets={"grubx64.efi":["5ee042c15e104b825d6bc15c41cdb026589f1ec57ed966dd3f29f961d4d6924efc54b187743fa3a583b62722882d405d"]}
current_trusted_recovery_boot_assets={"bootx64.efi":["39efae6545f16e39633fbfbef0d5e9fdd45a25d7df8764978ce4d81f255b038046a38d9855e42e5c7c4024e153fd2e37"],"grubx64.efi":["aa3c1a83e74bf6dd40dd64e5c5bd1971d75cdf55515b23b9eb379f66bf43d4661d22c4b8cf7d7a982d2013ab65c1c4c5"]}
`)
	copiedGrubBin := filepath.Join(
		dirs.SnapBootAssetsDirUnder(boot.InstallHostWritableDir),
		"grub",
		"grubx64.efi-5ee042c15e104b825d6bc15c41cdb026589f1ec57ed966dd3f29f961d4d6924efc54b187743fa3a583b62722882d405d",
	)
	copiedRecoveryGrubBin := filepath.Join(
		dirs.SnapBootAssetsDirUnder(boot.InstallHostWritableDir),
		"grub",
		"grubx64.efi-aa3c1a83e74bf6dd40dd64e5c5bd1971d75cdf55515b23b9eb379f66bf43d4661d22c4b8cf7d7a982d2013ab65c1c4c5",
	)
	copiedRecoveryShimBin := filepath.Join(
		dirs.SnapBootAssetsDirUnder(boot.InstallHostWritableDir),
		"grub",
		"bootx64.efi-39efae6545f16e39633fbfbef0d5e9fdd45a25d7df8764978ce4d81f255b038046a38d9855e42e5c7c4024e153fd2e37",
	)

	// only one file in the cache under new root
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDirUnder(boot.InstallHostWritableDir), "grub", "*"), []string{
		copiedRecoveryShimBin,
		copiedGrubBin,
		copiedRecoveryGrubBin,
	})
	// with the right content
	c.Check(copiedGrubBin, testutil.FileEquals, "grub content")
	c.Check(copiedRecoveryGrubBin, testutil.FileEquals, "recovery grub content")
	c.Check(copiedRecoveryShimBin, testutil.FileEquals, "recovery shim content")

	// make sure SealKey was called
	c.Check(sealKeyCalls, Equals, 1)

	// make sure the marker file for sealed key was created
	c.Check(filepath.Join(dirs.SnapFDEDirUnder(boot.InstallHostWritableDir), "sealed-keys"), testutil.FilePresent)

	// make sure we wrote the boot chains data file
	c.Check(filepath.Join(dirs.SnapFDEDirUnder(boot.InstallHostWritableDir), "boot-chains"), testutil.FilePresent)
}

func (s *makeBootable20Suite) TestMakeBootable20RunModeInstallBootConfigErr(c *C) {
	bootloader.Force(nil)

	model := makeMockUC20Model()
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
	err = ioutil.WriteFile(mockBootGrubCfg, nil, 0644)
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

	bootWith := &boot.BootableSet{
		RecoverySystemDir: "20191216",
		BasePath:          baseInSeed,
		Base:              baseInfo,
		KernelPath:        kernelInSeed,
		Kernel:            kernelInfo,
		Recovery:          false,
		UnpackedGadgetDir: unpackedGadgetDir,
	}

	// no grub cfg in gadget directory raises an error
	err = boot.MakeBootable(model, s.rootdir, bootWith, nil)
	c.Assert(err, ErrorMatches, "cannot install boot config with a mismatched gadget")

	// set up grub.cfg in gadget
	grubCfg := []byte("#grub cfg")
	err = ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "grub.conf"), grubCfg, 0644)
	c.Assert(err, IsNil)

	// no write access to destination directory
	restore := assets.MockInternal("grub.cfg", nil)
	defer restore()
	err = boot.MakeBootable(model, s.rootdir, bootWith, nil)
	c.Assert(err, ErrorMatches, `cannot install managed bootloader assets: internal error: no boot asset for "grub.cfg"`)
}

func (s *makeBootable20Suite) TestMakeBootable20RunModeSealKeyErr(c *C) {
	bootloader.Force(nil)

	model := makeMockUC20Model()
	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
	err := os.MkdirAll(seedSnapsDirs, 0755)
	c.Assert(err, IsNil)

	// grub on ubuntu-seed
	mockSeedGrubDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI", "ubuntu")
	mockSeedGrubCfg := filepath.Join(mockSeedGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockSeedGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockSeedGrubCfg, []byte("# Snapd-Boot-Config-Edition: 1\n"), 0644)
	c.Assert(err, IsNil)

	// setup recovery boot assets
	err = os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot"), 0755)
	c.Assert(err, IsNil)
	// SHA3-384: 39efae6545f16e39633fbfbef0d5e9fdd45a25d7df8764978ce4d81f255b038046a38d9855e42e5c7c4024e153fd2e37
	err = ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot/bootx64.efi"),
		[]byte("recovery shim content"), 0644)
	c.Assert(err, IsNil)
	// SHA3-384: aa3c1a83e74bf6dd40dd64e5c5bd1971d75cdf55515b23b9eb379f66bf43d4661d22c4b8cf7d7a982d2013ab65c1c4c5
	err = ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "EFI/boot/grubx64.efi"),
		[]byte("recovery grub content"), 0644)
	c.Assert(err, IsNil)

	// grub on ubuntu-boot
	mockBootGrubDir := filepath.Join(boot.InitramfsUbuntuBootDir, "EFI", "ubuntu")
	mockBootGrubCfg := filepath.Join(mockBootGrubDir, "grub.cfg")
	err = os.MkdirAll(filepath.Dir(mockBootGrubCfg), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockBootGrubCfg, nil, 0644)
	c.Assert(err, IsNil)

	unpackedGadgetDir := c.MkDir()
	grubRecoveryCfg := []byte("#grub-recovery cfg")
	grubRecoveryCfgAsset := []byte("#grub-recovery cfg from assets")
	err = ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "grub-recovery.conf"), grubRecoveryCfg, 0644)
	c.Assert(err, IsNil)
	restore := assets.MockInternal("grub-recovery.cfg", grubRecoveryCfgAsset)
	defer restore()
	grubCfg := []byte("#grub cfg")
	err = ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "grub.conf"), grubCfg, 0644)
	c.Assert(err, IsNil)
	grubCfgAsset := []byte("# Snapd-Boot-Config-Edition: 1\n#grub cfg from assets")
	restore = assets.MockInternal("grub.cfg", grubCfgAsset)
	defer restore()

	err = ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "bootx64.efi"), []byte("shim content"), 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "grubx64.efi"), []byte("grub content"), 0644)
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
		UnpackedGadgetDir: unpackedGadgetDir,
	}

	// set up observer state
	obs, err := boot.TrustedAssetsInstallObserverForModel(model, unpackedGadgetDir)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)
	runBootStruct := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Role: gadget.SystemBoot,
		},
	}

	// only grubx64.efi gets installed to system-boot
	_, err = obs.Observe(gadget.ContentWrite, runBootStruct, boot.InitramfsUbuntuBootDir, "EFI/boot/grubx64.efi",
		&gadget.ContentChange{After: filepath.Join(unpackedGadgetDir, "grubx64.efi")})
	c.Assert(err, IsNil)

	// observe recovery assets
	err = obs.ObserveExistingTrustedRecoveryAssets(boot.InitramfsUbuntuSeedDir)
	c.Assert(err, IsNil)

	// set encryption key
	myKey := secboot.EncryptionKey{}
	for i := range myKey {
		myKey[i] = byte(i)
	}
	obs.ChosenEncryptionKey(myKey)

	// set a mock recovery kernel
	readSystemEssentialCalls := 0
	restore = boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		readSystemEssentialCalls++
		kernelSnap := &seed.Snap{
			Path: "/var/lib/snapd/seed/snaps/pc-kernel_1.snap",
			SideInfo: &snap.SideInfo{
				Revision: snap.Revision{N: 0},
				RealName: "pc-kernel",
			},
		}
		return model, []*seed.Snap{kernelSnap}, nil
	})
	defer restore()

	// set mock key sealing
	sealKeyCalls := 0
	restore = boot.MockSecbootSealKey(func(key secboot.EncryptionKey, params *secboot.SealKeyParams) error {
		sealKeyCalls++
		c.Check(key, DeepEquals, myKey)
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
			"snapd_recovery_mode=recover snapd_recovery_system=20191216 console=ttyS0 console=tty1 panic=-1",
			"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		})
		c.Assert(params.ModelParams[0].Model.DisplayName(), Equals, "My Model")

		return fmt.Errorf("seal error")
	})
	defer restore()

	err = boot.MakeBootable(model, s.rootdir, bootWith, obs)
	c.Assert(err, ErrorMatches, "cannot seal the encryption key: seal error")
}

func (s *makeBootable20UbootSuite) TestUbootMakeBootable20TraditionalUbootenvFails(c *C) {
	model := makeMockUC20Model()

	unpackedGadgetDir := c.MkDir()
	ubootEnv := []byte("#uboot env")
	err := ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "uboot.conf"), ubootEnv, 0644)
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
	err = boot.MakeBootable(model, s.rootdir, bootWith, nil)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot find boot config in %q", unpackedGadgetDir))
}

func (s *makeBootable20UbootSuite) TestUbootMakeBootable20BootScr(c *C) {
	model := makeMockUC20Model()

	unpackedGadgetDir := c.MkDir()
	// the uboot.conf must be empty for this to work/do the right thing
	err := ioutil.WriteFile(filepath.Join(unpackedGadgetDir, "uboot.conf"), nil, 0644)
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

	err = boot.MakeBootable(model, s.rootdir, bootWith, nil)
	c.Assert(err, IsNil)

	// since uboot.conf was absent, we won't have installed the uboot.env, as
	// it is expected that the gadget assets would have installed boot.scr
	// instead
	c.Check(filepath.Join(s.rootdir, "uboot.env"), testutil.FileAbsent)

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
	bootloader.Force(nil)

	model := makeMockUC20Model()
	seedSnapsDirs := filepath.Join(s.rootdir, "/snaps")
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

	err = boot.MakeBootable(model, s.rootdir, bootWith, nil)
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
	ubuntuDataModeEnvPath := filepath.Join(s.rootdir, "/run/mnt/ubuntu-data/system-data/var/lib/snapd/modeenv")
	c.Check(ubuntuDataModeEnvPath, testutil.FileEquals, `mode=run
recovery_system=20191216
current_recovery_systems=20191216
base=core20_3.snap
current_kernels=arm-kernel_5.snap
model=my-brand/my-model-uc20
grade=dangerous
`)
}
