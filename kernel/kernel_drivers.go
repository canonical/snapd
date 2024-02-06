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

func createFirmwareSymlinks(orig, dest string) error {
	fwOrig := filepath.Join(orig, "firmware")
	fwDest := filepath.Join(dest, "lib", "firmware")
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

func createModulesSubtree(origin, dest string) error {
	kversion, err := KernelVersionFromModulesDir(origin)
	if err != nil {
		// Bit of a corner case, but maybe possible. Log anyway.
		// TODO detect this issue in snap pack, should be enforced
		// if the snap declares kernel-modules components.
		logger.Noticef("no modules found in %q", origin)
		return nil
	}

	// Although empty we need "lib" because "depmod" always appends
	// "/lib/modules/<kernel_version>" to the directory passed with option
	// "-b".
	modsRoot := filepath.Join(dest, "lib", "modules", kversion)
	if err := os.MkdirAll(modsRoot, 0755); err != nil {
		return err
	}

	// Copy modinfo files from the snap (these might be overwritten if
	// kernel-modules components are installed).
	modsGlob := filepath.Join(origin, "modules", kversion, "modules.*")
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
	earlyMntDir := filepath.Join(origin, "modules", kversion)
	for _, d := range []string{"kernel", "vdso"} {
		lname := filepath.Join(modsRoot, d)
		to := filepath.Join(earlyMntDir, d)
		if err := osSymlink(to, lname); err != nil {
			return err
		}
	}

	return nil
}

func driversTreeDir(ksnapName string, rev snap.Revision) string {
	return filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "kernel", ksnapName, rev.String())
}

// RemoveKernelDriversTree cleans-up the writable kernel tree for a given snap
// and revision.
func RemoveKernelDriversTree(ksnapName string, rev snap.Revision) (err error) {
	treeRoot := driversTreeDir(ksnapName, rev)
	return os.RemoveAll(treeRoot)
}

// EnsureKernelDriversTree creates a tree in
// /var/lib/snapd/kernel/<ksnapName>/<rev> with subfolders can be bind-mounted
// on boot to /usr/lib/{modules,firmware}. This tree contains files from the
// kernel snap mounted on kernelMount, as well as symlinks to it.
// TODO this will be extended to consider kernel-modules components.
func EnsureKernelDriversTree(ksnapName string, rev snap.Revision, kernelMount string) (err error) {
	// Initial clean-up to make the function idempotent
	if rmErr := RemoveKernelDriversTree(ksnapName, rev); rmErr != nil &&
		!errors.Is(err, fs.ErrNotExist) {
		logger.Noticef("while removing old kernel tree: %v", rmErr)
	}

	// Remove tree in case something goes wrong
	defer func() {
		if err != nil {
			if rmErr := RemoveKernelDriversTree(ksnapName, rev); rmErr != nil &&
				!errors.Is(err, fs.ErrNotExist) {
				logger.Noticef("while cleaning up kernel tree: %v", rmErr)
			}
		}
	}()

	// Root of our kernel tree
	treeRoot := driversTreeDir(ksnapName, rev)

	if err := createModulesSubtree(kernelMount, treeRoot); err != nil {
		return err
	}

	return createFirmwareSymlinks(kernelMount, treeRoot)
}
