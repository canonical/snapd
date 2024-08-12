// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package kernel_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type kernelDriversTestSuite struct {
	testutil.BaseTest
}

var _ = Suite(&kernelDriversTestSuite{})

func (s *kernelDriversTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *kernelDriversTestSuite) TestKernelVersionFromModulesDir(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	c.Assert(os.MkdirAll(mountDir, 0755), IsNil)

	// No map file
	ver, err := kernel.KernelVersionFromModulesDir(mountDir)
	c.Check(err, ErrorMatches, `open .*/run/mnt/pc-kernel/modules: no such file or directory`)
	c.Check(ver, Equals, "")

	// Create directory so kernel version can be found
	c.Assert(os.MkdirAll(filepath.Join(
		mountDir, "modules", "5.15.0-78-generic"), 0755), IsNil)
	ver, err = kernel.KernelVersionFromModulesDir(mountDir)
	c.Check(err, IsNil)
	c.Check(ver, Equals, "5.15.0-78-generic")

	// Too many matches
	c.Assert(os.MkdirAll(filepath.Join(
		mountDir, "modules", "5.15.0-90-generic"), 0755), IsNil)
	ver, err = kernel.KernelVersionFromModulesDir(mountDir)
	c.Check(err, ErrorMatches, `more than one modules directory in ".*/run/mnt/pc-kernel/modules"`)
	c.Check(ver, Equals, "")
}

func (s *kernelDriversTestSuite) TestKernelVersionFromModulesDirNoModDir(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	c.Assert(os.MkdirAll(mountDir, 0755), IsNil)

	c.Assert(os.MkdirAll(filepath.Join(mountDir, "modules"), 0755), IsNil)
	// Create file instead of directory
	c.Assert(os.WriteFile(filepath.Join(
		mountDir, "modules", "5.15.0-78-generic"), []byte{}, 0644), IsNil)
	ver, err := kernel.KernelVersionFromModulesDir(mountDir)
	c.Check(err, ErrorMatches, `no modules directory found in ".*/run/mnt/pc-kernel/modules"`)
	c.Check(ver, Equals, "")
}

func (s *kernelDriversTestSuite) TestKernelVersionFromModulesDirBadVersion(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	c.Assert(os.MkdirAll(filepath.Join(
		mountDir, "modules", "5.15.myway"), 0755), IsNil)

	ver, err := kernel.KernelVersionFromModulesDir(mountDir)
	c.Check(err, ErrorMatches, `no modules directory found in ".*/run/mnt/pc-kernel/modules"`)
	c.Check(ver, Equals, "")
}

type createKernelSnapFilesOpts struct {
	withFwUpdatesDir bool
}

func createKernelSnapFiles(c *C, kversion, kdir string, opts createKernelSnapFilesOpts) {
	c.Assert(os.MkdirAll(kdir, 0755), IsNil)

	// Create modinfo files
	modDir := filepath.Join(kdir, "modules", kversion)
	c.Assert(os.MkdirAll(modDir, 0755), IsNil)
	modFile := []string{"modules.builtin.alias.bin", "modules.dep.bin", "modules.symbols"}
	allFiles := append(modFile, "other.mod", "foo.bin")
	for _, f := range allFiles {
		c.Assert(os.WriteFile(filepath.Join(modDir, f), []byte{}, 0644), IsNil)
	}

	// Create firmware
	fwDir := filepath.Join(kdir, "firmware")
	c.Assert(os.MkdirAll(fwDir, 0755), IsNil)
	// Regular files
	for _, f := range []string{"blob1", "blob2"} {
		c.Assert(os.WriteFile(filepath.Join(fwDir, f), []byte{}, 0644), IsNil)
	}
	if opts.withFwUpdatesDir {
		c.Assert(os.MkdirAll(filepath.Join(fwDir, "updates"), 0755), IsNil)
	}
	// Directory, write file inside
	fwSubDir := filepath.Join(fwDir, "subdir")
	c.Assert(os.MkdirAll(fwSubDir, 0755), IsNil)
	blob3 := filepath.Join(fwSubDir, "blob3")
	c.Assert(os.WriteFile(blob3, []byte{}, 0644), IsNil)
	// Symlink
	os.Symlink("subdir/blob3", filepath.Join(fwDir, "ln_to_blob3"))
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversTree(c *C) {
	// Build twice to make sure the function is idempotent
	testBuildKernelDriversTree(c, createKernelSnapFilesOpts{})
	testBuildKernelDriversTree(c, createKernelSnapFilesOpts{})

	// Now remove and check
	treeRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1")
	kernel.RemoveKernelDriversTree(treeRoot)
	c.Assert(osutil.FileExists(treeRoot), Equals, false)
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversTreeWithUpdates(c *C) {
	testBuildKernelDriversTree(c, createKernelSnapFilesOpts{withFwUpdatesDir: true})

	// Now remove and check
	treeRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1")
	kernel.RemoveKernelDriversTree(treeRoot)
	c.Assert(osutil.FileExists(treeRoot), Equals, false)
}

type expectInode struct {
	file       string
	fType      fs.FileMode
	linkTarget string
}

func doDirChecks(c *C, dir string, expected []expectInode) {
	entries, err := os.ReadDir(dir)
	c.Assert(err, IsNil)
	c.Assert(len(entries), Equals, len(expected))
	for i, ent := range entries {
		c.Check(ent.Name(), Equals, expected[i].file)
		c.Check(ent.Type(), Equals, expected[i].fType)
		if ent.Type() == fs.ModeSymlink {
			dest, err := os.Readlink(filepath.Join(dir, ent.Name()))
			c.Assert(err, IsNil)
			c.Check(dest, Equals, expected[i].linkTarget)
		}
	}
}

func testBuildKernelDriversTree(c *C, opts createKernelSnapFilesOpts) {
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	kversion := "5.15.0-78-generic"
	createKernelSnapFiles(c, kversion, mountDir, opts)

	// Now build the tree
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	c.Assert(kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir},
		nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true}), IsNil)

	// Check content is as expected
	modsRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1", "lib", "modules", kversion)
	modsMntDir := filepath.Join(mountDir, "modules", kversion)
	expected := []expectInode{
		{"kernel", fs.ModeSymlink, filepath.Join(modsMntDir, "kernel")},
		{"modules.builtin.alias.bin", 0, ""},
		{"modules.dep.bin", 0, ""},
		{"modules.symbols", 0, ""},
		{"updates", fs.ModeDir, ""},
		{"vdso", fs.ModeSymlink, filepath.Join(modsMntDir, "vdso")},
	}
	doDirChecks(c, modsRoot, expected)

	// Check firmware entries
	fwRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1", "lib", "firmware")
	fwMntDir := filepath.Join(mountDir, "firmware")
	expected = []expectInode{
		{"blob1", fs.ModeSymlink, filepath.Join(fwMntDir, "blob1")},
		{"blob2", fs.ModeSymlink, filepath.Join(fwMntDir, "blob2")},
		{"ln_to_blob3", fs.ModeSymlink, "subdir/blob3"},
		{"subdir", fs.ModeSymlink, filepath.Join(fwMntDir, "subdir")},
		{"updates", fs.ModeDir, ""},
	}
	doDirChecks(c, fwRoot, expected)

	// Check symlinks to files point to real files
	for _, ln := range []string{
		filepath.Join(fwRoot, "blob1"),
		filepath.Join(fwRoot, "blob2"),
		filepath.Join(fwRoot, "ln_to_blob3"),
		filepath.Join(fwRoot, "subdir/blob3"),
	} {
		path, err := filepath.EvalSymlinks(ln)
		c.Assert(err, IsNil)
		exists, isReg, err := osutil.RegularFileExists(path)
		c.Assert(err, IsNil)
		c.Check(exists, Equals, true)
		c.Check(isReg, Equals, true)
	}
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversNoModsOrFw(c *C) {
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/11")
	createKernelSnapFilesOnlyModules(c, kversion, mountDir)

	// Build the tree should not fail
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(11))
	err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir}, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	// check kernel dep file is still copied
	modPath := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir),
		"kernel", "pc-kernel", "11", "lib", "modules", kversion, "modules.dep.bin")
	exists, isReg, err := osutil.RegularFileExists(modPath)
	c.Assert(err, IsNil)
	c.Check(exists, Equals, true)
	c.Check(isReg, Equals, true)
}

func createKernelSnapFilesOnlyModules(c *C, kversion, kdir string) {
	c.Assert(os.MkdirAll(kdir, 0755), IsNil)

	// Create modinfo files
	modDir := filepath.Join(kdir, "modules", kversion)
	c.Assert(os.MkdirAll(modDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(modDir, "modules.dep.bin"), []byte{}, 0644), IsNil)
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversOnlyMods(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	kversion := "5.15.0-78-generic"
	createKernelSnapFilesOnlyModules(c, kversion, mountDir)

	// Build the tree should not fail
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir}, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	// check created file
	modPath := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1", "lib", "modules", kversion, "modules.dep.bin")
	exists, isReg, err := osutil.RegularFileExists(modPath)
	c.Assert(err, IsNil)
	c.Check(exists, Equals, true)
	c.Check(isReg, Equals, true)
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversOnlyModsWithTargetDir(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/tmp-mount")
	kTargetDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	kversion := "5.15.0-78-generic"
	createKernelSnapFilesOnlyModules(c, kversion, mountDir)

	// Build the tree should not fail
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  kTargetDir}, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	// check created file
	modPath := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1", "lib", "modules", kversion)
	modDepBinPath := filepath.Join(modPath, "modules.dep.bin")
	exists, isReg, err := osutil.RegularFileExists(modDepBinPath)
	c.Assert(err, IsNil)
	c.Check(exists, Equals, true)
	c.Check(isReg, Equals, true)
	// Check symlinks points to final target
	modsPath := filepath.Join(modPath, "kernel")
	modsTarget, err := os.Readlink(modsPath)
	c.Assert(err, IsNil)
	c.Check(modsTarget, Equals, filepath.Join(kTargetDir, "modules", kversion, "kernel"))
}

func createKernelSnapFilesOnlyFw(c *C, kdir string) {
	c.Assert(os.MkdirAll(kdir, 0755), IsNil)

	// Create firmware files
	fwDir := filepath.Join(kdir, "firmware")
	c.Assert(os.MkdirAll(fwDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(fwDir, "wifi_fw.bin"), []byte{}, 0644), IsNil)
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversOnlyFw(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	createKernelSnapFilesOnlyFw(c, mountDir)

	// Build the tree should not fail
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir}, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	// check link
	fwPath := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1", "lib", "firmware", "wifi_fw.bin")
	c.Assert(osutil.IsSymlink(fwPath), Equals, true)
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversOnlyFwWithTargetDir(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/tmp-mount")
	kTargetDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	createKernelSnapFilesOnlyFw(c, mountDir)

	// Build the tree should not fail
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  kTargetDir}, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	// check link
	fwPath := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1", "lib", "firmware", "wifi_fw.bin")
	fwPathTarget, err := os.Readlink(fwPath)
	c.Assert(err, IsNil)
	c.Check(fwPathTarget, Equals, filepath.Join(kTargetDir, "firmware", "wifi_fw.bin"))
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversAbsFwSymlink(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")

	// Create firmware files
	fwDir := filepath.Join(mountDir, "firmware")
	c.Assert(os.MkdirAll(fwDir, 0755), IsNil)
	// Symlink
	os.Symlink("/absdir/blob3", filepath.Join(fwDir, "ln_to_abs"))

	// Fails on the absolute path in the link
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir}, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, ErrorMatches, `symlink \".*lib/firmware/ln_to_abs\" points to absolute path \"/absdir/blob3\"`)

	// Make sure the tree has been deleted
	treeRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1")
	c.Assert(osutil.FileExists(treeRoot), Equals, false)
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversTreeCleanup(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	kversion := "5.15.0-78-generic"
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	restore := kernel.MockOsSymlink(func(string, string) error {
		return errors.New("mocked symlink error")
	})
	defer restore()

	// Now build the tree
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir}, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, ErrorMatches, "mocked symlink error")

	// Make sure the tree has been deleted
	treeRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1")
	c.Assert(osutil.FileExists(treeRoot), Equals, false)
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversBadFileType(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	kversion := "5.15.0-78-generic"
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	// Additional file of not expected type in "firmware"
	fwDir := filepath.Join(mountDir, "firmware")
	c.Assert(syscall.Mkfifo(filepath.Join(fwDir, "fifo"), 0666), IsNil)

	// Now build the tree
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir}, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, ErrorMatches, `"fifo" has unexpected file type: p---------`)

	// Make sure the tree has been deleted
	treeRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1")
	c.Assert(osutil.FileExists(treeRoot), Equals, false)
}

func createKernelModulesCompFiles(c *C, kversion, compdir, filePrefix string) {
	c.Assert(os.MkdirAll(compdir, 0755), IsNil)

	// Create some kernel module file
	modDir := filepath.Join(compdir, "modules", kversion, "kernel/foo")
	c.Assert(os.MkdirAll(modDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(modDir, filePrefix+".ko.zst"), []byte{}, 0644), IsNil)

	// and some fw
	fwDir := filepath.Join(compdir, "firmware")
	c.Assert(os.MkdirAll(fwDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(fwDir, filePrefix+".bin"), []byte{}, 0644), IsNil)
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversTreeWithKernelAndComps(c *C) {
	// Build twice to make sure the function is idempotent
	opts := &kernel.KernelDriversTreeOptions{KernelInstall: true}
	testBuildKernelDriversTreeWithComps(c, opts)
	testBuildKernelDriversTreeWithComps(c, opts)

	// Now remove and check
	treeRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1")
	kernel.RemoveKernelDriversTree(treeRoot)
	c.Assert(osutil.FileExists(treeRoot), Equals, false)
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversTreeCompsNoKernelInstall(c *C) {
	// Kernel needs to have been installed first
	testBuildKernelDriversTree(c, createKernelSnapFilesOpts{})
	// Build twice to make sure the function is idempotent
	opts := &kernel.KernelDriversTreeOptions{KernelInstall: false}
	testBuildKernelDriversTreeWithComps(c, opts)
	testBuildKernelDriversTreeWithComps(c, opts)

	// Now remove and check
	treeRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1")
	kernel.RemoveKernelDriversTree(treeRoot)
	c.Assert(osutil.FileExists(treeRoot), Equals, false)

	// No _tmp folder should be around
	treeRoot = filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1_tmp")
	c.Assert(osutil.FileExists(treeRoot), Equals, false)
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversTreeCompsNoKernel(c *C) {
	mockCmd := testutil.MockCommand(c, "depmod", "")
	defer mockCmd.Restore()

	mountDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	kversion := "5.15.0-78-generic"
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	compMntDir1 := filepath.Join(dirs.RunDir, "mnt/kernel-snaps/comp1")
	compMntDir2 := filepath.Join(dirs.RunDir, "mnt/kernel-snaps/comp2")
	createKernelModulesCompFiles(c, kversion, compMntDir1, "comp1")
	createKernelModulesCompFiles(c, kversion, compMntDir2, "comp2")
	kmodsConts := []snap.ContainerPlaceInfo{
		snap.MinimalComponentContainerPlaceInfo("comp1", snap.R(11), "pc-kernel"),
		snap.MinimalComponentContainerPlaceInfo("comp2", snap.R(22), "pc-kernel"),
	}
	compsMntPts := []kernel.ModulesCompMountPoints{
		{"comp1", kernel.MountPoints{kmodsConts[0].MountDir(), kmodsConts[0].MountDir()}},
		{"comp2", kernel.MountPoints{kmodsConts[1].MountDir(), kmodsConts[1].MountDir()}},
	}

	// Now build the tree, will fail as no kernel was installed previously
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir},
		compsMntPts, destDir, &kernel.KernelDriversTreeOptions{KernelInstall: false})
	c.Assert(err, ErrorMatches, `while swapping .*: no such file or directory`)
}

func testBuildKernelDriversTreeWithComps(c *C, opts *kernel.KernelDriversTreeOptions) {
	mockCmd := testutil.MockCommand(c, "depmod", "")
	defer mockCmd.Restore()

	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	kversion := "5.15.0-78-generic"
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	compMntDir1 := filepath.Join(dirs.SnapMountDir, "pc-kernel/components/mnt/comp1/11")
	compMntDir2 := filepath.Join(dirs.SnapMountDir, "pc-kernel/components/mnt/comp2/22")
	createKernelModulesCompFiles(c, kversion, compMntDir1, "comp1")
	createKernelModulesCompFiles(c, kversion, compMntDir2, "comp2")
	kmodsConts := []snap.ContainerPlaceInfo{
		snap.MinimalComponentContainerPlaceInfo("comp1", snap.R(11), "pc-kernel"),
		snap.MinimalComponentContainerPlaceInfo("comp2", snap.R(22), "pc-kernel"),
	}
	compsMntPts := []kernel.ModulesCompMountPoints{
		{"comp1", kernel.MountPoints{kmodsConts[0].MountDir(), kmodsConts[0].MountDir()}},
		{"comp2", kernel.MountPoints{kmodsConts[1].MountDir(), kmodsConts[1].MountDir()}},
	}

	workSubdir := "1_tmp"
	if opts.KernelInstall {
		workSubdir = "1"
	}
	treeRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", workSubdir)
	// Find out if the directory already exists, as in that case
	// there are no calls to depmod
	exists, isDir, err := osutil.DirExists(treeRoot)
	c.Assert(err, IsNil)

	// Now build the tree
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	c.Assert(kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir},
		compsMntPts, destDir, opts), IsNil)

	if exists {
		c.Assert(isDir, Equals, true)
		c.Assert(mockCmd.Calls(), IsNil)
	} else {
		c.Assert(mockCmd.Calls(), DeepEquals, [][]string{
			{"depmod", "-b", treeRoot, kversion},
		})
	}

	// Check modules root dir is as expected
	modsRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1", "lib", "modules", kversion)
	modsMntDir := filepath.Join(mountDir, "modules", kversion)
	expected := []expectInode{
		{"kernel", fs.ModeSymlink, filepath.Join(modsMntDir, "kernel")},
		{"modules.builtin.alias.bin", 0, ""},
		{"modules.dep.bin", 0, ""},
		{"modules.symbols", 0, ""},
		{"updates", fs.ModeDir, ""},
		{"vdso", fs.ModeSymlink, filepath.Join(modsMntDir, "vdso")},
	}
	doDirChecks(c, modsRoot, expected)

	// Check links for modules shipped in components
	updatesDir := filepath.Join(modsRoot, "updates")
	expected = []expectInode{
		{"comp1", fs.ModeSymlink, filepath.Join(compMntDir1, "modules", kversion)},
		{"comp2", fs.ModeSymlink, filepath.Join(compMntDir2, "modules", kversion)},
	}
	doDirChecks(c, updatesDir, expected)

	// Check firmware entries from snap
	fwRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1", "lib", "firmware")
	fwMntDir := filepath.Join(mountDir, "firmware")
	expected = []expectInode{
		{"blob1", fs.ModeSymlink, filepath.Join(fwMntDir, "blob1")},
		{"blob2", fs.ModeSymlink, filepath.Join(fwMntDir, "blob2")},
		{"ln_to_blob3", fs.ModeSymlink, "subdir/blob3"},
		{"subdir", fs.ModeSymlink, filepath.Join(fwMntDir, "subdir")},
		{"updates", fs.ModeDir, ""},
	}
	doDirChecks(c, fwRoot, expected)

	// Check firmware entries from components
	fwUpdates := filepath.Join(fwRoot, "updates")
	expected = []expectInode{
		{"comp1.bin", fs.ModeSymlink, filepath.Join(compMntDir1, "firmware/comp1.bin")},
		{"comp2.bin", fs.ModeSymlink, filepath.Join(compMntDir2, "firmware/comp2.bin")},
	}
	doDirChecks(c, fwUpdates, expected)

	// Check symlinks to files point to real files
	for _, ln := range []string{
		filepath.Join(updatesDir, "comp1/kernel/foo/comp1.ko.zst"),
		filepath.Join(updatesDir, "comp2/kernel/foo/comp2.ko.zst"),
		filepath.Join(fwRoot, "blob1"),
		filepath.Join(fwRoot, "blob2"),
		filepath.Join(fwRoot, "ln_to_blob3"),
		filepath.Join(fwRoot, "subdir/blob3"),
		filepath.Join(fwUpdates, "comp1.bin"),
		filepath.Join(fwUpdates, "comp2.bin"),
	} {
		path, err := filepath.EvalSymlinks(ln)
		c.Assert(err, IsNil)
		exists, isReg, err := osutil.RegularFileExists(path)
		c.Assert(err, IsNil)
		c.Check(exists, Equals, true)
		c.Check(isReg, Equals, true)
	}

	if !opts.KernelInstall {
		// Check that there is no tmp folder left behind
		tmpDir := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1_tmp")
		exists, _, _ = osutil.RegularFileExists(tmpDir)
		c.Check(exists, Equals, false)
	}
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversTreeCompsWithTargetDir(c *C) {
	mockCmd := testutil.MockCommand(c, "depmod", "")
	defer mockCmd.Restore()

	// Kernel needs to have been installed first
	testBuildKernelDriversTree(c, createKernelSnapFilesOpts{})

	mountDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	kversion := "5.15.0-78-generic"
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	compMntDir1 := filepath.Join(dirs.RunDir, "mnt/kernel-snaps/comp1")
	createKernelModulesCompFiles(c, kversion, compMntDir1, "comp1")
	kmodCont := snap.MinimalComponentContainerPlaceInfo("comp1", snap.R(11), "pc-kernel")
	// Current mount is different to the one in the final system
	compsMntPts := []kernel.ModulesCompMountPoints{
		{"comp1", kernel.MountPoints{
			Current: compMntDir1,
			Target:  kmodCont.MountDir()}},
	}

	// Now build the tree, will fail as no kernel was installed previously
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir},
		compsMntPts, destDir, &kernel.KernelDriversTreeOptions{KernelInstall: false})
	c.Assert(err, IsNil)

	// Check firmware entries from components
	fwRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1", "lib", "firmware")
	fwUpdates := filepath.Join(fwRoot, "updates")
	expected := []expectInode{
		{"comp1.bin", fs.ModeSymlink, filepath.Join(kmodCont.MountDir(), "firmware/comp1.bin")},
	}
	doDirChecks(c, fwUpdates, expected)
}
