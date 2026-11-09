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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
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

func (s *kernelDriversTestSuite) TestKernelInstallKeepsExistingMarkerlessTree(c *C) {
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, "5.15.0-78-generic", mountDir, createKernelSnapFilesOpts{})
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	liveFile := filepath.Join(destDir, "lib", "firmware", "user-file")
	c.Assert(os.MkdirAll(filepath.Dir(liveFile), 0755), IsNil)
	c.Assert(os.WriteFile(liveFile, []byte("keep"), 0644), IsNil)

	// A preexisting markerless tree is reconciled while preserving user data.
	err := kernel.EnsureKernelDriversTree(kernel.MountPoints{Current: mountDir, Target: mountDir}, nil,
		destDir)
	c.Assert(err, IsNil)
	content, err := os.ReadFile(liveFile)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "keep")
	outdated, err := kernel.DriversTreeOutdated(destDir)
	c.Assert(err, IsNil)
	c.Check(outdated, Equals, false)
}

type expectInode struct {
	file       string
	fType      fs.FileMode
	linkTarget string
}

func doDirChecks(c *C, dir string, expected []expectInode) {
	entries, err := os.ReadDir(dir)
	c.Assert(err, IsNil)
	c.Check(len(entries), Equals, len(expected))
	for i, ent := range entries {
		c.Logf("checking entry: %v", filepath.Join(dir, ent.Name()))
		if i >= len(expected) {
			c.Errorf("missing entry for %q", ent.Name())
			continue
		}
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
	err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir},
		nil, destDir)
	c.Assert(err, IsNil)

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

	// Marker file should record the current generator version.
	markerDir := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1")
	v, err := kernel.ReadDriversTreeMeta(markerDir)
	c.Assert(err, IsNil)
	c.Check(v.GeneratorVersion, Equals, kernel.KernelDriversTreeGeneratorVersion())
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
			Target:  mountDir}, nil, destDir)
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
			Target:  mountDir}, nil, destDir)
	c.Assert(err, IsNil)

	// check created files
	expected := []expectInode{
		{"kernel", fs.ModeSymlink, filepath.Join(mountDir, "modules", kversion, "kernel")},
		{"modules.dep.bin", 0, ""},
		{"updates", fs.ModeDir, ""},
		{"vdso", fs.ModeSymlink, filepath.Join(mountDir, "modules", kversion, "vdso")},
	}

	doDirChecks(c, filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1", "lib", "modules", kversion), expected)
}

// TestBuildKernelDriversModinfoSymlink verifies that a modinfo file (modules.*)
// that is a symlink in the kernel snap is still copied into the drivers tree,
// following the symlink, just like the previous glob-based copy did.
func (s *kernelDriversTestSuite) TestBuildKernelDriversModinfoSymlink(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	kversion := "5.15.0-78-generic"
	createKernelSnapFilesOnlyModules(c, kversion, mountDir)

	// Add a symlinked modinfo entry pointing at the regular modules.dep.bin.
	modDir := filepath.Join(mountDir, "modules", kversion)
	c.Assert(os.Symlink("modules.dep.bin", filepath.Join(modDir, "modules.alias")), IsNil)

	// Build the tree should not fail
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir}, nil, destDir)
	c.Assert(err, IsNil)

	modsRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1", "lib", "modules", kversion)

	// The regular modinfo file is copied.
	c.Check(osutil.FileExists(filepath.Join(modsRoot, "modules.dep.bin")), Equals, true)

	// The symlinked modinfo file is copied too (its content is followed),
	// rather than silently dropped because it is not a regular file.
	aliasPath := filepath.Join(modsRoot, "modules.alias")
	c.Check(osutil.FileExists(aliasPath), Equals, true)
	// CopyFile follows the symlink, so the destination is a regular file
	// with the same content as the link target.
	aliasExists, aliasIsReg, err := osutil.RegularFileExists(aliasPath)
	c.Assert(err, IsNil)
	c.Check(aliasExists, Equals, true)
	c.Check(aliasIsReg, Equals, true)
	c.Check(aliasPath, testutil.FileEquals, []byte{})
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversExtraDirsWithModules(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	kversion := "5.15.0-78-generic"
	createKernelSnapFilesOnlyModules(c, kversion, mountDir)

	extraModDir := filepath.Join(mountDir, "modules", kversion, "ubuntu")
	c.Assert(os.MkdirAll(extraModDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(extraModDir, "zfs.ko"), []byte("content"), 0644), IsNil)

	// Build the tree should not fail
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir}, nil, destDir)
	c.Assert(err, IsNil)

	// check created files
	expected := []expectInode{
		{"kernel", fs.ModeSymlink, filepath.Join(mountDir, "modules", kversion, "kernel")},
		{"modules.dep.bin", 0, ""},
		{"ubuntu", fs.ModeSymlink, filepath.Join(mountDir, "modules", kversion, "ubuntu")},
		{"updates", fs.ModeDir, ""},
		{"vdso", fs.ModeSymlink, filepath.Join(mountDir, "modules", kversion, "vdso")},
	}

	doDirChecks(c, filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1", "lib", "modules", kversion), expected)

	// and the extra module content is accessible
	c.Check(filepath.Join(mountDir, "modules", kversion, "ubuntu", "zfs.ko"), testutil.FileEquals, []byte("content"))
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversExtraDirsConflict(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	kversion := "5.15.0-78-generic"
	createKernelSnapFilesOnlyModules(c, kversion, mountDir)

	// the 'updates' directory conflicts with the name used for extra drivers from components
	extraModDir := filepath.Join(mountDir, "modules", kversion, "updates")
	c.Assert(os.MkdirAll(extraModDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(extraModDir, "abc.ko"), nil, 0644), IsNil)

	extraBuildDir := filepath.Join(mountDir, "modules", kversion, "build")
	c.Assert(os.MkdirAll(extraBuildDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(extraBuildDir, "main.c"), nil, 0644), IsNil)

	// Build the tree should not fail
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir}, nil, destDir)
	c.Assert(err, IsNil)

	// check created files, no symlink to 'updates', or 'build'
	expected := []expectInode{
		{"kernel", fs.ModeSymlink, filepath.Join(mountDir, "modules", kversion, "kernel")},
		{"modules.dep.bin", 0, ""},
		{"updates", fs.ModeDir, ""}, // a directory, not a symlink going back to the mounted kernel tree
		{"vdso", fs.ModeSymlink, filepath.Join(mountDir, "modules", kversion, "vdso")},
	}

	doDirChecks(c, filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1", "lib", "modules", kversion), expected)
}

// TestBuildKernelDriversOnlyModsWithTargetDir mirrors TestBuildKernelDriversOnlyMods
// but for the install/preseed flow where the current mount (where the kernel
// snap content is available now) differs from the target mount (the future
// runtime mount, which does not exist yet). The created symlinks are expected
// to point at the target mount and be dangling until the system boots into it.
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
			Target:  kTargetDir}, nil, destDir)
	c.Assert(err, IsNil)

	// check created files
	expected := []expectInode{
		{"kernel", fs.ModeSymlink, filepath.Join(kTargetDir, "modules", kversion, "kernel")},
		{"modules.dep.bin", 0, ""},
		{"updates", fs.ModeDir, ""},
		{"vdso", fs.ModeSymlink, filepath.Join(kTargetDir, "modules", kversion, "vdso")},
	}

	doDirChecks(c, filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1", "lib", "modules", kversion), expected)
}

// TestBuildKernelDriversExtraDirsWithModulesTargetDir mirrors
// TestBuildKernelDriversExtraDirsWithModules for the install/preseed flow
// where the target mount does not exist yet.
func (s *kernelDriversTestSuite) TestBuildKernelDriversExtraDirsWithModulesTargetDir(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/tmp-mount")
	kTargetDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	kversion := "5.15.0-78-generic"
	createKernelSnapFilesOnlyModules(c, kversion, mountDir)

	extraModDir := filepath.Join(mountDir, "modules", kversion, "ubuntu")
	c.Assert(os.MkdirAll(extraModDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(extraModDir, "zfs.ko"), []byte("content"), 0644), IsNil)

	// Build the tree should not fail
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  kTargetDir}, nil, destDir)
	c.Assert(err, IsNil)

	// check created files
	expected := []expectInode{
		{"kernel", fs.ModeSymlink, filepath.Join(kTargetDir, "modules", kversion, "kernel")},
		{"modules.dep.bin", 0, ""},
		{"ubuntu", fs.ModeSymlink, filepath.Join(kTargetDir, "modules", kversion, "ubuntu")},
		{"updates", fs.ModeDir, ""},
		{"vdso", fs.ModeSymlink, filepath.Join(kTargetDir, "modules", kversion, "vdso")},
	}

	doDirChecks(c, filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1", "lib", "modules", kversion), expected)
}

// TestBuildKernelDriversExtraDirsConflictTargetDir mirrors
// TestBuildKernelDriversExtraDirsConflict for the install/preseed flow where
// the target mount does not exist yet.
func (s *kernelDriversTestSuite) TestBuildKernelDriversExtraDirsConflictTargetDir(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/tmp-mount")
	kTargetDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	kversion := "5.15.0-78-generic"
	createKernelSnapFilesOnlyModules(c, kversion, mountDir)

	// the 'updates' directory conflicts with the name used for extra drivers from components
	extraModDir := filepath.Join(mountDir, "modules", kversion, "updates")
	c.Assert(os.MkdirAll(extraModDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(extraModDir, "abc.ko"), nil, 0644), IsNil)

	extraBuildDir := filepath.Join(mountDir, "modules", kversion, "build")
	c.Assert(os.MkdirAll(extraBuildDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(extraBuildDir, "main.c"), nil, 0644), IsNil)

	// Build the tree should not fail
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  kTargetDir}, nil, destDir)
	c.Assert(err, IsNil)

	// check created files, no symlink to 'updates', or 'build'
	expected := []expectInode{
		{"kernel", fs.ModeSymlink, filepath.Join(kTargetDir, "modules", kversion, "kernel")},
		{"modules.dep.bin", 0, ""},
		{"updates", fs.ModeDir, ""}, // a directory, not a symlink going back to the mounted kernel tree
		{"vdso", fs.ModeSymlink, filepath.Join(kTargetDir, "modules", kversion, "vdso")},
	}

	doDirChecks(c, filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1", "lib", "modules", kversion), expected)
}

func createKernelSnapFilesOnlyFw(c *C, kdir string) {
	c.Assert(os.MkdirAll(kdir, 0755), IsNil)

	// Create firmware files
	fwDir := filepath.Join(kdir, "firmware")
	c.Assert(os.MkdirAll(fwDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(fwDir, "wifi_fw.bin"), []byte{}, 0644), IsNil)
}

func (s *kernelDriversTestSuite) TestBuildAndRebuildKernelDriversOnlyFw(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	createKernelSnapFilesOnlyFw(c, mountDir)
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	mountPoints := kernel.MountPoints{Current: mountDir, Target: mountDir}
	c.Assert(kernel.EnsureKernelDriversTree(mountPoints, nil, destDir), IsNil)

	fwDir := filepath.Join(destDir, "lib", "firmware")
	fwLink := filepath.Join(fwDir, "wifi_fw.bin")
	fwTarget := filepath.Join(mountDir, "firmware", "wifi_fw.bin")
	c.Check(fwLink, testutil.SymlinkTargetEquals, fwTarget)
	staleFw := filepath.Join(fwDir, "updates", "stale.bin")
	c.Assert(os.WriteFile(staleFw, nil, 0644), IsNil)
	c.Assert(kernel.EnsureKernelDriversTree(mountPoints, nil, destDir), IsNil)

	c.Check(fwLink, testutil.SymlinkTargetEquals, fwTarget)
	c.Check(staleFw, testutil.FilePresent)
	c.Check(filepath.Join(fwDir, "updates"), testutil.FilePresent)
	c.Check(destDir+"_tmp", testutil.FileAbsent)
	meta, err := kernel.ReadDriversTreeMeta(destDir)
	c.Assert(err, IsNil)
	c.Check(meta.GeneratorVersion, Equals, kernel.KernelDriversTreeGeneratorVersion())
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
			Target:  kTargetDir}, nil, destDir)
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
			Target:  mountDir}, nil, destDir)
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
			Target:  mountDir}, nil, destDir)
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
			Target:  mountDir}, nil, destDir)
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
	testBuildKernelDriversTreeWithComps(c)
	testBuildKernelDriversTreeWithComps(c)

	// Now remove and check
	treeRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1")
	kernel.RemoveKernelDriversTree(treeRoot)
	c.Assert(osutil.FileExists(treeRoot), Equals, false)
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversTreeCompsNoKernelInstall(c *C) {
	// Kernel needs to have been installed first
	testBuildKernelDriversTree(c, createKernelSnapFilesOpts{})
	// Build twice to make sure the function is idempotent
	testBuildKernelDriversTreeWithComps(c)
	testBuildKernelDriversTreeWithComps(c)

	// Now remove and check
	treeRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1")
	kernel.RemoveKernelDriversTree(treeRoot)
	c.Assert(osutil.FileExists(treeRoot), Equals, false)

	// No _tmp folder should be around
	treeRoot = filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1_tmp")
	c.Assert(osutil.FileExists(treeRoot), Equals, false)
}

func (s *kernelDriversTestSuite) TestEnsureCreatesMissingTree(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	createKernelSnapFiles(c, "5.15.0-78-generic", mountDir, createKernelSnapFilesOpts{})
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))

	// The completed candidate must be synced before it is published.
	var syncedCandidate bool
	restore := kernel.MockDoSync(func() {
		if _, err := os.Readlink(filepath.Join(destDir+"_tmp", "lib", "firmware", "blob1")); err == nil {
			_, err = os.Lstat(destDir)
			syncedCandidate = os.IsNotExist(err)
		}
	})
	defer restore()
	err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, IsNil)
	c.Check(syncedCandidate, Equals, true)
	_, err = os.Stat(destDir)
	c.Check(err, IsNil)
	_, err = os.Stat(destDir + "_tmp")
	c.Check(os.IsNotExist(err), Equals, true)
	meta, err := kernel.ReadDriversTreeMeta(destDir)
	c.Assert(err, IsNil)
	c.Check(meta.GeneratorVersion, Equals, kernel.KernelDriversTreeGeneratorVersion())
	c.Check(*meta.Kernel, DeepEquals, kMntPts)
	c.Check(meta.Destination, Equals, destDir)
	c.Check(filepath.Join(destDir, "kernel.json.pending"), testutil.FileAbsent)

	// A file at the tree root is not an installed drivers tree.
	c.Assert(os.RemoveAll(destDir), IsNil)
	c.Assert(os.WriteFile(destDir, nil, 0644), IsNil)
	err = kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, ErrorMatches, `cannot update kernel drivers tree ".*": not a directory`)
}

func (s *kernelDriversTestSuite) TestEnsureReconcilesManagedChildren(c *C) {
	kversion := "5.15.0-78-generic"
	for _, tc := range []struct {
		name    string
		corrupt func(c *C, path string)
	}{
		{
			name: "missing",
			corrupt: func(c *C, path string) {
				c.Assert(os.RemoveAll(path), IsNil)
			},
		},
		{
			name: "wrong type",
			corrupt: func(c *C, path string) {
				c.Assert(os.RemoveAll(path), IsNil)
				c.Assert(os.WriteFile(path, []byte("stale"), 0644), IsNil)
			},
		},
	} {
		cmt := Commentf("case %q", tc.name)
		mountDir := filepath.Join(c.MkDir(), "mounted-kernel")
		createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})
		destDir := filepath.Join(c.MkDir(), "drivers-tree")
		kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}
		c.Assert(kernel.EnsureKernelDriversTree(kMntPts, nil, destDir), IsNil, cmt)

		modsDir := filepath.Join(destDir, "lib", "modules", kversion)
		fwUpdatesDir := filepath.Join(destDir, "lib", "firmware", "updates")
		tc.corrupt(c, modsDir)
		if tc.name == "missing" {
			tc.corrupt(c, fwUpdatesDir)
		}

		c.Assert(kernel.EnsureKernelDriversTree(kMntPts, nil, destDir), IsNil, cmt)
		c.Check(osutil.IsDirectory(modsDir), Equals, true, cmt)
		c.Check(osutil.IsDirectory(fwUpdatesDir), Equals, true, cmt)
		c.Check(osutil.FileExists(destDir+"_tmp"), Equals, false, cmt)
		// Reconciliation retains existing top-level kernel firmware.
		target, err := os.Readlink(filepath.Join(destDir, "lib", "firmware", "blob1"))
		c.Assert(err, IsNil, cmt)
		c.Check(target, Equals, filepath.Join(mountDir, "firmware", "blob1"), cmt)
	}
}

// TestRegenerateModulesAbsentFromSourceRetainsLiveModules pins that a source
// that stops exposing a modules directory does not wipe the previously
// built live modules tree. A kernel snap only ever ships a single kernel
// version, so there is no extraneous version to prune here, and a missing
// read must not be mistaken for an instruction to remove existing modules.
func (s *kernelDriversTestSuite) TestRegenerateModulesAbsentFromSourceRetainsLiveModules(c *C) {
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(c.MkDir(), "mounted-kernel")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})
	destDir := filepath.Join(c.MkDir(), "drivers-tree")
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}
	c.Assert(kernel.EnsureKernelDriversTree(kMntPts, nil, destDir), IsNil)

	modinfoPath := filepath.Join(destDir, "lib", "modules", kversion, "modules.symbols")
	before, err := os.ReadFile(modinfoPath)
	c.Assert(err, IsNil)

	// Firmware content is kept so the source still passes
	// checkKernelDriversSources; only "modules" goes away.
	c.Assert(os.RemoveAll(filepath.Join(mountDir, "modules")), IsNil)

	c.Assert(kernel.EnsureKernelDriversTree(kMntPts, nil, destDir), IsNil)
	after, err := os.ReadFile(modinfoPath)
	c.Assert(err, IsNil)
	c.Check(after, DeepEquals, before)
}

func testBuildKernelDriversTreeWithComps(c *C) {
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
	treeRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", workSubdir)
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir},
		compsMntPts, destDir)
	c.Assert(err, IsNil)
	c.Assert(mockCmd.Calls(), DeepEquals, [][]string{
		{"depmod", "-b", treeRoot, kversion},
	})

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

	// No temporary candidate survives a successful pass.
	c.Check(osutil.FileExists(destDir+"_tmp"), Equals, false)
	meta, err := kernel.ReadDriversTreeMeta(destDir)
	c.Assert(err, IsNil)
	c.Check(meta.Components, DeepEquals, compsMntPts)
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
		compsMntPts, destDir)
	c.Assert(err, IsNil)

	// Check firmware entries from components
	fwRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1", "lib", "firmware")
	fwUpdates := filepath.Join(fwRoot, "updates")
	expected := []expectInode{
		{"comp1.bin", fs.ModeSymlink, filepath.Join(kmodCont.MountDir(), "firmware/comp1.bin")},
	}
	doDirChecks(c, fwUpdates, expected)
}

func mockModel16(override map[string]any) *asserts.Model {
	model := map[string]any{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"gadget":       "gadget",
		"kernel":       "krnl",
		"architecture": "amd64",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	return assertstest.FakeAssertion(model, override).(*asserts.Model)
}

func mockModel20plus(override map[string]any) *asserts.Model {
	model := map[string]any{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []any{
			map[string]any{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]any{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			}}}
	for n, v := range override {
		model[n] = v
	}
	return assertstest.FakeAssertion(model, override).(*asserts.Model)
}

func (s *kernelDriversTestSuite) TestNeedsKernelDriversTree(c *C) {
	uc16model := mockModel16(nil)
	c.Assert(kernel.NeedsKernelDriversTree(uc16model), Equals, false)
	uc20model := mockModel20plus(nil)
	c.Assert(kernel.NeedsKernelDriversTree(uc20model), Equals, false)
	uc22model := mockModel20plus(map[string]any{"base": "core22"})
	c.Assert(kernel.NeedsKernelDriversTree(uc22model), Equals, false)
	uc24model := mockModel20plus(map[string]any{"base": "core24"})
	c.Assert(kernel.NeedsKernelDriversTree(uc24model), Equals, true)
}

func (s *kernelDriversTestSuite) TestNeedsKernelDriversTreeClassicWithWrongBase(c *C) {
	for _, tc := range []struct {
		version string
		result  bool
	}{
		{"23.10", false},
		{"24.04", true},
		{"24.10", true},
		{"25.04", false},
	} {
		defer release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: tc.version})()

		uc22model := mockModel20plus(map[string]any{"base": "core22",
			"classic": "true", "distribution": "ubuntu"})
		c.Assert(kernel.NeedsKernelDriversTree(uc22model), Equals, tc.result)
	}
}

func (s *kernelDriversTestSuite) TestDriversTreeMetaRoundTrip(c *C) {
	destDir := c.MkDir()

	c.Assert(kernel.WriteDriversTreeMeta(destDir), IsNil)

	v, err := kernel.ReadDriversTreeMeta(destDir)
	c.Assert(err, IsNil)
	c.Assert(v.GeneratorVersion, Equals, kernel.KernelDriversTreeGeneratorVersion())

	// The marker file lives inside destDir, so it is cleaned up by
	// RemoveKernelDriversTree's existing os.RemoveAll.
	c.Assert(osutil.FileExists(filepath.Join(destDir, "kernel.json")), Equals, true)
}

func (s *kernelDriversTestSuite) TestDriversTreeOutdatedMissingMarker(c *C) {
	destDir := c.MkDir()

	needsCheck, err := kernel.DriversTreeOutdated(destDir)
	c.Assert(err, IsNil)
	c.Assert(needsCheck, Equals, true)
}

func (s *kernelDriversTestSuite) TestDriversTreeOutdatedUpToDate(c *C) {
	destDir := c.MkDir()

	c.Assert(kernel.WriteDriversTreeMeta(destDir), IsNil)

	needsCheck, err := kernel.DriversTreeOutdated(destDir)
	c.Assert(err, IsNil)
	c.Assert(needsCheck, Equals, false)
}

func (s *kernelDriversTestSuite) TestDriversTreeOutdatedForwardOnly(c *C) {
	destDir := c.MkDir()

	// Simulate a tree built by a newer generator than what is currently
	// running (e.g. after a snapd revert): the marker records a version
	// higher than the current constant.
	func() {
		restore := kernel.MockKernelDriversTreeGeneratorVersion(100)
		defer restore()
		c.Assert(kernel.WriteDriversTreeMeta(destDir), IsNil)
	}()

	needsCheck, err := kernel.DriversTreeOutdated(destDir)
	c.Assert(err, IsNil)
	c.Assert(needsCheck, Equals, false)
}

func (s *kernelDriversTestSuite) TestDriversTreeOutdatedCorruptMarker(c *C) {
	destDir := c.MkDir()

	c.Assert(os.WriteFile(filepath.Join(destDir, "kernel.json"), []byte("not json"), 0644), IsNil)

	needsCheck, err := kernel.DriversTreeOutdated(destDir)
	c.Assert(err, IsNil)
	c.Assert(needsCheck, Equals, true)
}

func inodeOf(c *C, path string) uint64 {
	st, err := os.Stat(path)
	c.Assert(err, IsNil)
	sys, ok := st.Sys().(*syscall.Stat_t)
	c.Assert(ok, Equals, true)
	return sys.Ino
}

func (s *kernelDriversTestSuite) TestReconcileAlreadyCorrectKeepsParentInodes(c *C) {
	// Ensure rebuilds the versioned modules directory, but preserves the
	// lib/modules, lib/firmware, and firmware/updates directory inodes.
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, IsNil)

	modsParent := filepath.Join(destDir, "lib", "modules")
	fwParent := filepath.Join(destDir, "lib", "firmware")
	modsParentInode := inodeOf(c, modsParent)
	fwParentInode := inodeOf(c, fwParent)
	modsDir := filepath.Join(modsParent, kversion)
	fwUpdatesDir := filepath.Join(fwParent, "updates")
	modsInode := inodeOf(c, modsDir)
	fwUpdatesInode := inodeOf(c, fwUpdatesDir)

	err = kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, IsNil)

	// Marker gets (re)written.
	v, err := kernel.ReadDriversTreeMeta(destDir)
	c.Assert(err, IsNil)
	c.Check(v.GeneratorVersion, Equals, kernel.KernelDriversTreeGeneratorVersion())

	// lib/modules and lib/firmware themselves are bind-mount sources and
	// must never be renamed/recreated: only their children may be
	// swapped/replaced.
	c.Check(inodeOf(c, modsParent), Equals, modsParentInode)
	c.Check(inodeOf(c, fwParent), Equals, fwParentInode)
	c.Check(inodeOf(c, modsDir), Not(Equals), modsInode)
	c.Check(inodeOf(c, fwUpdatesDir), Equals, fwUpdatesInode)
}

func (s *kernelDriversTestSuite) TestRegenerateMissingLiveModulesParentDir(c *C) {
	// A missing lib/modules parent must be recreated before its versioned child.
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, IsNil)

	// Remove lib/modules itself, not just the <kversion> child.
	modsParent := filepath.Join(destDir, "lib", "modules")
	c.Assert(os.RemoveAll(modsParent), IsNil)

	err = kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, IsNil)

	modsKversionDir := filepath.Join(modsParent, kversion)
	modinfo, err := os.ReadFile(filepath.Join(modsKversionDir, "modules.symbols"))
	c.Assert(err, IsNil)
	src, err := os.ReadFile(filepath.Join(mountDir, "modules", kversion, "modules.symbols"))
	c.Assert(err, IsNil)
	c.Check(modinfo, DeepEquals, src)

	target, err := os.Readlink(filepath.Join(modsKversionDir, "kernel"))
	c.Assert(err, IsNil)
	c.Check(target, Equals, filepath.Join(mountDir, "modules", kversion, "kernel"))
}

func (s *kernelDriversTestSuite) TestRegenerateMissingModulesVendorDirSymlink(c *C) {
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, IsNil)

	modsParent := filepath.Join(destDir, "lib", "modules")
	modsParentInode := inodeOf(c, modsParent)

	// Simulate a kernel mount exposing a vendor directory not present
	// when the live tree was built.
	newVendorDir := filepath.Join(mountDir, "modules", kversion, "newvendor")
	c.Assert(os.MkdirAll(newVendorDir, 0755), IsNil)

	err = kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, IsNil)

	newLink := filepath.Join(modsParent, kversion, "newvendor")
	target, err := os.Readlink(newLink)
	c.Assert(err, IsNil)
	c.Check(target, Equals, newVendorDir)

	// The lib/modules directory itself must never be renamed/recreated:
	// it is a bind-mount source, only its children may be swapped.
	c.Check(inodeOf(c, modsParent), Equals, modsParentInode)
}

func (s *kernelDriversTestSuite) TestRegenerateCorruptedModinfoFile(c *C) {
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, IsNil)

	// Ensure rebuilds the managed modules child even when its marker is current.
	modinfoPath := filepath.Join(destDir, "lib", "modules", kversion, "modules.symbols")
	c.Assert(os.WriteFile(modinfoPath, []byte("corrupted"), 0644), IsNil)

	err = kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, IsNil)

	restored, err := os.ReadFile(modinfoPath)
	c.Assert(err, IsNil)
	src, err := os.ReadFile(filepath.Join(mountDir, "modules", kversion, "modules.symbols"))
	c.Assert(err, IsNil)
	c.Check(restored, DeepEquals, src)
}

func (s *kernelDriversTestSuite) TestRegenerateFirmwareBestEffortContinuesPastError(c *C) {
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, IsNil)

	fwParent := filepath.Join(destDir, "lib", "firmware")

	// Make three links need replacing; repair what we can and report both
	// failures rather than hiding all but the first.
	for _, name := range []string{"blob1", "blob2", "subdir"} {
		c.Assert(os.Remove(filepath.Join(fwParent, name)), IsNil)
	}
	boom := errors.New("boom: no space left on device")
	other := errors.New("boom: permission denied")
	restore := kernel.MockAtomicSymlink(func(target, linkPath string) error {
		switch filepath.Base(linkPath) {
		case "blob2":
			return boom
		case "subdir":
			return other
		}
		return osutil.AtomicSymlink(target, linkPath)
	})
	defer restore()

	err = kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, ErrorMatches, `(?s).*boom: no space left on device.*boom: permission denied.*`)
	// "blob1" must still have been fixed - the loop did not stop early.
	target, err := os.Readlink(filepath.Join(fwParent, "blob1"))
	c.Assert(err, IsNil)
	c.Check(target, Equals, filepath.Join(mountDir, "firmware", "blob1"))
	for _, name := range []string{"blob2", "subdir"} {
		_, err = os.Lstat(filepath.Join(fwParent, name))
		c.Check(errors.Is(err, fs.ErrNotExist), Equals, true)
	}
	c.Check(filepath.Join(destDir, "kernel.json.pending"), Not(testutil.FileAbsent))
	outdated, err := kernel.DriversTreeOutdated(destDir)
	c.Assert(err, IsNil)
	c.Check(outdated, Equals, true)
}

func (s *kernelDriversTestSuite) TestRegenerateLeavesNonEmptyLocalFirmwareDirAlone(c *C) {
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, IsNil)

	fwParent := filepath.Join(destDir, "lib", "firmware")

	// Simulate user-placed content at a spot the generator wants a
	// top-level firmware symlink: replace the "blob2" symlink with a
	// non-empty directory.
	blob2 := filepath.Join(fwParent, "blob2")
	c.Assert(os.Remove(blob2), IsNil)
	c.Assert(os.Mkdir(blob2, 0755), IsNil)
	userContent := filepath.Join(blob2, "user_placed_file")
	c.Assert(os.WriteFile(userContent, []byte("do not touch me"), 0644), IsNil)

	err = kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, ErrorMatches, `cannot replace unowned firmware entry .*blob2.*`)

	// The directory and its content must be left completely untouched.
	c.Check(osutil.FileExists(userContent), Equals, true)
	content, err := os.ReadFile(userContent)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "do not touch me")
	fi, err := os.Lstat(blob2)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)

	// The collision prevents a false completion marker. Existing firmware
	// remains intact.
	target, err := os.Readlink(filepath.Join(fwParent, "blob1"))
	c.Assert(err, IsNil)
	c.Check(target, Equals, filepath.Join(mountDir, "firmware", "blob1"))
}

func (s *kernelDriversTestSuite) TestRegenerateModulesReadErrorDoesNotAdvanceMarker(c *C) {
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, "5.15.0-78-generic", mountDir, createKernelSnapFilesOpts{})
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}
	c.Assert(kernel.EnsureKernelDriversTree(kMntPts, nil, destDir), IsNil)

	// Replace the directory with a regular file, so ReadDir fails with
	// ENOTDIR rather than reporting a legitimately absent modules directory.
	modsDir := filepath.Join(mountDir, "modules")
	c.Assert(os.Rename(modsDir, modsDir+"-away"), IsNil)
	c.Assert(os.WriteFile(modsDir, []byte("invalid"), 0644), IsNil)
	err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, NotNil)
	c.Check(errors.Is(err, syscall.ENOTDIR), Equals, true)
	outdated, err := kernel.DriversTreeOutdated(destDir)
	c.Assert(err, IsNil)
	// Construction failed before publishing a pending live update.
	c.Check(outdated, Equals, false)
	c.Check(filepath.Join(destDir, "kernel.json.pending"), testutil.FileAbsent)
	c.Check(osutil.FileExists(destDir+"_tmp"), Equals, false)
}

func (s *kernelDriversTestSuite) TestReconcileTopLevelFirmwareSymlinks(c *C) {
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, "5.15.0-78-generic", mountDir, createKernelSnapFilesOpts{})
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}
	c.Assert(kernel.EnsureKernelDriversTree(kMntPts, nil, destDir), IsNil)

	fwDir := filepath.Join(destDir, "lib", "firmware")
	// Top-level symlinks are managed regardless of who created them.
	blob := filepath.Join(fwDir, "blob2")
	c.Assert(os.Remove(blob), IsNil)
	c.Assert(os.Symlink("../custom-blob", blob), IsNil)
	staleLink := filepath.Join(fwDir, "custom-link")
	c.Assert(os.Symlink("/var/lib/firmware/vendor.bin", staleLink), IsNil)
	staleFile := filepath.Join(fwDir, "custom-file")
	c.Assert(os.WriteFile(staleFile, []byte("keep"), 0644), IsNil)
	userDir := filepath.Join(fwDir, "custom-dir")
	c.Assert(os.Mkdir(userDir, 0755), IsNil)
	c.Assert(os.Remove(filepath.Join(mountDir, "firmware", "ln_to_blob3")), IsNil)
	kernelLink := filepath.Join(fwDir, "blob1")
	c.Assert(os.Remove(kernelLink), IsNil)
	c.Assert(os.Symlink(filepath.Join(mountDir, "firmware", "old-blob"), kernelLink), IsNil)

	c.Assert(kernel.EnsureKernelDriversTree(kMntPts, nil, destDir), IsNil)
	for path, want := range map[string]string{
		blob:       filepath.Join(mountDir, "firmware", "blob2"),
		kernelLink: filepath.Join(mountDir, "firmware", "blob1"),
	} {
		got, err := os.Readlink(path)
		c.Assert(err, IsNil)
		c.Check(got, Equals, want)
	}
	c.Check(staleLink, testutil.FileAbsent)
	c.Check(filepath.Join(fwDir, "ln_to_blob3"), testutil.FileAbsent)
	c.Check(userDir, testutil.FilePresent)
	content, err := os.ReadFile(staleFile)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "keep")
}

func (s *kernelDriversTestSuite) TestReconcileModulesAndFirmwareUpdates(c *C) {
	mockCmd := testutil.MockCommand(c, "depmod", "")
	defer mockCmd.Restore()

	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	compMntDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/components/mnt/comp1/11")
	createKernelModulesCompFiles(c, kversion, compMntDir, "comp1")
	c.Assert(os.Symlink("comp1.bin", filepath.Join(compMntDir, "firmware", "alias.bin")), IsNil)
	kmodsCont := snap.MinimalComponentContainerPlaceInfo("comp1", snap.R(11), "pc-kernel")
	compsMntPts := []kernel.ModulesCompMountPoints{
		{"comp1", kernel.MountPoints{kmodsCont.MountDir(), kmodsCont.MountDir()}},
	}

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	err := kernel.EnsureKernelDriversTree(kMntPts, compsMntPts, destDir)
	c.Assert(err, IsNil)

	fwUpdatesDir := filepath.Join(destDir, "lib", "firmware", "updates")
	modsUpdatesDir := filepath.Join(destDir, "lib", "modules", kversion, "updates")

	// A user link occupying a component's desired name fails preflight;
	// it must not be overwritten merely because it is a symlink.
	compLink := filepath.Join(fwUpdatesDir, "comp1.bin")
	c.Assert(os.Remove(compLink), IsNil)
	c.Assert(os.Symlink("/unrelated/user.bin", compLink), IsNil)
	c.Assert(kernel.EnsureKernelDriversTree(kMntPts, compsMntPts, destDir), ErrorMatches, `cannot replace unowned firmware symlink .*comp1.bin.*`)
	c.Check(compLink, testutil.SymlinkTargetEquals, "/unrelated/user.bin")
	c.Check(filepath.Join(destDir, "kernel.json.pending"), testutil.FileAbsent)
	c.Assert(os.Remove(compLink), IsNil)

	userFile := filepath.Join(fwUpdatesDir, "user.bin")
	c.Assert(os.WriteFile(userFile, []byte("keep"), 0644), IsNil)
	userDir := filepath.Join(fwUpdatesDir, "user-dir")
	c.Assert(os.Mkdir(userDir, 0755), IsNil)
	userBlob := filepath.Join(userDir, "blob.bin")
	c.Assert(os.WriteFile(userBlob, []byte("nested"), 0644), IsNil)
	userLink := filepath.Join(fwUpdatesDir, "user-link")
	c.Assert(os.Symlink("/missing/user.bin", userLink), IsNil)
	staleDynamic := filepath.Join(fwUpdatesDir, "old-dynamic.bin")
	c.Assert(os.Symlink(filepath.Join(dirs.SnapDataDir, "pc-kernel", "1", "firmware", "old-dynamic.bin"), staleDynamic), IsNil)

	// Retarget a component's firmware link while retaining user entries.
	newCompMntDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/components/mnt/comp1/22")
	createKernelModulesCompFiles(c, kversion, newCompMntDir, "comp1")
	c.Assert(os.Symlink("comp1.bin", filepath.Join(newCompMntDir, "firmware", "alias.bin")), IsNil)
	compsMntPts[0].MountPoints = kernel.MountPoints{Current: newCompMntDir, Target: newCompMntDir}
	c.Assert(kernel.EnsureKernelDriversTree(kMntPts, compsMntPts, destDir), IsNil)
	c.Check(userFile, testutil.FileEquals, []byte("keep"))
	c.Check(userBlob, testutil.FileEquals, []byte("nested"))
	c.Check(userLink, testutil.SymlinkTargetEquals, "/missing/user.bin")
	c.Check(staleDynamic, testutil.FileAbsent)
	c.Check(compLink, testutil.SymlinkTargetEquals, filepath.Join(newCompMntDir, "firmware", "comp1.bin"))
	c.Check(filepath.Join(modsUpdatesDir, "comp1"), testutil.SymlinkTargetEquals, filepath.Join(newCompMntDir, "modules", kversion))

	// A removed component's absolute links disappear even while its old
	// mount still exists. Relative aliases cannot be identified and remain.
	c.Assert(kernel.EnsureKernelDriversTree(kMntPts, nil, destDir), IsNil)
	c.Check(filepath.Join(fwUpdatesDir, "comp1.bin"), testutil.FileAbsent)
	c.Check(filepath.Join(fwUpdatesDir, "alias.bin"), testutil.SymlinkTargetEquals, "comp1.bin")
	c.Check(userFile, testutil.FileEquals, []byte("keep"))
	c.Check(userBlob, testutil.FileEquals, []byte("nested"))
	c.Check(userLink, testutil.SymlinkTargetEquals, "/missing/user.bin")
	c.Check(filepath.Join(modsUpdatesDir, "comp1"), testutil.FileAbsent)
}

func (s *kernelDriversTestSuite) TestRegenerateSyncsAfterFirmwareFixBeforeMarker(c *C) {
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, IsNil)

	// Force the live firmware fix path to actually do something.
	fwParent := filepath.Join(destDir, "lib", "firmware")
	blob2 := filepath.Join(fwParent, "blob2")
	c.Assert(os.Remove(blob2), IsNil)

	// AtomicRename syncs the pending marker's directory before live writes;
	// the live repair still needs syncing before it is marked complete.
	pending := filepath.Join(destDir, "kernel.json.pending")
	var pendingBeforeLive, syncedLiveBeforeCompletion bool
	restoreLink := kernel.MockAtomicSymlink(func(target, linkPath string) error {
		if linkPath == blob2 {
			_, err := os.Lstat(pending)
			pendingBeforeLive = err == nil
		}
		return osutil.AtomicSymlink(target, linkPath)
	})
	defer restoreLink()
	restoreSync := kernel.MockDoSync(func() {
		if _, err := os.Lstat(pending); err == nil {
			if _, err := os.Readlink(blob2); err == nil {
				syncedLiveBeforeCompletion = true
			}
		}
	})
	defer restoreSync()

	err = kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, IsNil)
	c.Check(pendingBeforeLive, Equals, true)
	c.Check(syncedLiveBeforeCompletion, Equals, true)
}

func (s *kernelDriversTestSuite) TestKernelInstallMarkerWriteFailureDiscardsTree(c *C) {
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	boom := errors.New("boom: no space left on device")
	restore := kernel.MockAtomicWriteFile(func(string, []byte, os.FileMode, osutil.AtomicWriteFlags) error {
		return boom
	})
	defer restore()

	err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, Equals, boom)

	c.Check(osutil.FileExists(destDir), Equals, false)
}

func (s *kernelDriversTestSuite) TestRegenerateMarkerWriteFailureKeepsLiveTree(c *C) {
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, IsNil)
	completed, err := os.ReadFile(filepath.Join(destDir, "kernel.json"))
	c.Assert(err, IsNil)

	// Force the Regenerate path to find something to fix, so the live
	// swap/sync logic runs before the marker write.
	fwParent := filepath.Join(destDir, "lib", "firmware")
	blob2 := filepath.Join(fwParent, "blob2")
	c.Assert(os.Remove(blob2), IsNil)
	// Once both live subtrees have been updated, a later marker-write error
	// must not revert firmware updates alone.
	staleFw := filepath.Join(fwParent, "updates", "stale")
	c.Assert(os.WriteFile(staleFw, []byte("stale"), 0644), IsNil)
	modsFile := filepath.Join(destDir, "lib", "modules", kversion, "modules.symbols")
	c.Assert(os.WriteFile(modsFile, []byte("stale"), 0644), IsNil)

	boom := errors.New("boom: no space left on device")
	restore := kernel.MockAtomicWriteFile(func(string, []byte, os.FileMode, osutil.AtomicWriteFlags) error {
		return boom
	})
	defer restore()

	// A pending marker write failure leaves the live tree untouched.
	err = kernel.EnsureKernelDriversTree(kMntPts, nil, destDir)
	c.Assert(err, Equals, boom)

	// Neither the prior final marker nor live children have changed.
	c.Check(osutil.FileExists(destDir), Equals, true)
	unchanged, err := os.ReadFile(filepath.Join(destDir, "kernel.json"))
	c.Assert(err, IsNil)
	c.Check(unchanged, DeepEquals, completed)
	c.Check(filepath.Join(destDir, "kernel.json.pending"), testutil.FileAbsent)
	_, err = os.Readlink(blob2)
	c.Check(os.IsNotExist(err), Equals, true)
	content, err := os.ReadFile(staleFw)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "stale")
	content, err = os.ReadFile(modsFile)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "stale")

	// The _tmp scratch copy must not linger around.
	c.Check(osutil.FileExists(destDir+"_tmp"), Equals, false)
}
