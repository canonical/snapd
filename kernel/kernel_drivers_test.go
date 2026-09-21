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
	"fmt"
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
	"github.com/snapcore/snapd/logger"
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
	changed := testBuildKernelDriversTree(c, createKernelSnapFilesOpts{})
	c.Check(changed, Equals, true)
	changed = testBuildKernelDriversTree(c, createKernelSnapFilesOpts{})
	c.Check(changed, Equals, false)

	// Now remove and check
	treeRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1")
	kernel.RemoveKernelDriversTree(treeRoot)
	c.Assert(osutil.FileExists(treeRoot), Equals, false)
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversTreeWithUpdates(c *C) {
	changed := testBuildKernelDriversTree(c, createKernelSnapFilesOpts{withFwUpdatesDir: true})
	c.Check(changed, Equals, true)

	// Now remove and check
	treeRoot := filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", "pc-kernel", "1")
	kernel.RemoveKernelDriversTree(treeRoot)
	c.Assert(osutil.FileExists(treeRoot), Equals, false)
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversTreeAlreadyInstalledNoChange(c *C) {
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	kversion := "5.15.0-78-generic"
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	opts := &kernel.KernelDriversTreeOptions{KernelInstall: true}
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	changed, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir, opts)
	c.Assert(err, IsNil)
	c.Check(changed, Equals, true)

	// Second call finds the tree already present and does nothing.
	changed, err = kernel.EnsureKernelDriversTree(kMntPts, nil, destDir, opts)
	c.Assert(err, IsNil)
	c.Check(changed, Equals, false)
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

func testBuildKernelDriversTree(c *C, opts createKernelSnapFilesOpts) bool {
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	kversion := "5.15.0-78-generic"
	createKernelSnapFiles(c, kversion, mountDir, opts)

	// Now build the tree
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	changed, err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir},
		nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
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
	v, err := kernel.ReadDriversTreeGeneratorVersion(markerDir)
	c.Assert(err, IsNil)
	c.Check(v, Equals, kernel.KernelDriversTreeGeneratorVersion())

	return changed
}

func (s *kernelDriversTestSuite) TestBuildKernelDriversNoModsOrFw(c *C) {
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/11")
	createKernelSnapFilesOnlyModules(c, kversion, mountDir)

	// Build the tree should not fail
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(11))
	_, err := kernel.EnsureKernelDriversTree(
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
	_, err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir}, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
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
	_, err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir}, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
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
	_, err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir}, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
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
	_, err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir}, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
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
	_, err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  kTargetDir}, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
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
	_, err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  kTargetDir}, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
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
	_, err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  kTargetDir}, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
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

func (s *kernelDriversTestSuite) TestBuildKernelDriversOnlyFw(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	createKernelSnapFilesOnlyFw(c, mountDir)

	// Build the tree should not fail
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	_, err := kernel.EnsureKernelDriversTree(
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
	_, err := kernel.EnsureKernelDriversTree(
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
	_, err := kernel.EnsureKernelDriversTree(
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
	_, err := kernel.EnsureKernelDriversTree(
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
	_, err := kernel.EnsureKernelDriversTree(
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

	// This scenario (a kernel-modules-component-only call for a kernel
	// that was never installed at all, i.e. destDir does not exist yet in
	// any form) never happens in real operation: components always attach
	// to an already-linked, already-installed kernel snap. It used to fail
	// here specifically because osutil.SwapDirs (RENAME_EXCHANGE) requires
	// both sides to exist, and neither the live modules nor the live
	// firmware "updates" directory existed. Both of those swaps now
	// tolerate a missing live directory by falling back to a plain move
	// (see EnsureKernelDriversTree), the same fix already applied for
	// modules being reused for firmware "updates" too - so this no longer
	// fails, it just builds everything from scratch instead.
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	_, err := kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir},
		compsMntPts, destDir, &kernel.KernelDriversTreeOptions{KernelInstall: false})
	c.Assert(err, IsNil)

	// Components are correctly wired in, in the modules subtree (a blind
	// symlink to wherever the component mount points are, regardless of
	// whether the ad-hoc test fixture above actually populated content
	// there - unlike firmware, this does not need to read anything from
	// the component mount to create the symlink).
	modsUpdatesDir := filepath.Join(destDir, "lib", "modules", kversion, "updates")
	_, err = os.Readlink(filepath.Join(modsUpdatesDir, "comp1"))
	c.Check(err, IsNil)

	// The firmware "updates" directory itself always gets created too
	// (needed regardless of whether there is any component firmware
	// content to link in right now).
	fwUpdatesDir := filepath.Join(destDir, "lib", "firmware", "updates")
	c.Check(osutil.IsDirectory(fwUpdatesDir), Equals, true)

	// Top-level firmware symlinks, however, are only ever created when
	// opts.KernelInstall is true (see createFirmwareSymlinks's call site in
	// EnsureKernelDriversTree) - this call did not request that, so they
	// are correctly absent. This is the one real gap in a scenario that
	// should not occur in practice anyway.
	c.Check(osutil.FileExists(filepath.Join(destDir, "lib", "firmware", "blob1")), Equals, false)
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
	_, err = kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: mountDir,
			Target:  mountDir},
		compsMntPts, destDir, opts)
	c.Assert(err, IsNil)

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
	_, err := kernel.EnsureKernelDriversTree(
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

	v, err := kernel.ReadDriversTreeGeneratorVersion(destDir)
	c.Assert(err, IsNil)
	c.Assert(v, Equals, kernel.KernelDriversTreeGeneratorVersion())

	// The marker file lives inside destDir, so it is cleaned up by
	// RemoveKernelDriversTree's existing os.RemoveAll.
	c.Assert(osutil.FileExists(filepath.Join(destDir, "kernel.json")), Equals, true)
}

func (s *kernelDriversTestSuite) TestDriversTreeNeedsCheckMissingMarker(c *C) {
	destDir := c.MkDir()

	needsCheck, err := kernel.DriversTreeNeedsCheck(destDir)
	c.Assert(err, IsNil)
	c.Assert(needsCheck, Equals, true)
}

func (s *kernelDriversTestSuite) TestDriversTreeNeedsCheckUpToDate(c *C) {
	destDir := c.MkDir()

	c.Assert(kernel.WriteDriversTreeMeta(destDir), IsNil)

	needsCheck, err := kernel.DriversTreeNeedsCheck(destDir)
	c.Assert(err, IsNil)
	c.Assert(needsCheck, Equals, false)
}

func (s *kernelDriversTestSuite) TestDriversTreeNeedsCheckForwardOnly(c *C) {
	destDir := c.MkDir()

	// Simulate a tree built by a newer generator than what is currently
	// running (e.g. after a snapd revert): the marker records a version
	// higher than the current constant.
	restore := kernel.MockKernelDriversTreeGeneratorVersion(100)
	c.Assert(kernel.WriteDriversTreeMeta(destDir), IsNil)
	restore()

	needsCheck, err := kernel.DriversTreeNeedsCheck(destDir)
	c.Assert(err, IsNil)
	c.Assert(needsCheck, Equals, false)
}

func (s *kernelDriversTestSuite) TestDriversTreeNeedsCheckCorruptMarker(c *C) {
	destDir := c.MkDir()

	c.Assert(os.WriteFile(filepath.Join(destDir, "kernel.json"), []byte("not json"), 0644), IsNil)

	needsCheck, err := kernel.DriversTreeNeedsCheck(destDir)
	c.Assert(err, IsNil)
	c.Assert(needsCheck, Equals, true)
}

func (s *kernelDriversTestSuite) TestComponentOnlyChangeDoesNotAdvanceMarker(c *C) {
	// Regression test: a plain kernel-modules-component-only change
	// (KernelInstall: false, Regenerate: false) never checks/fixes
	// top-level lib/firmware symlinks (see syncFirmwareTopLevelSymlinks,
	// only run for opts.Regenerate), so it must not be allowed to advance
	// the generator-version marker: doing so would let an unrelated
	// component change permanently mask a still-pending, unrelated need
	// for a real Regenerate pass.
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	_, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	// Simulate a tree that has never been through a full Regenerate pass
	// (e.g. built by a snapd predating this mechanism, or simply not
	// checked yet): remove the marker entirely.
	markerPath := filepath.Join(destDir, "kernel.json")
	c.Assert(os.Remove(markerPath), IsNil)

	compMntDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/components/mnt/comp1/11")
	createKernelModulesCompFiles(c, kversion, compMntDir, "comp1")
	kmodsCont := snap.MinimalComponentContainerPlaceInfo("comp1", snap.R(11), "pc-kernel")
	compsMntPts := []kernel.ModulesCompMountPoints{
		{"comp1", kernel.MountPoints{kmodsCont.MountDir(), kmodsCont.MountDir()}},
	}
	mockCmd := testutil.MockCommand(c, "depmod", "")
	defer mockCmd.Restore()

	_, err = kernel.EnsureKernelDriversTree(kMntPts, compsMntPts, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: false})
	c.Assert(err, IsNil)

	c.Check(osutil.FileExists(markerPath), Equals, false)

	needsCheck, err := kernel.DriversTreeNeedsCheck(destDir)
	c.Assert(err, IsNil)
	c.Check(needsCheck, Equals, true)
}

func inodeOf(c *C, path string) uint64 {
	st, err := os.Stat(path)
	c.Assert(err, IsNil)
	sys, ok := st.Sys().(*syscall.Stat_t)
	c.Assert(ok, Equals, true)
	return sys.Ino
}

func (s *kernelDriversTestSuite) TestRegenerateAlreadyCorrectStillSwaps(c *C) {
	// Regenerate no longer compares the freshly-built candidate against
	// what is live before deciding whether to swap it in: it always does,
	// whenever it is triggered at all (a rebuild is always warranted, see
	// the comment above the modules swap/move in EnsureKernelDriversTree).
	// This test pins down that, even when the live tree already matches
	// what a fresh build would produce, a Regenerate call still reports a
	// change and still swaps the <kversion> directory (verified elsewhere,
	// e.g. TestRegenerateRestoresModinfoFile, by content), while leaving
	// the lib/modules and lib/firmware directories themselves - the
	// bind-mount sources - untouched.
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	changed, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)
	c.Check(changed, Equals, true)

	modsParent := filepath.Join(destDir, "lib", "modules")
	fwParent := filepath.Join(destDir, "lib", "firmware")
	modsParentInode := inodeOf(c, modsParent)
	fwParentInode := inodeOf(c, fwParent)

	changed, err = kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{Regenerate: true})
	c.Assert(err, IsNil)
	c.Check(changed, Equals, true)

	// Marker gets (re)written.
	v, err := kernel.ReadDriversTreeGeneratorVersion(destDir)
	c.Assert(err, IsNil)
	c.Check(v, Equals, kernel.KernelDriversTreeGeneratorVersion())

	// lib/modules and lib/firmware themselves are bind-mount sources and
	// must never be renamed/recreated: only their children may be
	// swapped/replaced.
	c.Check(inodeOf(c, modsParent), Equals, modsParentInode)
	c.Check(inodeOf(c, fwParent), Equals, fwParentInode)
}

func (s *kernelDriversTestSuite) TestRegenerateMissingLiveModulesKversionDir(c *C) {
	// Regression test for the discovery that osutil.SwapDirs
	// (RENAME_EXCHANGE) requires both sides to exist: the now-removed
	// modules-tree comparison used to error out when the live directory
	// was missing; EnsureKernelDriversTree must instead fall back to a
	// plain move when the live lib/modules/<kversion> directory does not
	// exist at all.
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	_, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	// Remove only the <kversion> child; lib/modules itself still exists.
	modsKversionDir := filepath.Join(destDir, "lib", "modules", kversion)
	c.Assert(os.RemoveAll(modsKversionDir), IsNil)

	changed, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{Regenerate: true})
	c.Assert(err, IsNil)
	c.Check(changed, Equals, true)

	// The directory is recreated with content matching a fresh build.
	modinfo, err := os.ReadFile(filepath.Join(modsKversionDir, "modules.symbols"))
	c.Assert(err, IsNil)
	src, err := os.ReadFile(filepath.Join(mountDir, "modules", kversion, "modules.symbols"))
	c.Assert(err, IsNil)
	c.Check(modinfo, DeepEquals, src)

	target, err := os.Readlink(filepath.Join(modsKversionDir, "kernel"))
	c.Assert(err, IsNil)
	c.Check(target, Equals, filepath.Join(mountDir, "modules", kversion, "kernel"))
}

func (s *kernelDriversTestSuite) TestRegenerateMissingLiveModulesParentDir(c *C) {
	// Same as above, but the whole lib/modules parent directory is missing,
	// covering the second failure shape from the original review comment.
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	_, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	// Remove lib/modules itself, not just the <kversion> child.
	modsParent := filepath.Join(destDir, "lib", "modules")
	c.Assert(os.RemoveAll(modsParent), IsNil)

	changed, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{Regenerate: true})
	c.Assert(err, IsNil)
	c.Check(changed, Equals, true)

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

	_, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	modsParent := filepath.Join(destDir, "lib", "modules")
	modsParentInode := inodeOf(c, modsParent)

	// Simulate a kernel mount that now exposes a vendor directory that
	// was not there (or not linked) when the live tree was built.
	newVendorDir := filepath.Join(mountDir, "modules", kversion, "newvendor")
	c.Assert(os.MkdirAll(newVendorDir, 0755), IsNil)

	changed, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{Regenerate: true})
	c.Assert(err, IsNil)
	c.Check(changed, Equals, true)

	newLink := filepath.Join(modsParent, kversion, "newvendor")
	target, err := os.Readlink(newLink)
	c.Assert(err, IsNil)
	c.Check(target, Equals, newVendorDir)

	// The lib/modules directory itself must never be renamed/recreated:
	// it is a bind-mount source, only its children may be swapped.
	c.Check(inodeOf(c, modsParent), Equals, modsParentInode)
}

func (s *kernelDriversTestSuite) TestRegenerateRestoresModinfoFile(c *C) {
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	_, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	modsParent := filepath.Join(destDir, "lib", "modules")
	modsParentInode := inodeOf(c, modsParent)

	// Delete a modprobe data file (a regular file copied from the kernel
	// snap by createModulesSubtree) from the live tree; it must be
	// restored from the kernel snap content on regenerate. With no
	// components installed depmod does not run, so the restored content
	// must match the kernel snap's copy exactly.
	modinfoPath := filepath.Join(modsParent, kversion, "modules.symbols")
	c.Assert(os.Remove(modinfoPath), IsNil)

	changed, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{Regenerate: true})
	c.Assert(err, IsNil)
	c.Check(changed, Equals, true)

	restored, err := os.ReadFile(modinfoPath)
	c.Assert(err, IsNil)
	src, err := os.ReadFile(filepath.Join(mountDir, "modules", kversion, "modules.symbols"))
	c.Assert(err, IsNil)
	c.Check(restored, DeepEquals, src)

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

	_, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	// Corrupt (rather than delete) a modprobe data file in the live
	// tree; Regenerate always rebuilds the tree unconditionally (there is
	// no comparison step), so the corrupted file must be restored
	// regardless of whether it "looks different" from what was there.
	modinfoPath := filepath.Join(destDir, "lib", "modules", kversion, "modules.symbols")
	c.Assert(os.WriteFile(modinfoPath, []byte("corrupted"), 0644), IsNil)

	changed, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{Regenerate: true})
	c.Assert(err, IsNil)
	c.Check(changed, Equals, true)

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

	_, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	fwParent := filepath.Join(destDir, "lib", "firmware")

	// Make both "blob1" and "blob2" need replacing.
	c.Assert(os.Remove(filepath.Join(fwParent, "blob1")), IsNil)
	c.Assert(os.Remove(filepath.Join(fwParent, "blob2")), IsNil)

	// Simulate a genuine, unrelated failure (e.g. ENOSPC, EROFS) for
	// "blob2" specifically, while "blob1" (and everything else) succeeds
	// normally - there is no scratch copy to roll back to for firmware
	// entries, so the best-effort contract is: keep going, still fix
	// whatever can be fixed, and report the failure at the end rather than
	// stopping partway through and leaving otherwise-fixable entries broken.
	boom := errors.New("boom: no space left on device")
	restore := kernel.MockAtomicSymlink(func(target, linkPath string) error {
		if filepath.Base(linkPath) == "blob2" {
			return boom
		}
		return osutil.AtomicSymlink(target, linkPath)
	})
	defer restore()

	changed, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{Regenerate: true})
	// The failure must still be visible to the caller (so
	// EnsureKernelDriversTree does not write the generator-version marker
	// over a tree that was not actually fully fixed).
	c.Assert(err, ErrorMatches, ".*boom: no space left on device.*")
	// But "blob1" (which comes before "blob2" and does not fail) must still
	// have been fixed - the loop must not have stopped at the first error.
	c.Check(changed, Equals, true)
	target, err := os.Readlink(filepath.Join(fwParent, "blob1"))
	c.Assert(err, IsNil)
	c.Check(target, Equals, filepath.Join(mountDir, "firmware", "blob1"))
	// "blob2" itself is left missing (the mocked failure prevented it from
	// being recreated), confirming the mock actually exercised the failure
	// path rather than silently succeeding anyway.
	_, err = os.Lstat(filepath.Join(fwParent, "blob2"))
	c.Check(errors.Is(err, fs.ErrNotExist), Equals, true)
}

func (s *kernelDriversTestSuite) TestRegenerateMissingTopLevelFirmwareSymlink(c *C) {
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	_, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	fwParent := filepath.Join(destDir, "lib", "firmware")
	fwParentInode := inodeOf(c, fwParent)

	// Simulate a live tree missing an entry the kernel mount has (e.g. a
	// pre-fix tree, or corruption): remove the "blob2" top-level symlink.
	c.Assert(os.Remove(filepath.Join(fwParent, "blob2")), IsNil)

	changed, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{Regenerate: true})
	c.Assert(err, IsNil)
	c.Check(changed, Equals, true)

	target, err := os.Readlink(filepath.Join(fwParent, "blob2"))
	c.Assert(err, IsNil)
	c.Check(target, Equals, filepath.Join(mountDir, "firmware", "blob2"))

	// lib/firmware itself must never be renamed/recreated: it is a
	// bind-mount source, only its children may be swapped/replaced.
	c.Check(inodeOf(c, fwParent), Equals, fwParentInode)
}

func (s *kernelDriversTestSuite) TestRegenerateLeavesNonEmptyLocalFirmwareDirAlone(c *C) {
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	_, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	fwParent := filepath.Join(destDir, "lib", "firmware")

	// Simulate the user (or some other tool) having placed real content
	// directly on the live tree, at a spot the generator wants to place a
	// top-level firmware symlink: replace the "blob2" symlink with a
	// non-empty directory.
	blob2 := filepath.Join(fwParent, "blob2")
	c.Assert(os.Remove(blob2), IsNil)
	c.Assert(os.Mkdir(blob2, 0755), IsNil)
	userContent := filepath.Join(blob2, "user_placed_file")
	c.Assert(os.WriteFile(userContent, []byte("do not touch me"), 0644), IsNil)

	logBuf, restore := logger.MockLogger()
	defer restore()

	changed, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{Regenerate: true})
	c.Assert(err, IsNil)
	// The blocked firmware entry itself does not count as a change, but
	// modules are now always rebuilt/swapped in whenever Regenerate is
	// triggered at all (see EnsureKernelDriversTree), so overall this
	// still reports a change - but, crucially, no error was returned
	// either (checked above).
	c.Check(changed, Equals, true)

	// The directory and its content must be left completely untouched.
	c.Check(osutil.FileExists(userContent), Equals, true)
	content, err := os.ReadFile(userContent)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "do not touch me")
	fi, err := os.Lstat(blob2)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)

	c.Check(logBuf.String(), Matches, fmt.Sprintf(`(?s).*skipping a non-symlink entry %q in firmware directory.*\n`, blob2))

	// The unrelated "blob1" entry must still be created/updated correctly:
	// this one blocked entry must not abort processing of the rest.
	target, err := os.Readlink(filepath.Join(fwParent, "blob1"))
	c.Assert(err, IsNil)
	c.Check(target, Equals, filepath.Join(mountDir, "firmware", "blob1"))
}

func (s *kernelDriversTestSuite) TestRegenerateLeavesEmptyLocalFirmwareDirAlone(c *C) {
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	_, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	fwParent := filepath.Join(destDir, "lib", "firmware")

	// Same as the non-empty case above, but this time the stray directory
	// sitting where we want to place a top-level firmware symlink is
	// itself empty. Behavior change: this used to be silently removed and
	// replaced (a plain os.Remove succeeds on an empty directory); with an
	// atomic symlink replace via rename(2), placing a symlink over *any*
	// pre-existing directory - empty or not - fails with EEXIST, so an
	// empty directory is now left alone too, exactly like a non-empty one.
	blob2 := filepath.Join(fwParent, "blob2")
	c.Assert(os.Remove(blob2), IsNil)
	c.Assert(os.Mkdir(blob2, 0755), IsNil)

	logBuf, restore := logger.MockLogger()
	defer restore()

	changed, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{Regenerate: true})
	c.Assert(err, IsNil)
	// The blocked firmware entry itself does not count as a change, but
	// modules are now always rebuilt/swapped in whenever Regenerate is
	// triggered at all (see EnsureKernelDriversTree), so overall this
	// still reports a change.
	c.Check(changed, Equals, true)

	// The empty directory must be left completely untouched (not removed,
	// not replaced with a symlink).
	fi, err := os.Lstat(blob2)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	entries, err := os.ReadDir(blob2)
	c.Assert(err, IsNil)
	c.Check(entries, HasLen, 0)

	c.Check(logBuf.String(), Matches, fmt.Sprintf(`(?s).*skipping a non-symlink entry %q in firmware directory.*\n`, blob2))

	// The unrelated "blob1" entry must still be created/updated correctly:
	// this one blocked entry must not abort processing of the rest.
	target, err := os.Readlink(filepath.Join(fwParent, "blob1"))
	c.Assert(err, IsNil)
	c.Check(target, Equals, filepath.Join(mountDir, "firmware", "blob1"))
}

func (s *kernelDriversTestSuite) TestRegenerateNoModulesDir(c *C) {
	mountDir := filepath.Join(dirs.RunDir, "mnt/pc-kernel")
	createKernelSnapFilesOnlyFw(c, mountDir)

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	_, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	// A kernel with no modules/ directory at all is valid and must not
	// break Regenerate mode: firmware is still processed and the marker
	// is still written. changed is still true: the firmware "updates"
	// subtree is unconditionally rebuilt/swapped regardless of whether
	// there are any modules at all (see EnsureKernelDriversTree).
	changed, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{Regenerate: true})
	c.Assert(err, IsNil)
	c.Check(changed, Equals, true)

	fwPath := filepath.Join(destDir, "lib", "firmware", "wifi_fw.bin")
	c.Assert(osutil.IsSymlink(fwPath), Equals, true)

	v, err := kernel.ReadDriversTreeGeneratorVersion(destDir)
	c.Assert(err, IsNil)
	c.Check(v, Equals, kernel.KernelDriversTreeGeneratorVersion())
}

func (s *kernelDriversTestSuite) TestRegenerateRemovesStaleTopLevelFirmwareSymlink(c *C) {
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	_, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	fwParent := filepath.Join(destDir, "lib", "firmware")

	// Simulate a stale entry left behind by an older, buggy generator:
	// something that no longer corresponds to anything in the kernel
	// mount's firmware/ directory.
	staleLink := filepath.Join(fwParent, "stale_vendor")
	c.Assert(os.Symlink(filepath.Join(mountDir, "firmware", "gone"), staleLink), IsNil)

	changed, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{Regenerate: true})
	c.Assert(err, IsNil)
	c.Check(changed, Equals, true)

	// osutil.FileExists follows symlinks (via os.Stat), so it would report
	// "false" for this dangling symlink regardless of whether it was
	// actually removed - use os.Lstat on the symlink itself instead, which
	// does not follow it, to actually prove removal.
	_, err = os.Lstat(staleLink)
	c.Check(errors.Is(err, fs.ErrNotExist), Equals, true)
}

func (s *kernelDriversTestSuite) TestRegenerateMissingLiveFirmwareUpdatesDir(c *C) {
	// Regression test mirroring TestRegenerateMissingLiveModulesKversionDir:
	// osutil.SwapDirs (RENAME_EXCHANGE) requires both sides to exist, and
	// since the firmware "updates" swap is now unconditional (see
	// EnsureKernelDriversTree), it needs the same missing-live-directory
	// fallback the modules subtree already has.
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	_, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	// Remove the live lib/firmware/updates directory entirely (e.g.
	// external tampering, a partially generated tree).
	fwUpdatesDir := filepath.Join(destDir, "lib", "firmware", "updates")
	c.Assert(os.RemoveAll(fwUpdatesDir), IsNil)

	changed, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{Regenerate: true})
	c.Assert(err, IsNil)
	c.Check(changed, Equals, true)

	// The directory is recreated (empty, since there are no components in
	// this test, but present - required so future component swaps have
	// somewhere to swap into, see EnsureKernelDriversTree).
	fi, err := os.Lstat(fwUpdatesDir)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)

	v, err := kernel.ReadDriversTreeGeneratorVersion(destDir)
	c.Assert(err, IsNil)
	c.Check(v, Equals, kernel.KernelDriversTreeGeneratorVersion())
}

func (s *kernelDriversTestSuite) TestRegenerateRebuildsBothModulesAndFirmwareUpdates(c *C) {
	mockCmd := testutil.MockCommand(c, "depmod", "")
	defer mockCmd.Restore()

	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	compMntDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/components/mnt/comp1/11")
	createKernelModulesCompFiles(c, kversion, compMntDir, "comp1")
	kmodsCont := snap.MinimalComponentContainerPlaceInfo("comp1", snap.R(11), "pc-kernel")
	compsMntPts := []kernel.ModulesCompMountPoints{
		{"comp1", kernel.MountPoints{kmodsCont.MountDir(), kmodsCont.MountDir()}},
	}

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	_, err := kernel.EnsureKernelDriversTree(kMntPts, compsMntPts, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	fwUpdatesDir := filepath.Join(destDir, "lib", "firmware", "updates")
	modsUpdatesDir := filepath.Join(destDir, "lib", "modules", kversion, "updates")

	// Simulate a stale entry in the firmware "updates" subtree left behind
	// by an older/buggy generator, or by a component that is no longer
	// active: something that does not correspond to any of the given
	// compsMntPts.
	staleMarker := filepath.Join(fwUpdatesDir, "stale_marker")
	c.Assert(os.WriteFile(staleMarker, []byte("x"), 0644), IsNil)

	changed, err := kernel.EnsureKernelDriversTree(kMntPts, compsMntPts, destDir,
		&kernel.KernelDriversTreeOptions{Regenerate: true})
	c.Assert(err, IsNil)
	c.Check(changed, Equals, true)

	// Both "updates" subtrees are unconditionally rebuilt/swapped in
	// Regenerate mode from the given compsMntPts (see
	// EnsureKernelDriversTree): a fix to component/dynamic-modules firmware
	// generation must actually be applied here too, not silently discarded
	// (a review-confirmed gap in an earlier version of this code, where
	// firmware "updates" was left untouched in Regenerate mode while
	// modules "updates" was not - an inconsistency with no principled
	// reason behind it). The stale entry above does not survive the
	// rebuild.
	c.Check(osutil.FileExists(staleMarker), Equals, false)

	target, err := os.Readlink(filepath.Join(fwUpdatesDir, "comp1.bin"))
	c.Assert(err, IsNil)
	c.Check(target, Equals, filepath.Join(compMntDir, "firmware", "comp1.bin"))

	// The modules "updates" subtree, correctly re-derived from the given
	// compsMntPts (the same components as originally installed) as before.
	target, err = os.Readlink(filepath.Join(modsUpdatesDir, "comp1"))
	c.Assert(err, IsNil)
	c.Check(target, Equals, filepath.Join(compMntDir, "modules", kversion))
}

func (s *kernelDriversTestSuite) TestRegenerateSyncsAfterFirmwareFixBeforeMarker(c *C) {
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	_, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	// Force the live firmware fix path to actually do something.
	fwParent := filepath.Join(destDir, "lib", "firmware")
	blob2 := filepath.Join(fwParent, "blob2")
	c.Assert(os.Remove(blob2), IsNil)

	// Record, at each doSync call, whether the firmware fix (recreating
	// the "blob2" symlink) has already happened - this pins down the
	// ordering deterministically, without relying on wall-clock/mtime
	// comparisons.
	var fwFixedAtSync []bool
	restore := kernel.MockDoSync(func() {
		_, err := os.Readlink(blob2)
		fwFixedAtSync = append(fwFixedAtSync, err == nil)
	})
	defer restore()

	changed, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{Regenerate: true})
	c.Assert(err, IsNil)
	c.Check(changed, Equals, true)

	// One sync after building the candidate tree, one after the modules
	// swap decision (neither of which has touched firmware yet), and one
	// more - the fix under test - after the live firmware fix, before the
	// marker gets written.
	c.Assert(fwFixedAtSync, DeepEquals, []bool{false, false, true})
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

	// Current, intentionally-kept behavior on the fresh-install path: a
	// marker-write failure is fatal and discards the entire freshly-built
	// tree, even though the tree content itself (modules/firmware) was
	// already correctly built by this point. See the comment above the
	// writeDriversTreeMeta call in the KernelInstall branch of
	// EnsureKernelDriversTree for the rationale.
	_, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, Equals, boom)

	c.Check(osutil.FileExists(destDir), Equals, false)
}

func (s *kernelDriversTestSuite) TestRegenerateMarkerWriteFailureKeepsLiveTree(c *C) {
	kversion := "5.15.0-78-generic"
	mountDir := filepath.Join(dirs.SnapMountDir, "pc-kernel/1")
	createKernelSnapFiles(c, kversion, mountDir, createKernelSnapFilesOpts{})

	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, "pc-kernel", snap.R(1))
	kMntPts := kernel.MountPoints{Current: mountDir, Target: mountDir}

	_, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
	c.Assert(err, IsNil)

	// Force the Regenerate path to actually find something to fix, so the
	// live tree swap/sync logic runs before the marker write is reached.
	fwParent := filepath.Join(destDir, "lib", "firmware")
	blob2 := filepath.Join(fwParent, "blob2")
	c.Assert(os.Remove(blob2), IsNil)

	boom := errors.New("boom: no space left on device")
	restore := kernel.MockAtomicWriteFile(func(string, []byte, os.FileMode, osutil.AtomicWriteFlags) error {
		return boom
	})
	defer restore()

	// Current, intentionally-kept behavior on the Regenerate path: a
	// marker-write failure only discards the _tmp scratch copy (per the
	// deferred cleanup), leaving the already-live, already-correct tree
	// untouched. It still surfaces as an error to the caller. See the
	// comment above this writeDriversTreeMeta call in EnsureKernelDriversTree
	// for the rationale.
	changed, err := kernel.EnsureKernelDriversTree(kMntPts, nil, destDir,
		&kernel.KernelDriversTreeOptions{Regenerate: true})
	c.Assert(err, Equals, boom)
	// changed is still reported accurately: the live fix did happen.
	c.Check(changed, Equals, true)

	// The live tree survives, and the firmware fix that was made live
	// before the marker write was attempted is still in place.
	c.Check(osutil.FileExists(destDir), Equals, true)
	target, err := os.Readlink(blob2)
	c.Assert(err, IsNil)
	c.Check(target, Equals, filepath.Join(mountDir, "firmware", "blob2"))

	// The _tmp scratch copy must not linger around.
	c.Check(osutil.FileExists(destDir+"_tmp"), Equals, false)
}
