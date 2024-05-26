// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2024 Canonical Ltd
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
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/mvo5/goconfigparser"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/arch/archtest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/assets"
	"github.com/snapcore/snapd/bootloader/grubenv"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
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
	// By default assume amd64 in the tests: there are specialized
	// tests for other arches
	s.AddCleanup(archtest.MockArchitecture("amd64"))
	snippets := []assets.ForEditions{
		{FirstEdition: 1, Snippet: []byte("console=ttyS0 console=tty1 panic=-1")},
	}
	s.AddCleanup(assets.MockSnippetsForEdition("grub.cfg:static-cmdline", snippets))
	s.AddCleanup(assets.MockSnippetsForEdition("grub-recovery.cfg:static-cmdline", snippets))
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

	output := mylog.Check2(exec.Command(grubEditenvCmd(), grubEnvPath(s.rootdir), "set", fmt.Sprintf("%s=%s", key, value)).CombinedOutput())
	c.Check(err, IsNil)
	c.Check(string(output), Equals, "")
}

func (s *grubTestSuite) grubEditenvGet(c *C, key string) string {
	if grubEditenvCmd() == "" {
		c.Skip("grub{,2}-editenv is not available")
	}

	output := mylog.Check2(exec.Command(grubEditenvCmd(), grubEnvPath(s.rootdir), "list").CombinedOutput())

	cfg := goconfigparser.New()
	cfg.AllowNoSectionHeader = true
	mylog.Check(cfg.ReadString(string(output)))

	v := mylog.Check2(cfg.Get("", key))

	return v
}

func (s *grubTestSuite) makeFakeGrubEnv(c *C) {
	s.grubEditenvSet(c, "k", "v")
}

func (s *grubTestSuite) TestNewGrub(c *C) {
	// no files means bl is not present, but we can still create the bl object
	c.Assert(os.RemoveAll(s.rootdir), IsNil)
	g := bootloader.NewGrub(s.rootdir, nil)
	c.Assert(g, NotNil)
	c.Assert(g.Name(), Equals, "grub")

	present := mylog.Check2(g.Present())

	c.Assert(present, Equals, false)

	// now with files present, the bl is present
	bootloader.MockGrubFiles(c, s.rootdir)
	s.makeFakeGrubEnv(c)
	present = mylog.Check2(g.Present())

	c.Assert(present, Equals, true)
}

func (s *grubTestSuite) TestGetBootloaderWithGrub(c *C) {
	s.makeFakeGrubEnv(c)

	bootloader := mylog.Check2(bootloader.Find(s.rootdir, nil))

	c.Assert(bootloader.Name(), Equals, "grub")
}

func (s *grubTestSuite) TestGetBootloaderWithGrubWithDefaultRoot(c *C) {
	s.makeFakeGrubEnv(c)

	dirs.SetRootDir(s.rootdir)
	defer func() { dirs.SetRootDir("") }()

	bootloader := mylog.Check2(bootloader.Find("", nil))

	c.Assert(bootloader.Name(), Equals, "grub")
}

func (s *grubTestSuite) TestGetBootVer(c *C) {
	s.makeFakeGrubEnv(c)
	s.grubEditenvSet(c, "snap_mode", "regular")

	g := bootloader.NewGrub(s.rootdir, nil)
	v := mylog.Check2(g.GetBootVars("snap_mode"))

	c.Check(v, HasLen, 1)
	c.Check(v["snap_mode"], Equals, "regular")
}

func (s *grubTestSuite) TestSetBootVer(c *C) {
	s.makeFakeGrubEnv(c)

	g := bootloader.NewGrub(s.rootdir, nil)
	mylog.Check(g.SetBootVars(map[string]string{
		"k1": "v1",
		"k2": "v2",
	}))


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
	snapf := mylog.Check2(snapfile.Open(fn))


	info := mylog.Check2(snap.ReadInfoFromSnapFile(snapf, si))

	mylog.Check(g.ExtractKernelAssets(info, snapf))


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
	snapf := mylog.Check2(snapfile.Open(fn))


	info := mylog.Check2(snap.ReadInfoFromSnapFile(snapf, si))

	mylog.Check(g.ExtractKernelAssets(info, snapf))


	// kernel is extracted
	kernimg := filepath.Join(s.bootdir, "grub", "ubuntu-kernel_42.snap", "kernel.img")
	c.Assert(osutil.FileExists(kernimg), Equals, true)
	// initrd
	initrdimg := filepath.Join(s.bootdir, "grub", "ubuntu-kernel_42.snap", "initrd.img")
	c.Assert(osutil.FileExists(initrdimg), Equals, true)
	mylog.

		// ensure that removal of assets also works
		Check(g.RemoveKernelAssets(info))

	exists, _ := mylog.Check3(osutil.DirExists(filepath.Dir(kernimg)))

	c.Check(exists, Equals, false)
}

func (s *grubTestSuite) grubDir() string {
	return filepath.Join(s.bootdir, "grub")
}

func (s *grubTestSuite) grubEFINativeDir() string {
	return filepath.Join(s.rootdir, "EFI/ubuntu")
}

func (s *grubTestSuite) makeFakeGrubEFINativeEnv(c *C, content []byte) {
	mylog.Check(os.MkdirAll(s.grubEFINativeDir(), 0755))

	mylog.Check(os.WriteFile(filepath.Join(s.grubEFINativeDir(), "grub.cfg"), content, 0644))

}

func (s *grubTestSuite) makeFakeShimFallback(c *C) {
	mylog.Check(os.MkdirAll(filepath.Join(s.rootdir, "/EFI/boot"), 0755))

	_ = mylog.Check2(os.Create(filepath.Join(s.rootdir, "/EFI/boot/fbx64.efi")))

	_ = mylog.Check2(os.Create(filepath.Join(s.rootdir, "/EFI/boot/fbaa64.efi")))

}

func (s *grubTestSuite) TestNewGrubWithOptionRecovery(c *C) {
	s.makeFakeGrubEFINativeEnv(c, nil)

	g := bootloader.NewGrub(s.rootdir, &bootloader.Options{Role: bootloader.RoleRecovery})
	c.Assert(g, NotNil)
	c.Assert(g.Name(), Equals, "grub")
}

func (s *grubTestSuite) TestNewGrubWithOptionRecoveryBootEnv(c *C) {
	s.makeFakeGrubEFINativeEnv(c, nil)
	g := bootloader.NewGrub(s.rootdir, &bootloader.Options{Role: bootloader.RoleRecovery})

	// check that setting vars goes to the right place
	c.Check(filepath.Join(s.grubEFINativeDir(), "grubenv"), testutil.FileAbsent)
	mylog.Check(g.SetBootVars(map[string]string{
		"k1": "v1",
		"k2": "v2",
	}))

	c.Check(filepath.Join(s.grubEFINativeDir(), "grubenv"), testutil.FilePresent)

	env := mylog.Check2(g.GetBootVars("k1", "k2"))

	c.Check(env, DeepEquals, map[string]string{
		"k1": "v1",
		"k2": "v2",
	})
}

func (s *grubTestSuite) TestNewGrubWithOptionRecoveryNoEnv(c *C) {
	// fake a *regular* grub env
	s.makeFakeGrubEnv(c)

	// we can't create a recovery grub with that
	g := mylog.Check2(bootloader.Find(s.rootdir, &bootloader.Options{Role: bootloader.RoleRecovery}))
	c.Assert(g, IsNil)
	c.Assert(err, Equals, bootloader.ErrBootloader)
}

func (s *grubTestSuite) TestGrubSetRecoverySystemEnv(c *C) {
	s.makeFakeGrubEFINativeEnv(c, nil)
	g := bootloader.NewGrub(s.rootdir, &bootloader.Options{Role: bootloader.RoleRecovery})

	// check that we can set a recovery system specific bootenv
	bvars := map[string]string{
		"snapd_recovery_kernel": "/snaps/pc-kernel_1.snap",
		"other_options":         "are-supported",
	}
	mylog.Check(g.SetRecoverySystemEnv("/systems/20191209", bvars))

	recoverySystemGrubenv := filepath.Join(s.rootdir, "/systems/20191209/grubenv")
	c.Assert(recoverySystemGrubenv, testutil.FilePresent)

	genv := grubenv.NewEnv(recoverySystemGrubenv)
	mylog.Check(genv.Load())

	c.Check(genv.Get("snapd_recovery_kernel"), Equals, "/snaps/pc-kernel_1.snap")
	c.Check(genv.Get("other_options"), Equals, "are-supported")
}

func (s *grubTestSuite) TestGetRecoverySystemEnv(c *C) {
	s.makeFakeGrubEFINativeEnv(c, nil)
	g := bootloader.NewGrub(s.rootdir, &bootloader.Options{Role: bootloader.RoleRecovery})
	mylog.Check(os.MkdirAll(filepath.Join(s.rootdir, "/systems/20191209"), 0755))

	recoverySystemGrubenv := filepath.Join(s.rootdir, "/systems/20191209/grubenv")

	// does not fail when there is no recovery env
	value := mylog.Check2(g.GetRecoverySystemEnv("/systems/20191209", "no_file"))

	c.Check(value, Equals, "")

	genv := grubenv.NewEnv(recoverySystemGrubenv)
	genv.Set("snapd_extra_cmdline_args", "foo bar baz")
	genv.Set("random_option", `has "some spaces"`)
	mylog.Check(genv.Save())


	value = mylog.Check2(g.GetRecoverySystemEnv("/systems/20191209", "snapd_extra_cmdline_args"))

	c.Check(value, Equals, "foo bar baz")
	value = mylog.Check2(g.GetRecoverySystemEnv("/systems/20191209", "random_option"))

	c.Check(value, Equals, `has "some spaces"`)
	value = mylog.Check2(g.GetRecoverySystemEnv("/systems/20191209", "not_set"))

	c.Check(value, Equals, ``)
}

func (s *grubTestSuite) makeKernelAssetSnap(c *C, snapFileName string) snap.PlaceInfo {
	kernelSnap := mylog.Check2(snap.ParsePlaceInfoFromSnapFileName(snapFileName))


	// make a kernel.efi snap as it would be by ExtractKernelAssets()
	kernelSnapExtractedAssetsDir := filepath.Join(s.grubDir(), snapFileName)
	mylog.Check(os.MkdirAll(kernelSnapExtractedAssetsDir, 0755))

	mylog.Check(os.WriteFile(filepath.Join(kernelSnapExtractedAssetsDir, "kernel.efi"), nil, 0644))


	return kernelSnap
}

func (s *grubTestSuite) makeKernelAssetSnapAndSymlink(c *C, snapFileName, symlinkName string) snap.PlaceInfo {
	kernelSnap := s.makeKernelAssetSnap(c, snapFileName)
	mylog.

		// make a kernel.efi symlink to the kernel.efi above
		Check(os.Symlink(
			filepath.Join(snapFileName, "kernel.efi"),
			filepath.Join(s.grubDir(), symlinkName),
		))


	return kernelSnap
}

func (s *grubTestSuite) TestGrubExtractedRunKernelImageKernel(c *C) {
	s.makeFakeGrubEnv(c)
	g := bootloader.NewGrub(s.rootdir, nil)
	eg, ok := g.(bootloader.ExtractedRunKernelImageBootloader)
	c.Assert(ok, Equals, true)

	kernel := s.makeKernelAssetSnapAndSymlink(c, "pc-kernel_1.snap", "kernel.efi")

	// ensure that the returned kernel is the same as the one we put there
	sn := mylog.Check2(eg.Kernel())

	c.Assert(sn, DeepEquals, kernel)
}

func (s *grubTestSuite) TestGrubExtractedRunKernelImageTryKernel(c *C) {
	s.makeFakeGrubEnv(c)
	g := bootloader.NewGrub(s.rootdir, nil)
	eg, ok := g.(bootloader.ExtractedRunKernelImageBootloader)
	c.Assert(ok, Equals, true)

	// ensure it doesn't return anything when the symlink doesn't exist
	_ := mylog.Check2(eg.TryKernel())
	c.Assert(err, Equals, bootloader.ErrNoTryKernelRef)

	// when a bad kernel snap name is in the extracted path, it will complain
	// appropriately
	kernelSnapExtractedAssetsDir := filepath.Join(s.grubDir(), "bad_snap_rev_name")
	badKernelSnapPath := filepath.Join(kernelSnapExtractedAssetsDir, "kernel.efi")
	tryKernelSymlink := filepath.Join(s.grubDir(), "try-kernel.efi")
	mylog.Check(os.MkdirAll(kernelSnapExtractedAssetsDir, 0755))

	mylog.Check(os.WriteFile(badKernelSnapPath, nil, 0644))

	mylog.Check(os.Symlink("bad_snap_rev_name/kernel.efi", tryKernelSymlink))


	_ = mylog.Check2(eg.TryKernel())
	c.Assert(err, ErrorMatches, "cannot parse kernel snap file name from symlink target \"bad_snap_rev_name\": .*")
	mylog.

		// remove the bad symlink
		Check(os.Remove(tryKernelSymlink))


	// make a real symlink
	tryKernel := s.makeKernelAssetSnapAndSymlink(c, "pc-kernel_2.snap", "try-kernel.efi")

	// ensure that the returned kernel is the same as the one we put there
	sn := mylog.Check2(eg.TryKernel())

	c.Assert(sn, DeepEquals, tryKernel)
	mylog.

		// if the destination of the symlink is removed, we get an error
		Check(os.Remove(filepath.Join(s.grubDir(), "pc-kernel_2.snap", "kernel.efi")))

	_ = mylog.Check2(eg.TryKernel())
	c.Assert(err, ErrorMatches, "cannot read dangling symlink try-kernel.efi")
}

func (s *grubTestSuite) TestGrubExtractedRunKernelImageEnableKernel(c *C) {
	s.makeFakeGrubEnv(c)
	g := bootloader.NewGrub(s.rootdir, nil)
	eg, ok := g.(bootloader.ExtractedRunKernelImageBootloader)
	c.Assert(ok, Equals, true)

	// ensure we fail to create a dangling symlink to a kernel snap that was not
	// actually extracted
	nonExistSnap := mylog.Check2(snap.ParsePlaceInfoFromSnapFileName("pc-kernel_12.snap"))

	mylog.Check(eg.EnableKernel(nonExistSnap))
	c.Assert(err, ErrorMatches, "cannot enable kernel.efi at pc-kernel_12.snap/kernel.efi: file does not exist")

	kernel := s.makeKernelAssetSnap(c, "pc-kernel_1.snap")
	mylog.

		// enable the Kernel we extracted
		Check(eg.EnableKernel(kernel))


	// ensure that the symlink was put where we expect it
	asset := mylog.Check2(os.Readlink(filepath.Join(s.grubDir(), "kernel.efi")))

	c.Assert(asset, DeepEquals, filepath.Join("pc-kernel_1.snap", "kernel.efi"))

	// create a new kernel snap and ensure that we can safely enable that one
	// too
	kernel2 := s.makeKernelAssetSnap(c, "pc-kernel_2.snap")
	mylog.Check(eg.EnableKernel(kernel2))


	// ensure that the symlink was put where we expect it
	asset = mylog.Check2(os.Readlink(filepath.Join(s.grubDir(), "kernel.efi")))

	c.Assert(asset, DeepEquals, filepath.Join("pc-kernel_2.snap", "kernel.efi"))
}

func (s *grubTestSuite) TestGrubExtractedRunKernelImageEnableTryKernel(c *C) {
	s.makeFakeGrubEnv(c)
	g := bootloader.NewGrub(s.rootdir, nil)
	eg, ok := g.(bootloader.ExtractedRunKernelImageBootloader)
	c.Assert(ok, Equals, true)

	kernel := s.makeKernelAssetSnap(c, "pc-kernel_1.snap")
	mylog.

		// enable the Kernel we extracted
		Check(eg.EnableTryKernel(kernel))


	// ensure that the symlink was put where we expect it
	asset := mylog.Check2(os.Readlink(filepath.Join(s.grubDir(), "try-kernel.efi")))


	c.Assert(asset, DeepEquals, filepath.Join("pc-kernel_1.snap", "kernel.efi"))
}

func (s *grubTestSuite) TestGrubExtractedRunKernelImageDisableTryKernel(c *C) {
	if os.Geteuid() == 0 {
		c.Skip("the test cannot be run by the root user")
	}

	s.makeFakeGrubEnv(c)
	g := bootloader.NewGrub(s.rootdir, nil)
	eg, ok := g.(bootloader.ExtractedRunKernelImageBootloader)
	c.Assert(ok, Equals, true)
	mylog.

		// trying to disable when the try-kernel.efi symlink is missing does not
		// raise any errors
		Check(eg.DisableTryKernel())


	// make the symlink and check that the symlink is missing afterwards
	s.makeKernelAssetSnapAndSymlink(c, "pc-kernel_1.snap", "try-kernel.efi")
	// make sure symlink is there
	c.Assert(filepath.Join(s.grubDir(), "try-kernel.efi"), testutil.FilePresent)
	mylog.Check(eg.DisableTryKernel())


	// ensure that the symlink is no longer there
	c.Assert(filepath.Join(s.grubDir(), "try-kernel.efi"), testutil.FileAbsent)
	c.Assert(filepath.Join(s.grubDir(), "pc-kernel_1.snap/kernel.efi"), testutil.FilePresent)

	// try again but make sure that the directory cannot be written to
	s.makeKernelAssetSnapAndSymlink(c, "pc-kernel_1.snap", "try-kernel.efi")
	mylog.Check(os.Chmod(s.grubDir(), 000))

	defer os.Chmod(s.grubDir(), 0755)
	mylog.Check(eg.DisableTryKernel())
	c.Assert(err, ErrorMatches, "remove .*/grub/try-kernel.efi: permission denied")
}

func (s *grubTestSuite) TestKernelExtractionRunImageKernel(c *C) {
	s.makeFakeGrubEnv(c)

	g := bootloader.NewGrub(s.rootdir, &bootloader.Options{Role: bootloader.RoleRunMode})
	c.Assert(g, NotNil)

	files := [][]string{
		{"kernel.efi", "I'm a kernel"},
		{"another-kernel-file", "another kernel file"},
		{"meta/kernel.yaml", "version: 4.2"},
	}
	si := &snap.SideInfo{
		RealName: "ubuntu-kernel",
		Revision: snap.R(42),
	}
	fn := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)
	snapf := mylog.Check2(snapfile.Open(fn))


	info := mylog.Check2(snap.ReadInfoFromSnapFile(snapf, si))

	mylog.Check(g.ExtractKernelAssets(info, snapf))


	// kernel is extracted
	kernefi := filepath.Join(s.bootdir, "grub", "ubuntu-kernel_42.snap", "kernel.efi")
	c.Assert(kernefi, testutil.FilePresent)
	// other file is not extracted
	other := filepath.Join(s.bootdir, "grub", "ubuntu-kernel_42.snap", "another-kernel-file")
	c.Assert(other, testutil.FileAbsent)
	mylog.

		// ensure that removal of assets also works
		Check(g.RemoveKernelAssets(info))

	exists, _ := mylog.Check3(osutil.DirExists(filepath.Dir(kernefi)))

	c.Check(exists, Equals, false)
}

func (s *grubTestSuite) TestKernelExtractionRunImageKernelNoSlashBoot(c *C) {
	// this is ubuntu-boot but during install we use the native EFI/ubuntu
	// layout, same as Recovery, without the /boot mount
	s.makeFakeGrubEFINativeEnv(c, nil)

	g := bootloader.NewGrub(s.rootdir, &bootloader.Options{Role: bootloader.RoleRunMode, NoSlashBoot: true})
	c.Assert(g, NotNil)

	files := [][]string{
		{"kernel.efi", "I'm a kernel"},
		{"another-kernel-file", "another kernel file"},
		{"meta/kernel.yaml", "version: 4.2"},
	}
	si := &snap.SideInfo{
		RealName: "ubuntu-kernel",
		Revision: snap.R(42),
	}
	fn := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)
	snapf := mylog.Check2(snapfile.Open(fn))


	info := mylog.Check2(snap.ReadInfoFromSnapFile(snapf, si))

	mylog.Check(g.ExtractKernelAssets(info, snapf))


	// kernel is extracted
	kernefi := filepath.Join(s.rootdir, "EFI/ubuntu", "ubuntu-kernel_42.snap", "kernel.efi")
	c.Assert(kernefi, testutil.FilePresent)
	// other file is not extracted
	other := filepath.Join(s.rootdir, "EFI/ubuntu", "ubuntu-kernel_42.snap", "another-kernel-file")
	c.Assert(other, testutil.FileAbsent)

	// enable the Kernel we extracted
	eg, ok := g.(bootloader.ExtractedRunKernelImageBootloader)
	c.Assert(ok, Equals, true)
	mylog.Check(eg.EnableKernel(info))


	// ensure that the symlink was put where we expect it
	asset := mylog.Check2(os.Readlink(filepath.Join(s.rootdir, "EFI/ubuntu", "kernel.efi")))


	c.Assert(asset, DeepEquals, filepath.Join("ubuntu-kernel_42.snap", "kernel.efi"))
	mylog.

		// ensure that removal of assets also works
		Check(g.RemoveKernelAssets(info))

	exists, _ := mylog.Check3(osutil.DirExists(filepath.Dir(kernefi)))

	c.Check(exists, Equals, false)
}

func (s *grubTestSuite) TestListTrustedAssetsNotForArch(c *C) {
	oldArch := arch.DpkgArchitecture()
	defer arch.SetArchitecture(arch.ArchitectureType(oldArch))
	arch.SetArchitecture("non-existing-architecture")

	s.makeFakeGrubEFINativeEnv(c, []byte(`this is
some random boot config`))

	opts := &bootloader.Options{NoSlashBoot: true}
	g := bootloader.NewGrub(s.rootdir, opts)
	c.Assert(g, NotNil)

	tg, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)

	ta := mylog.Check2(tg.TrustedAssets())
	c.Check(err, ErrorMatches, `cannot find grub assets for "non-existing-architecture"`)
	c.Check(ta, HasLen, 0)
}

func (s *grubTestSuite) TestListManagedAssets(c *C) {
	s.makeFakeGrubEFINativeEnv(c, []byte(`this is
some random boot config`))

	opts := &bootloader.Options{NoSlashBoot: true}
	g := bootloader.NewGrub(s.rootdir, opts)
	c.Assert(g, NotNil)

	tg, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)

	c.Check(tg.ManagedAssets(), DeepEquals, []string{
		"EFI/ubuntu/grub.cfg",
	})

	opts = &bootloader.Options{Role: bootloader.RoleRecovery}
	tg = bootloader.NewGrub(s.rootdir, opts).(bootloader.TrustedAssetsBootloader)
	c.Check(tg.ManagedAssets(), DeepEquals, []string{
		"EFI/ubuntu/grub.cfg",
	})

	// as it called for the root fs rather than a mount point of a partition
	// with boot assets
	tg = bootloader.NewGrub(s.rootdir, nil).(bootloader.TrustedAssetsBootloader)
	c.Check(tg.ManagedAssets(), DeepEquals, []string{
		"boot/grub/grub.cfg",
	})
}

func (s *grubTestSuite) TestRecoveryUpdateBootConfigNoEdition(c *C) {
	// native EFI/ubuntu setup
	s.makeFakeGrubEFINativeEnv(c, []byte("recovery boot script"))

	opts := &bootloader.Options{Role: bootloader.RoleRecovery}
	g := bootloader.NewGrub(s.rootdir, opts)
	c.Assert(g, NotNil)

	restore := assets.MockInternal("grub-recovery.cfg", []byte(`# Snapd-Boot-Config-Edition: 5
this is mocked grub-recovery.conf
`))
	defer restore()

	tg, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)
	// install the recovery boot script
	updated := mylog.Check2(tg.UpdateBootConfig())

	c.Assert(updated, Equals, false)
	c.Assert(filepath.Join(s.grubEFINativeDir(), "grub.cfg"), testutil.FileEquals, `recovery boot script`)
}

func (s *grubTestSuite) TestRecoveryUpdateBootConfigUpdates(c *C) {
	// native EFI/ubuntu setup
	s.makeFakeGrubEFINativeEnv(c, []byte(`# Snapd-Boot-Config-Edition: 1
recovery boot script`))

	opts := &bootloader.Options{Role: bootloader.RoleRecovery}
	g := bootloader.NewGrub(s.rootdir, opts)
	c.Assert(g, NotNil)

	restore := assets.MockInternal("grub-recovery.cfg", []byte(`# Snapd-Boot-Config-Edition: 3
this is mocked grub-recovery.conf
`))
	defer restore()
	restore = assets.MockInternal("grub.cfg", []byte(`# Snapd-Boot-Config-Edition: 4
this is mocked grub.conf
`))
	defer restore()
	tg, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)
	// install the recovery boot script
	updated := mylog.Check2(tg.UpdateBootConfig())

	c.Assert(updated, Equals, true)
	// the recovery boot asset was picked
	c.Assert(filepath.Join(s.grubEFINativeDir(), "grub.cfg"), testutil.FileEquals, `# Snapd-Boot-Config-Edition: 3
this is mocked grub-recovery.conf
`)
}

func (s *grubTestSuite) testBootUpdateBootConfigUpdates(c *C, oldConfig, newConfig string, update bool) {
	// native EFI/ubuntu setup
	s.makeFakeGrubEFINativeEnv(c, []byte(oldConfig))

	opts := &bootloader.Options{NoSlashBoot: true}
	g := bootloader.NewGrub(s.rootdir, opts)
	c.Assert(g, NotNil)

	restore := assets.MockInternal("grub.cfg", []byte(newConfig))
	defer restore()

	tg, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)
	updated := mylog.Check2(tg.UpdateBootConfig())

	c.Assert(updated, Equals, update)
	if update {
		c.Assert(filepath.Join(s.grubEFINativeDir(), "grub.cfg"), testutil.FileEquals, newConfig)
	} else {
		c.Assert(filepath.Join(s.grubEFINativeDir(), "grub.cfg"), testutil.FileEquals, oldConfig)
	}
}

func (s *grubTestSuite) TestNoSlashBootUpdateBootConfigNoUpdateWhenNotManaged(c *C) {
	oldConfig := `not managed`
	newConfig := `# Snapd-Boot-Config-Edition: 3
this update is not applied
`
	// the current boot config is not managed, no update applied
	const updateApplied = false
	s.testBootUpdateBootConfigUpdates(c, oldConfig, newConfig, updateApplied)
}

func (s *grubTestSuite) TestNoSlashBootUpdateBootConfigUpdates(c *C) {
	oldConfig := `# Snapd-Boot-Config-Edition: 2
boot script
`
	// edition is higher, update is applied
	newConfig := `# Snapd-Boot-Config-Edition: 3
this is updated grub.cfg
`
	const updateApplied = true
	s.testBootUpdateBootConfigUpdates(c, oldConfig, newConfig, updateApplied)
}

func (s *grubTestSuite) TestNoSlashBootUpdateBootConfigNoUpdate(c *C) {
	oldConfig := `# Snapd-Boot-Config-Edition: 2
boot script
`
	// edition is lower, no update is applied
	newConfig := `# Snapd-Boot-Config-Edition: 1
this is updated grub.cfg
`
	const updateApplied = false
	s.testBootUpdateBootConfigUpdates(c, oldConfig, newConfig, updateApplied)
}

func (s *grubTestSuite) TestNoSlashBootUpdateBootConfigSameEdition(c *C) {
	oldConfig := `# Snapd-Boot-Config-Edition: 1
boot script
`
	// edition is equal, no update is applied
	newConfig := `# Snapd-Boot-Config-Edition: 1
this is updated grub.cfg
`
	const updateApplied = false
	s.testBootUpdateBootConfigUpdates(c, oldConfig, newConfig, updateApplied)
}

func (s *grubTestSuite) TestBootUpdateBootConfigTrivialErr(c *C) {
	if os.Geteuid() == 0 {
		c.Skip("the test cannot be run by the root user")
	}

	oldConfig := `# Snapd-Boot-Config-Edition: 2
boot script
`
	// edition is higher, update is applied
	newConfig := `# Snapd-Boot-Config-Edition: 3
this is updated grub.cfg
`
	// native EFI/ubuntu setup
	s.makeFakeGrubEFINativeEnv(c, []byte(oldConfig))
	restore := assets.MockInternal("grub.cfg", []byte(newConfig))
	defer restore()

	opts := &bootloader.Options{NoSlashBoot: true}
	g := bootloader.NewGrub(s.rootdir, opts)
	c.Assert(g, NotNil)
	tg, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)
	mylog.Check(os.Chmod(s.grubEFINativeDir(), 0000))

	defer os.Chmod(s.grubEFINativeDir(), 0755)

	updated := mylog.Check2(tg.UpdateBootConfig())
	c.Assert(err, ErrorMatches, "cannot load existing config asset: .*/EFI/ubuntu/grub.cfg: permission denied")
	c.Assert(updated, Equals, false)
	mylog.Check(os.Chmod(s.grubEFINativeDir(), 0555))


	c.Assert(filepath.Join(s.grubEFINativeDir(), "grub.cfg"), testutil.FileEquals, oldConfig)
	mylog.

		// writing out new config fails
		Check(os.Chmod(s.grubEFINativeDir(), 0111))

	updated = mylog.Check2(tg.UpdateBootConfig())
	c.Assert(err, ErrorMatches, `open .*/EFI/ubuntu/grub.cfg\..+: permission denied`)
	c.Assert(updated, Equals, false)
	c.Assert(filepath.Join(s.grubEFINativeDir(), "grub.cfg"), testutil.FileEquals, oldConfig)
}

func (s *grubTestSuite) TestStaticCmdlineForGrubAsset(c *C) {
	restore := assets.MockSnippetsForEdition("grub-asset:static-cmdline", []assets.ForEditions{
		{FirstEdition: 2, Snippet: []byte(`static cmdline "with spaces"`)},
	})
	defer restore()
	cmdline := bootloader.StaticCommandLineForGrubAssetEdition("grub-asset", 1)
	c.Check(cmdline, Equals, ``)
	cmdline = bootloader.StaticCommandLineForGrubAssetEdition("grub-asset", 2)
	c.Check(cmdline, Equals, `static cmdline "with spaces"`)
	cmdline = bootloader.StaticCommandLineForGrubAssetEdition("grub-asset", 4)
	c.Check(cmdline, Equals, `static cmdline "with spaces"`)
}

func (s *grubTestSuite) TestCommandLineNotManaged(c *C) {
	grubCfg := "boot script\n"

	// native EFI/ubuntu setup
	s.makeFakeGrubEFINativeEnv(c, []byte(grubCfg))

	restore := assets.MockSnippetsForEdition("grub.cfg:static-cmdline", []assets.ForEditions{
		{FirstEdition: 1, Snippet: []byte(`static=1`)},
		{FirstEdition: 2, Snippet: []byte(`static=2`)},
	})
	defer restore()
	restore = assets.MockSnippetsForEdition("grub-recovery.cfg:static-cmdline", []assets.ForEditions{
		{FirstEdition: 1, Snippet: []byte(`static=1 recovery`)},
		{FirstEdition: 2, Snippet: []byte(`static=2 recovery`)},
	})
	defer restore()

	opts := &bootloader.Options{NoSlashBoot: true}
	mg := bootloader.NewGrub(s.rootdir, opts).(bootloader.TrustedAssetsBootloader)

	args := mylog.Check2(mg.CommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=run",
		ExtraArgs: "extra",
	}))

	c.Check(args, Equals, "snapd_recovery_mode=run static=1 extra")

	optsRecovery := &bootloader.Options{NoSlashBoot: true, Role: bootloader.RoleRecovery}
	mgr := bootloader.NewGrub(s.rootdir, optsRecovery).(bootloader.TrustedAssetsBootloader)

	args = mylog.Check2(mgr.CommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=recover",
		SystemArg: "snapd_recovery_system=1234",
		ExtraArgs: "extra",
	}))

	c.Check(args, Equals, "snapd_recovery_mode=recover snapd_recovery_system=1234 static=1 recovery extra")
}

func (s *grubTestSuite) TestCommandLineMocked(c *C) {
	grubCfg := `# Snapd-Boot-Config-Edition: 2
boot script
`
	staticCmdline := `arg1   foo=123 panic=-1 arg2="with spaces "`
	staticCmdlineEdition3 := `edition=3 static args`
	restore := assets.MockSnippetsForEdition("grub.cfg:static-cmdline", []assets.ForEditions{
		{FirstEdition: 1, Snippet: []byte(staticCmdline)},
		{FirstEdition: 3, Snippet: []byte(staticCmdlineEdition3)},
	})
	defer restore()
	staticCmdlineRecovery := `recovery config panic=-1`
	restore = assets.MockSnippetsForEdition("grub-recovery.cfg:static-cmdline", []assets.ForEditions{
		{FirstEdition: 1, Snippet: []byte(staticCmdlineRecovery)},
	})
	defer restore()

	// native EFI/ubuntu setup
	s.makeFakeGrubEFINativeEnv(c, []byte(grubCfg))

	optsNoSlashBoot := &bootloader.Options{NoSlashBoot: true}
	g := bootloader.NewGrub(s.rootdir, optsNoSlashBoot)
	c.Assert(g, NotNil)
	tg, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)

	extraArgs := `extra_arg=1  extra_foo=-1   panic=3 baz="more  spaces"`
	args := mylog.Check2(tg.CommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=run",
		ExtraArgs: extraArgs,
	}))

	c.Check(args, Equals, `snapd_recovery_mode=run arg1 foo=123 panic=-1 arg2="with spaces " extra_arg=1 extra_foo=-1 panic=3 baz="more  spaces"`)

	// empty mode/system do not produce confusing results
	args = mylog.Check2(tg.CommandLine(bootloader.CommandLineComponents{
		ExtraArgs: extraArgs,
	}))

	c.Check(args, Equals, `arg1 foo=123 panic=-1 arg2="with spaces " extra_arg=1 extra_foo=-1 panic=3 baz="more  spaces"`)

	// now check the recovery bootloader
	optsRecovery := &bootloader.Options{NoSlashBoot: true, Role: bootloader.RoleRecovery}
	mrg := bootloader.NewGrub(s.rootdir, optsRecovery).(bootloader.TrustedAssetsBootloader)
	args = mylog.Check2(mrg.CommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=recover",
		SystemArg: "snapd_recovery_system=20200202",
		ExtraArgs: extraArgs,
	}))

	// static command line from recovery asset
	c.Check(args, Equals, `snapd_recovery_mode=recover snapd_recovery_system=20200202 recovery config panic=-1 extra_arg=1 extra_foo=-1 panic=3 baz="more  spaces"`)

	// try with a different edition
	grubCfg3 := `# Snapd-Boot-Config-Edition: 3
boot script
`
	s.makeFakeGrubEFINativeEnv(c, []byte(grubCfg3))
	tg = bootloader.NewGrub(s.rootdir, optsNoSlashBoot).(bootloader.TrustedAssetsBootloader)
	c.Assert(g, NotNil)
	extraArgs = `extra_arg=1`
	args = mylog.Check2(tg.CommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=run",
		ExtraArgs: extraArgs,
	}))

	c.Check(args, Equals, `snapd_recovery_mode=run edition=3 static args extra_arg=1`)

	// full args set overrides static arguments
	args = mylog.Check2(tg.CommandLine(bootloader.CommandLineComponents{
		ModeArg:  "snapd_recovery_mode=run",
		FullArgs: "full for run mode",
	}))

	c.Check(args, Equals, `snapd_recovery_mode=run full for run mode`)
	args = mylog.Check2(mrg.CommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=recover",
		SystemArg: "snapd_recovery_system=20200202",
		FullArgs:  "full for recover mode",
	}))

	c.Check(args, Equals, `snapd_recovery_mode=recover snapd_recovery_system=20200202 full for recover mode`)
}

func (s *grubTestSuite) TestCandidateCommandLineMocked(c *C) {
	grubCfg := `# Snapd-Boot-Config-Edition: 1
boot script
`
	// edition on disk
	s.makeFakeGrubEFINativeEnv(c, []byte(grubCfg))

	edition2 := []byte(`# Snapd-Boot-Config-Edition: 2`)
	edition3 := []byte(`# Snapd-Boot-Config-Edition: 3`)
	edition4 := []byte(`# Snapd-Boot-Config-Edition: 4`)

	restore := assets.MockInternal("grub.cfg", edition2)
	defer restore()
	restore = assets.MockInternal("grub-recovery.cfg", edition2)
	defer restore()

	restore = assets.MockSnippetsForEdition("grub.cfg:static-cmdline", []assets.ForEditions{
		{FirstEdition: 1, Snippet: []byte(`edition=1`)},
		{FirstEdition: 3, Snippet: []byte(`edition=3`)},
	})
	defer restore()
	restore = assets.MockSnippetsForEdition("grub-recovery.cfg:static-cmdline", []assets.ForEditions{
		{FirstEdition: 1, Snippet: []byte(`recovery edition=1`)},
		{FirstEdition: 3, Snippet: []byte(`recovery edition=3`)},
		{FirstEdition: 4, Snippet: []byte(`recovery edition=4up`)},
	})
	defer restore()

	optsNoSlashBoot := &bootloader.Options{NoSlashBoot: true}
	mg := bootloader.NewGrub(s.rootdir, optsNoSlashBoot).(bootloader.TrustedAssetsBootloader)
	optsRecovery := &bootloader.Options{NoSlashBoot: true, Role: bootloader.RoleRecovery}
	recoverymg := bootloader.NewGrub(s.rootdir, optsRecovery).(bootloader.TrustedAssetsBootloader)

	args := mylog.Check2(mg.CandidateCommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=run",
		ExtraArgs: "extra=1",
	}))

	c.Check(args, Equals, `snapd_recovery_mode=run edition=1 extra=1`)
	args = mylog.Check2(recoverymg.CandidateCommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=recover",
		SystemArg: "snapd_recovery_system=20200202",
		ExtraArgs: "extra=1",
	}))

	c.Check(args, Equals, `snapd_recovery_mode=recover snapd_recovery_system=20200202 recovery edition=1 extra=1`)

	restore = assets.MockInternal("grub.cfg", edition3)
	defer restore()
	restore = assets.MockInternal("grub-recovery.cfg", edition3)
	defer restore()

	args = mylog.Check2(mg.CandidateCommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=run",
		ExtraArgs: "extra=1",
	}))

	c.Check(args, Equals, `snapd_recovery_mode=run edition=3 extra=1`)
	args = mylog.Check2(recoverymg.CandidateCommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=recover",
		SystemArg: "snapd_recovery_system=20200202",
		ExtraArgs: "extra=1",
	}))

	c.Check(args, Equals, `snapd_recovery_mode=recover snapd_recovery_system=20200202 recovery edition=3 extra=1`)

	// bump edition only for recovery
	restore = assets.MockInternal("grub-recovery.cfg", edition4)
	defer restore()
	// boot bootloader unchanged
	args = mylog.Check2(mg.CandidateCommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=run",
		ExtraArgs: "extra=1",
	}))

	c.Check(args, Equals, `snapd_recovery_mode=run edition=3 extra=1`)
	// recovery uses a new edition
	args = mylog.Check2(recoverymg.CandidateCommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=recover",
		SystemArg: "snapd_recovery_system=20200202",
		ExtraArgs: "extra=1",
	}))

	c.Check(args, Equals, `snapd_recovery_mode=recover snapd_recovery_system=20200202 recovery edition=4up extra=1`)

	// the static snippet is ignored when using full arg set
	args = mylog.Check2(recoverymg.CandidateCommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=recover",
		SystemArg: "snapd_recovery_system=20200202",
		FullArgs:  "full args set",
	}))

	c.Check(args, Equals, `snapd_recovery_mode=recover snapd_recovery_system=20200202 full args set`)
}

func (s *grubTestSuite) TestCommandLineReal(c *C) {
	grubCfg := `# Snapd-Boot-Config-Edition: 1
boot script
`
	// native EFI/ubuntu setup
	s.makeFakeGrubEFINativeEnv(c, []byte(grubCfg))

	opts := &bootloader.Options{NoSlashBoot: true}
	g := bootloader.NewGrub(s.rootdir, opts)
	c.Assert(g, NotNil)
	tg, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)

	extraArgs := "foo bar baz=1"
	args := mylog.Check2(tg.CommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=run",
		ExtraArgs: extraArgs,
	}))

	c.Check(args, Equals, `snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 foo bar baz=1`)
	// with full args the static part is not used
	args = mylog.Check2(tg.CommandLine(bootloader.CommandLineComponents{
		ModeArg:  "snapd_recovery_mode=run",
		FullArgs: "full for run mode",
	}))

	c.Check(args, Equals, `snapd_recovery_mode=run full for run mode`)

	// now check the recovery bootloader
	opts = &bootloader.Options{NoSlashBoot: true, Role: bootloader.RoleRecovery}
	mrg := bootloader.NewGrub(s.rootdir, opts).(bootloader.TrustedAssetsBootloader)
	args = mylog.Check2(mrg.CommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=recover",
		SystemArg: "snapd_recovery_system=20200202",
		ExtraArgs: extraArgs,
	}))

	// static command line from recovery asset
	c.Check(args, Equals, `snapd_recovery_mode=recover snapd_recovery_system=20200202 console=ttyS0 console=tty1 panic=-1 foo bar baz=1`)
	// similarly, when passed full args, the static part is not used
	args = mylog.Check2(mrg.CommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=recover",
		SystemArg: "snapd_recovery_system=20200202",
		FullArgs:  "full for recover mode",
	}))

	c.Check(args, Equals, `snapd_recovery_mode=recover snapd_recovery_system=20200202 full for recover mode`)
}

func (s *grubTestSuite) TestCommandLineComponentsValidate(c *C) {
	grubCfg := `# Snapd-Boot-Config-Edition: 1
boot script
`
	// native EFI/ubuntu setup
	s.makeFakeGrubEFINativeEnv(c, []byte(grubCfg))

	opts := &bootloader.Options{NoSlashBoot: true}
	g := bootloader.NewGrub(s.rootdir, opts)
	c.Assert(g, NotNil)
	tg, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)

	args := mylog.Check2(tg.CommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=run",
		ExtraArgs: "extra is set",
		FullArgs:  "full is set",
	}))
	c.Assert(err, ErrorMatches, "cannot use both full and extra components of command line")
	c.Check(args, Equals, "")
	// invalid for the candidate command line too
	args = mylog.Check2(tg.CandidateCommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=run",
		ExtraArgs: "extra is set",
		FullArgs:  "full is set",
	}))
	c.Assert(err, ErrorMatches, "cannot use both full and extra components of command line")
	c.Check(args, Equals, "")

	// now check the recovery bootloader
	opts = &bootloader.Options{NoSlashBoot: true, Role: bootloader.RoleRecovery}
	mrg := bootloader.NewGrub(s.rootdir, opts).(bootloader.TrustedAssetsBootloader)
	args = mylog.Check2(mrg.CommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=recover",
		SystemArg: "snapd_recovery_system=20200202",
		ExtraArgs: "extra is set",
		FullArgs:  "full is set",
	}))
	c.Assert(err, ErrorMatches, "cannot use both full and extra components of command line")
	c.Check(args, Equals, "")
	// candidate recovery command line is checks validity of the components too
	args = mylog.Check2(mrg.CandidateCommandLine(bootloader.CommandLineComponents{
		ModeArg:   "snapd_recovery_mode=recover",
		SystemArg: "snapd_recovery_system=20200202",
		ExtraArgs: "extra is set",
		FullArgs:  "full is set",
	}))
	c.Assert(err, ErrorMatches, "cannot use both full and extra components of command line")
	c.Check(args, Equals, "")
}

func (s *grubTestSuite) TestTrustedAssetsNativePartitionLayout(c *C) {
	// native EFI/ubuntu setup
	s.makeFakeGrubEFINativeEnv(c, []byte("grub.cfg"))
	opts := &bootloader.Options{NoSlashBoot: true}
	g := bootloader.NewGrub(s.rootdir, opts)
	c.Assert(g, NotNil)

	tab, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)

	ta := mylog.Check2(tab.TrustedAssets())

	c.Check(ta, DeepEquals, map[string]string{
		"EFI/boot/grubx64.efi": "grubx64.efi",
	})

	// recovery bootloader
	recoveryOpts := &bootloader.Options{NoSlashBoot: true, Role: bootloader.RoleRecovery}
	tarb := bootloader.NewGrub(s.rootdir, recoveryOpts).(bootloader.TrustedAssetsBootloader)
	c.Assert(tarb, NotNil)

	ta = mylog.Check2(tarb.TrustedAssets())

	c.Check(ta, DeepEquals, map[string]string{
		"EFI/boot/bootx64.efi":   "bootx64.efi",
		"EFI/boot/grubx64.efi":   "grubx64.efi",
		"EFI/ubuntu/shimx64.efi": "ubuntu:shimx64.efi",
		"EFI/ubuntu/grubx64.efi": "ubuntu:grubx64.efi",
	})

	// recovery bootloader, with fallback implemented
	s.makeFakeShimFallback(c)
	tarb = bootloader.NewGrub(s.rootdir, recoveryOpts).(bootloader.TrustedAssetsBootloader)
	c.Assert(tarb, NotNil)

	ta = mylog.Check2(tarb.TrustedAssets())

	c.Check(ta, DeepEquals, map[string]string{
		"EFI/ubuntu/shimx64.efi": "ubuntu:shimx64.efi",
		"EFI/ubuntu/grubx64.efi": "ubuntu:grubx64.efi",
		"EFI/boot/bootx64.efi":   "bootx64.efi",
		"EFI/boot/grubx64.efi":   "grubx64.efi",
	})
}

func (s *grubTestSuite) TestTrustedAssetsRoot(c *C) {
	s.makeFakeGrubEnv(c)
	g := bootloader.NewGrub(s.rootdir, nil)
	tab, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)

	ta := mylog.Check2(tab.TrustedAssets())
	c.Assert(err, ErrorMatches, "internal error: trusted assets called without native host-partition layout")
	c.Check(ta, IsNil)
}

func (s *grubTestSuite) TestTrustedAssetsFailAtPrepareImageTime(c *C) {
	// native EFI/ubuntu setup
	s.makeFakeGrubEFINativeEnv(c, []byte("grub.cfg"))

	opts := []bootloader.Options{
		{NoSlashBoot: true, PrepareImageTime: true},
		{NoSlashBoot: true, PrepareImageTime: true, Role: bootloader.RoleRecovery},
	}
	for _, opt := range opts {
		g := bootloader.NewGrub(s.rootdir, &opt)
		c.Assert(g, NotNil)

		tab, ok := g.(bootloader.TrustedAssetsBootloader)
		c.Assert(ok, Equals, true)

		ta := mylog.Check2(tab.TrustedAssets())
		c.Assert(err, ErrorMatches, "internal error: retrieving boot assets at prepare image time")
		c.Check(ta, IsNil)
	}
}

func (s *grubTestSuite) TestRecoveryBootChains(c *C) {
	s.makeFakeGrubEFINativeEnv(c, nil)
	g := bootloader.NewGrub(s.rootdir, &bootloader.Options{Role: bootloader.RoleRecovery})
	tab, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)

	chains := mylog.Check2(tab.RecoveryBootChains("kernel.snap"))

	c.Assert(chains, HasLen, 2)
	c.Assert(chains[0], DeepEquals, []bootloader.BootFile{
		{Path: "EFI/ubuntu/shimx64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/ubuntu/grubx64.efi", Role: bootloader.RoleRecovery},
		{Snap: "kernel.snap", Path: "kernel.efi", Role: bootloader.RoleRecovery},
	})
	c.Assert(chains[1], DeepEquals, []bootloader.BootFile{
		{Path: "EFI/boot/bootx64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/boot/grubx64.efi", Role: bootloader.RoleRecovery},
		{Snap: "kernel.snap", Path: "kernel.efi", Role: bootloader.RoleRecovery},
	})
}

func (s *grubTestSuite) TestRecoveryBootChainsWithFallback(c *C) {
	s.makeFakeShimFallback(c)
	s.makeFakeGrubEFINativeEnv(c, nil)
	g := bootloader.NewGrub(s.rootdir, &bootloader.Options{Role: bootloader.RoleRecovery})
	tab, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)

	chains := mylog.Check2(tab.RecoveryBootChains("kernel.snap"))

	c.Assert(chains, HasLen, 2)
	c.Assert(chains[0], DeepEquals, []bootloader.BootFile{
		{Path: "EFI/ubuntu/shimx64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/ubuntu/grubx64.efi", Role: bootloader.RoleRecovery},
		{Snap: "kernel.snap", Path: "kernel.efi", Role: bootloader.RoleRecovery},
	})
	c.Assert(chains[1], DeepEquals, []bootloader.BootFile{
		{Path: "EFI/boot/bootx64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/boot/grubx64.efi", Role: bootloader.RoleRecovery},
		{Snap: "kernel.snap", Path: "kernel.efi", Role: bootloader.RoleRecovery},
	})
}

func (s *grubTestSuite) TestRecoveryBootChainsNotRecoveryBootloader(c *C) {
	s.makeFakeGrubEnv(c)
	g := bootloader.NewGrub(s.rootdir, nil)
	tab, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)

	_ := mylog.Check2(tab.RecoveryBootChains("kernel.snap"))
	c.Assert(err, ErrorMatches, "not a recovery bootloader")
}

func (s *grubTestSuite) TestBootChains(c *C) {
	s.makeFakeGrubEFINativeEnv(c, nil)
	g := bootloader.NewGrub(s.rootdir, &bootloader.Options{Role: bootloader.RoleRecovery})
	tab, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)

	g2 := bootloader.NewGrub(s.rootdir, &bootloader.Options{Role: bootloader.RoleRunMode})

	chains := mylog.Check2(tab.BootChains(g2, "kernel.snap"))

	c.Assert(chains, HasLen, 2)
	c.Assert(chains[0], DeepEquals, []bootloader.BootFile{
		{Path: "EFI/ubuntu/shimx64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/ubuntu/grubx64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/boot/grubx64.efi", Role: bootloader.RoleRunMode},
		{Snap: "kernel.snap", Path: "kernel.efi", Role: bootloader.RoleRunMode},
	})
	c.Assert(chains[1], DeepEquals, []bootloader.BootFile{
		{Path: "EFI/boot/bootx64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/boot/grubx64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/boot/grubx64.efi", Role: bootloader.RoleRunMode},
		{Snap: "kernel.snap", Path: "kernel.efi", Role: bootloader.RoleRunMode},
	})
}

func (s *grubTestSuite) TestBootChainsWithFallback(c *C) {
	s.makeFakeShimFallback(c)
	s.makeFakeGrubEFINativeEnv(c, nil)
	g := bootloader.NewGrub(s.rootdir, &bootloader.Options{Role: bootloader.RoleRecovery})
	tab, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)

	g2 := bootloader.NewGrub(s.rootdir, &bootloader.Options{Role: bootloader.RoleRunMode})

	chains := mylog.Check2(tab.BootChains(g2, "kernel.snap"))

	c.Assert(chains, HasLen, 2)
	c.Assert(chains[0], DeepEquals, []bootloader.BootFile{
		{Path: "EFI/ubuntu/shimx64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/ubuntu/grubx64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/boot/grubx64.efi", Role: bootloader.RoleRunMode},
		{Snap: "kernel.snap", Path: "kernel.efi", Role: bootloader.RoleRunMode},
	})
	c.Assert(chains[1], DeepEquals, []bootloader.BootFile{
		{Path: "EFI/boot/bootx64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/boot/grubx64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/boot/grubx64.efi", Role: bootloader.RoleRunMode},
		{Snap: "kernel.snap", Path: "kernel.efi", Role: bootloader.RoleRunMode},
	})
}

func (s *grubTestSuite) TestBootChainsArm64(c *C) {
	s.makeFakeGrubEFINativeEnv(c, nil)
	r := archtest.MockArchitecture("arm64")
	defer r()
	g := bootloader.NewGrub(s.rootdir, &bootloader.Options{Role: bootloader.RoleRecovery})
	tab, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)

	g2 := bootloader.NewGrub(s.rootdir, &bootloader.Options{Role: bootloader.RoleRunMode})

	chains := mylog.Check2(tab.BootChains(g2, "kernel.snap"))

	c.Assert(chains, HasLen, 2)
	c.Assert(chains[0], DeepEquals, []bootloader.BootFile{
		{Path: "EFI/ubuntu/shimaa64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/ubuntu/grubaa64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/boot/grubaa64.efi", Role: bootloader.RoleRunMode},
		{Snap: "kernel.snap", Path: "kernel.efi", Role: bootloader.RoleRunMode},
	})
	c.Assert(chains[1], DeepEquals, []bootloader.BootFile{
		{Path: "EFI/boot/bootaa64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/boot/grubaa64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/boot/grubaa64.efi", Role: bootloader.RoleRunMode},
		{Snap: "kernel.snap", Path: "kernel.efi", Role: bootloader.RoleRunMode},
	})
}

func (s *grubTestSuite) TestBootChainsArm64WithFallback(c *C) {
	s.makeFakeGrubEFINativeEnv(c, nil)
	s.makeFakeShimFallback(c)
	r := archtest.MockArchitecture("arm64")
	defer r()
	g := bootloader.NewGrub(s.rootdir, &bootloader.Options{Role: bootloader.RoleRecovery})
	tab, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)

	g2 := bootloader.NewGrub(s.rootdir, &bootloader.Options{Role: bootloader.RoleRunMode})

	chains := mylog.Check2(tab.BootChains(g2, "kernel.snap"))

	c.Assert(chains, HasLen, 2)
	c.Assert(chains[0], DeepEquals, []bootloader.BootFile{
		{Path: "EFI/ubuntu/shimaa64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/ubuntu/grubaa64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/boot/grubaa64.efi", Role: bootloader.RoleRunMode},
		{Snap: "kernel.snap", Path: "kernel.efi", Role: bootloader.RoleRunMode},
	})
	c.Assert(chains[1], DeepEquals, []bootloader.BootFile{
		{Path: "EFI/boot/bootaa64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/boot/grubaa64.efi", Role: bootloader.RoleRecovery},
		{Path: "EFI/boot/grubaa64.efi", Role: bootloader.RoleRunMode},
		{Snap: "kernel.snap", Path: "kernel.efi", Role: bootloader.RoleRunMode},
	})
}

func (s *grubTestSuite) TestBootChainsNotRecoveryBootloader(c *C) {
	s.makeFakeGrubEnv(c)
	g := bootloader.NewGrub(s.rootdir, nil)
	tab, ok := g.(bootloader.TrustedAssetsBootloader)
	c.Assert(ok, Equals, true)

	g2 := bootloader.NewGrub(s.rootdir, &bootloader.Options{NoSlashBoot: true, Role: bootloader.RoleRunMode})

	_ := mylog.Check2(tab.BootChains(g2, "kernel.snap"))
	c.Assert(err, ErrorMatches, "not a recovery bootloader")
}
