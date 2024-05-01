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

package kernel

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"syscall"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// For testing purposes
var osSymlink = os.Symlink

// We expect as a minimum something that starts with three numbers
// separated by dots for the kernel version.
var utsRelease = regexp.MustCompile(`^([0-9]+\.){2}[0-9]+`)

// KernelVersionFromModulesDir returns the kernel version for a mounted kernel
// snap (this would be the output if "uname -r" for a running kernel). It
// assumes that there is a folder named modules/$(uname -r) inside the snap.
func KernelVersionFromModulesDir(mountPoint string) (string, error) {
	modsDir := filepath.Join(mountPoint, "modules")
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return "", err
	}

	kversion := ""
	for _, node := range entries {
		if !node.Type().IsDir() {
			continue
		}
		if !utsRelease.MatchString(node.Name()) {
			continue
		}
		if kversion != "" {
			return "", fmt.Errorf("more than one modules directory in %q", modsDir)
		}
		kversion = node.Name()
	}
	if kversion == "" {
		return "", fmt.Errorf("no modules directory found in %q", modsDir)
	}

	return kversion, nil
}

func createFirmwareSymlinks(fwMount, fwDest string) error {
	fwOrig := filepath.Join(fwMount, "firmware")
	if err := os.MkdirAll(fwDest, 0755); err != nil {
		return err
	}

	// Symbolic links inside firmware folder - it cannot be directly a
	// symlink to "firmware" as we will use firmware/updates/ subfolder for
	// components.
	entries, err := os.ReadDir(fwOrig)
	if err != nil {
		if os.IsNotExist(err) {
			// Bit of a corner case, but maybe possible. Log anyway.
			logger.Noticef("no firmware found in %q", fwOrig)
			return nil
		}
		return err
	}

	for _, node := range entries {
		lpath := filepath.Join(fwDest, node.Name())
		origPath := filepath.Join(fwOrig, node.Name())
		switch node.Type() {
		case 0, fs.ModeDir:
			// Create link for regular files or directories
			if err := os.Symlink(origPath, lpath); err != nil {
				return err
			}
		case fs.ModeSymlink:
			// Replicate link (it should be relative)
			// TODO check this in snap pack
			lpath := filepath.Join(fwDest, node.Name())
			dest, err := os.Readlink(origPath)
			if err != nil {
				return err
			}
			if filepath.IsAbs(dest) {
				return fmt.Errorf("symlink %q points to absolute path %q", lpath, dest)
			}
			if err := os.Symlink(dest, lpath); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%q has unexpected file type: %s",
				node.Name(), node.Type())
		}
	}

	return nil
}

func createModulesSubtree(kernelMount, kernelTree, kversion, kname string, krev snap.Revision, compInfos []*snap.ComponentSideInfo) error {
	// Although empty we need "lib" because "depmod" always appends
	// "/lib/modules/<kernel_version>" to the directory passed with option
	// "-b".
	modsRoot := filepath.Join(kernelTree, "lib", "modules", kversion)
	if err := os.MkdirAll(modsRoot, 0755); err != nil {
		return err
	}

	// Copy modinfo files from the snap (these might be overwritten if
	// kernel-modules components are installed).
	modsGlob := filepath.Join(kernelMount, "modules", kversion, "modules.*")
	modFiles, err := filepath.Glob(modsGlob)
	if err != nil {
		// Should not really happen (only possible error is ErrBadPattern)
		return err
	}
	for _, orig := range modFiles {
		target := filepath.Join(modsRoot, filepath.Base(orig))
		if err := osutil.CopyFile(orig, target, osutil.CopyFlagDefault); err != nil {
			return err
		}
	}

	// Symbolic links to early mount kernel snap
	earlyMntDir := filepath.Join(kernelMount, "modules", kversion)
	for _, d := range []string{"kernel", "vdso"} {
		lname := filepath.Join(modsRoot, d)
		to := filepath.Join(earlyMntDir, d)
		if err := osSymlink(to, lname); err != nil {
			return err
		}
	}

	// If necessary, add modules from components and run depmod
	return setupModsFromComp(kernelTree, kversion, kname, krev, compInfos)
}

func setupModsFromComp(kernelTree, kversion, kname string, krev snap.Revision, compInfos []*snap.ComponentSideInfo) error {
	// This folder needs to exist always to allow for directory swapping
	// in the future, even if right now we don't have components.
	compsRoot := filepath.Join(kernelTree, "lib", "modules", kversion, "updates")
	if err := os.MkdirAll(compsRoot, 0755); err != nil {
		return err
	}

	if len(compInfos) == 0 {
		return nil
	}

	// Symbolic links to components
	for _, ci := range compInfos {
		compPI := snap.MinimalComponentContainerPlaceInfo(ci.Component.ComponentName,
			ci.Revision, kname)
		lname := filepath.Join(compsRoot, ci.Component.ComponentName)
		to := filepath.Join(compPI.MountDir(), "modules", kversion)
		if err := osSymlink(to, lname); err != nil {
			return err
		}
	}

	// Run depmod
	stdout, stderr, err := osutil.RunSplitOutput("depmod", "-b", kernelTree, kversion)
	if err != nil {
		return osutil.OutputErrCombine(stdout, stderr, err)
	}
	logger.Noticef("depmod output:\n%s\n", string(osutil.CombineStdOutErr(stdout, stderr)))

	return nil
}

func driversTreeDir(kernelSubdir string, rev snap.Revision) string {
	return filepath.Join(dirs.SnapKernelDriversTreesDirUnder(dirs.GlobalRootDir),
		kernelSubdir, rev.String())
}

// RemoveKernelDriversTree cleans-up the writable kernel tree in snapd data
// folder, under kernelSubdir/<rev> (kernelSubdir is usually the snap name).
func RemoveKernelDriversTree(kernelSubdir string, rev snap.Revision) (err error) {
	treeRoot := driversTreeDir(kernelSubdir, rev)
	return os.RemoveAll(treeRoot)
}

type KernelDriversTreeOptions struct {
	// Set if we are building the tree for a kernel we are installing right now
	KernelInstall bool
}

// EnsureKernelDriversTree creates a drivers tree that can include modules/fw
// from kernel-modules components. opts.KernelInstall tells the function if
// this is a kernel install (which might be installing components at the same
// time) or an only components install.
//
// For kernel installs, this function creates a tree in
// /var/lib/snapd/kernel/<ksnapName>/<rev>, which is bind-mounted after a
// reboot to /usr/lib/{modules,firmware} (the currently active kernel is using
// a different path as it has a different revision). This tree contains files
// from the kernel snap mounted on kernelMount, as well as symlinks to it.
//
// For components-only install, we want the components to be available without
// rebooting. For this, we work on a temporary tree, and after finishing it we
// swap atomically the affected modules/firmware folders with those of the
// currently active kernel drivers tree.
func EnsureKernelDriversTree(ksnapName string, rev snap.Revision, kernelMount string, kmodsInfos []*snap.ComponentSideInfo, opts *KernelDriversTreeOptions) (err error) {
	// The temporal dir when installing only components can be fixed as a
	// task installing/updating a kernel-modules component must conflict
	// with changes containing this same task. This helps with clean-ups if
	// something goes wrong. Note that this folder needs to be in the same
	// filesystem as the final one so we can atomically switch the folders.
	ksnapDir := ksnapName + "_tmp"
	if opts.KernelInstall {
		ksnapDir = ksnapName
		treeDir := driversTreeDir(ksnapDir, rev)
		exists, isDir, _ := osutil.DirExists(treeDir)
		if exists && isDir {
			logger.Debugf("device tree %q already created on installation, not re-creating",
				treeDir)
			return nil
		}
	}
	// Initial clean-up to make the function idempotent
	if rmErr := RemoveKernelDriversTree(ksnapDir, rev); rmErr != nil &&
		!errors.Is(err, fs.ErrNotExist) {
		logger.Noticef("while removing old kernel tree: %v", rmErr)
	}

	defer func() {
		// Remove on return if error or if temporary tree
		if err == nil && opts.KernelInstall {
			return
		}
		if rmErr := RemoveKernelDriversTree(ksnapDir, rev); rmErr != nil &&
			!errors.Is(err, fs.ErrNotExist) {
			logger.Noticef("while cleaning up kernel tree: %v", rmErr)
		}
	}()

	treeRoot := driversTreeDir(ksnapDir, rev)

	// Create drivers tree
	kversion, err := KernelVersionFromModulesDir(kernelMount)
	if err == nil {
		if err := createModulesSubtree(kernelMount, treeRoot,
			kversion, ksnapName, rev, kmodsInfos); err != nil {
			return err
		}
	} else {
		// Bit of a corner case, but maybe possible. Log anyway.
		// TODO detect this issue in snap pack, should be enforced
		// if the snap declares kernel-modules components.
		logger.Noticef("no modules found in %q", kernelMount)
	}

	fwDir := filepath.Join(treeRoot, "lib", "firmware")
	if opts.KernelInstall {
		// symlinks in /lib/firmware are not affected by components
		if err := createFirmwareSymlinks(kernelMount, fwDir); err != nil {
			return err
		}
	}
	updateFwDir := filepath.Join(fwDir, "updates")
	// This folder needs to exist always to allow for directory swapping
	// in the future, even if right now we don't have components.
	if err := os.MkdirAll(updateFwDir, 0755); err != nil {
		return err
	}
	for _, kmi := range kmodsInfos {
		compPI := snap.MinimalComponentContainerPlaceInfo(kmi.Component.ComponentName,
			kmi.Revision, ksnapName)
		if err := createFirmwareSymlinks(compPI.MountDir(), updateFwDir); err != nil {
			return err
		}
	}

	// Sync before returning successfully (install kernel case) and also
	// for swapping case so we have consistent content before swapping
	// folder.
	syscall.Sync()

	if !opts.KernelInstall {
		// There is a (very small) chance of a poweroff/reboot while
		// having swapped only one of these two folders. If that
		// happens, snapd will re-run the task on the next boot, but
		// with mismatching modules/fw for the installed components. As
		// modules shipped by components should not be that critical,
		// in principle the system should recover.

		// Swap modules directories
		oldRoot := driversTreeDir(ksnapName, rev)

		// Swap updates directory inside firmware dir
		oldFwUpdates := filepath.Join(oldRoot, "lib", "firmware", "updates")
		if err := osutil.SwapDirs(oldFwUpdates, updateFwDir); err != nil {
			return fmt.Errorf("while swapping %q <-> %q: %w", oldFwUpdates, updateFwDir, err)
		}

		newMods := filepath.Join(treeRoot, "lib", "modules", kversion)
		oldMods := filepath.Join(oldRoot, "lib", "modules", kversion)
		if err := osutil.SwapDirs(oldMods, newMods); err != nil {
			// Undo firmware swap
			if err := osutil.SwapDirs(oldFwUpdates, updateFwDir); err != nil {
				logger.Noticef("while reverting modules swap: %v", err)
			}
			return fmt.Errorf("while swapping %q <-> %q: %w", newMods, oldMods, err)
		}

		// Make sure that changes are written
		syscall.Sync()
	}

	return nil
}
