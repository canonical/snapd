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
	"strings"
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

func createFirmwareSymlinks(fwMount MountPoints, fwDest string) error {
	fwOrig := fwMount.UnderCurrentPath("firmware")
	if err := os.MkdirAll(fwDest, 0755); err != nil {
		return err
	}

	// Symbolic links inside firmware folder - it cannot be directly a
	// symlink to "firmware" as we will use firmware/updates/ subfolder for
	// components.
	entries, err := os.ReadDir(fwOrig)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Debugf("no firmware found in %q", fwOrig)
			return nil
		}
		return err
	}

	fwTarget := fwMount.UnderTargetPath("firmware")
	for _, node := range entries {
		switch node.Type() {
		case 0, fs.ModeDir:
			// "updates" is included in (latest) kernel snaps but
			// is empty, and we use if for firmware shipped in
			// components, so we ignore it.
			if node.Name() == "updates" {
				continue
			}
			// Create link for regular files or directories
			lpath := filepath.Join(fwDest, node.Name())
			if err := os.Symlink(filepath.Join(fwTarget, node.Name()), lpath); err != nil {
				return err
			}
		case fs.ModeSymlink:
			// Replicate link (it should be relative)
			// TODO check this in snap pack
			lpath := filepath.Join(fwDest, node.Name())
			dest, err := os.Readlink(filepath.Join(fwOrig, node.Name()))
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

func createModulesSubtree(kMntPts MountPoints, kernelTree, kversion string, compsMntPts []ModulesCompMountPoints) error {
	// Although empty we need "lib" because "depmod" always appends
	// "/lib/modules/<kernel_version>" to the directory passed with option
	// "-b".
	modsRoot := filepath.Join(kernelTree, "lib", "modules", kversion)
	if err := os.MkdirAll(modsRoot, 0755); err != nil {
		return err
	}

	// Copy modinfo files from the snap (these might be overwritten if
	// kernel-modules components are installed).
	modsGlob := kMntPts.UnderCurrentPath("modules", kversion, "modules.*")
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

	// Symbolic links to current mount of the kernel snap
	currentMntDir := kMntPts.UnderCurrentPath("modules", kversion)
	if err := createKernelModulesSymlinks(modsRoot, currentMntDir); err != nil {
		return err
	}

	// If necessary, add modules from components and run depmod
	if err := setupModsFromComp(kernelTree, kversion, compsMntPts); err != nil {
		return err
	}

	// Change symlinks to target ones when needed
	if !kMntPts.CurrentEqualsTarget() {
		targetMntDir := kMntPts.UnderTargetPath("modules", kversion)
		if err := createKernelModulesSymlinks(modsRoot, targetMntDir); err != nil {
			return err
		}
	}

	return nil
}

func createKernelModulesSymlinks(modsRoot, kMntPt string) error {
	for _, d := range []string{"kernel", "vdso"} {
		lname := filepath.Join(modsRoot, d)
		to := filepath.Join(kMntPt, d)
		// We might be re-creating, first remove
		os.Remove(lname)
		if err := osSymlink(to, lname); err != nil {
			return err
		}
	}

	return nil
}

func setupModsFromComp(kernelTree, kversion string, compsMntPts []ModulesCompMountPoints) error {
	// This folder needs to exist always to allow for directory swapping
	// in the future, even if right now we don't have components.
	compsRoot := filepath.Join(kernelTree, "lib", "modules", kversion, "updates")
	if err := os.MkdirAll(compsRoot, 0755); err != nil {
		return err
	}

	if len(compsMntPts) == 0 {
		return nil
	}

	// Symbolic links to components
	for _, cmp := range compsMntPts {
		lname := filepath.Join(compsRoot, cmp.LinkName)
		to := cmp.UnderCurrentPath("modules", kversion)
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

	// Change symlinks to target ones when needed
	for _, cmp := range compsMntPts {
		if cmp.CurrentEqualsTarget() {
			continue
		}
		lname := filepath.Join(compsRoot, cmp.LinkName)
		to := cmp.UnderTargetPath("modules", kversion)
		// remove old link
		os.Remove(lname)
		if err := osSymlink(to, lname); err != nil {
			return err
		}
	}

	return nil
}

// DriversTreeDir returns the directory for a given kernel and revision under
// rootdir.
func DriversTreeDir(rootdir, kernelName string, rev snap.Revision) string {
	return filepath.Join(dirs.SnapKernelDriversTreesDirUnder(rootdir),
		kernelName, rev.String())
}

// RemoveKernelDriversTree cleans-up the writable kernel tree in snapd data
// folder, under kernelSubdir/<rev> (kernelSubdir is usually the snap name).
// When called from the kernel package <rev> might be <rev>_tmp.
func RemoveKernelDriversTree(treeRoot string) (err error) {
	return os.RemoveAll(treeRoot)
}

type KernelDriversTreeOptions struct {
	// Set if we are building the tree for a kernel we are installing right now
	KernelInstall bool
}

// MountPoints describes mount points for a snap or a component.
type MountPoints struct {
	// Current is where the container to be installed is currently
	// available
	Current string
	// Target is where the container will be found in a running system
	Target string
}

func (mp *MountPoints) UnderCurrentPath(dirs ...string) string {
	return filepath.Join(append([]string{mp.Current}, dirs...)...)
}

func (mp *MountPoints) UnderTargetPath(dirs ...string) string {
	return filepath.Join(append([]string{mp.Target}, dirs...)...)
}

func (mp *MountPoints) CurrentEqualsTarget() bool {
	return mp.Current == mp.Target
}

// ModulesCompMountPoints contains mount points for a component plus its name.
type ModulesCompMountPoints struct {
	// LinkName is the name of the symlink in the drivers tree that will
	// point to the component modules.
	LinkName string
	MountPoints
}

// EnsureKernelDriversTree creates a drivers tree that can include modules/fw
// from kernel-modules components. opts.KernelInstall tells the function if
// this is a kernel install (which might be installing components at the same
// time) or an only components install.
//
// For kernel installs, this function creates a tree in destDir (should be of
// the form <somedir>/var/lib/snapd/kernel/<ksnapName>/<rev>), which is
// bind-mounted after a reboot to /usr/lib/{modules,firmware} (the currently
// active kernel is using a different path as it has a different revision).
// This tree contains files from the kernel snap content in kSnapRoot, as well
// as symlinks to it. Information from modules is found by looking at
// comps slice.
//
// For components-only install, we want the components to be available without
// rebooting. For this, we work on a temporary tree, and after finishing it we
// swap atomically the affected modules/firmware folders with those of the
// currently active kernel drivers tree.
//
// To make this work in all cases we need to know the current mounts of the
// kernel snap / components to be installed and the final mounts when the
// system is run after installation (as the installing system might be classic
// while the installed system could be hybrid or UC, or we could be installing
// from the initramfs). To consider all cases, we need to run depmod with links
// to the currently available content, and then replace those links with the
// expected mounts in the running system.
func EnsureKernelDriversTree(kMntPts MountPoints, compsMntPts []ModulesCompMountPoints, destDir string, opts *KernelDriversTreeOptions) (err error) {
	// The temporal dir when installing only components can be fixed as a
	// task installing/updating a kernel-modules component must conflict
	// with changes containing this same task. This helps with clean-ups if
	// something goes wrong. Note that this folder needs to be in the same
	// filesystem as the final one so we can atomically switch the folders.
	destDir = strings.TrimSuffix(destDir, "/")
	targetDir := destDir + "_tmp"
	if opts.KernelInstall {
		targetDir = destDir
		exists, isDir, _ := osutil.DirExists(targetDir)
		if exists && isDir {
			logger.Debugf("device tree %q already created on installation, not re-creating",
				targetDir)
			return nil
		}
	}
	// Initial clean-up to make the function idempotent
	if rmErr := RemoveKernelDriversTree(targetDir); rmErr != nil &&
		!errors.Is(err, fs.ErrNotExist) {
		logger.Noticef("while removing old kernel tree: %v", rmErr)
	}

	defer func() {
		// Remove on return if error or if temporary tree
		if err == nil && opts.KernelInstall {
			return
		}
		if rmErr := RemoveKernelDriversTree(targetDir); rmErr != nil &&
			!errors.Is(err, fs.ErrNotExist) {
			logger.Noticef("while cleaning up kernel tree: %v", rmErr)
		}
	}()

	// Create drivers tree
	kversion, err := KernelVersionFromModulesDir(kMntPts.Current)
	if err == nil {
		if err := createModulesSubtree(kMntPts, targetDir,
			kversion, compsMntPts); err != nil {
			return err
		}
	} else {
		logger.Debugf("no modules found in %q", kMntPts.Current)
	}

	fwDir := filepath.Join(targetDir, "lib", "firmware")
	if opts.KernelInstall {
		// symlinks in /lib/firmware are not affected by components
		if err := createFirmwareSymlinks(kMntPts, fwDir); err != nil {
			return err
		}
	}
	updateFwDir := filepath.Join(fwDir, "updates")
	// This folder needs to exist always to allow for directory swapping
	// in the future, even if right now we don't have components.
	if err := os.MkdirAll(updateFwDir, 0755); err != nil {
		return err
	}
	for _, cmp := range compsMntPts {
		if err := createFirmwareSymlinks(cmp.MountPoints, updateFwDir); err != nil {
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
		oldRoot := destDir

		// Swap updates directory inside firmware dir
		oldFwUpdates := filepath.Join(oldRoot, "lib", "firmware", "updates")
		if err := osutil.SwapDirs(oldFwUpdates, updateFwDir); err != nil {
			return fmt.Errorf("while swapping %q <-> %q: %w", oldFwUpdates, updateFwDir, err)
		}

		newMods := filepath.Join(targetDir, "lib", "modules", kversion)
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
