// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

func validateAssetsContent(kernelRoot string, info *Info) error {
	// bare structure content is checked to exist during layout
	// make sure that filesystem content source paths exist as well
	for name, as := range info.Assets {
		for _, assetContent := range as.Content {
			c := assetContent
			// a single trailing / is allowed and indicates a directory
			isDir := strings.HasSuffix(c, "/")
			if isDir {
				c = strings.TrimSuffix(c, "/")
			}
			if filepath.Clean(c) != c || strings.Contains(c, "..") || c == "/" {
				return fmt.Errorf("asset %q: invalid content %q", name, assetContent)
			}
			realSource := filepath.Join(kernelRoot, c)
			if !osutil.FileExists(realSource) {
				return fmt.Errorf("asset %q: content %q source path does not exist", name, assetContent)
			}
			if isDir {
				// expecting a directory
				if !osutil.IsDirectory(realSource + "/") {
					return fmt.Errorf("asset %q: content %q is not a directory", name, assetContent)
				}
			}
		}
	}
	return nil
}

// validateModulesTree checks the kernel snap modules tree for entries that
// would conflict with snapd's runtime handling of the drivers tree.
func validateModulesTree(kernelRoot string) error {
	// The modules directory is optional; if there is none, there is nothing
	// to validate here.
	if _, err := os.Stat(filepath.Join(kernelRoot, "modules")); errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	// This does more checks, e.g. more than one kernel version
	kversion, err := KernelVersionFromModulesDir(kernelRoot)
	if err != nil {
		return err
	}

	modsDir := filepath.Join(kernelRoot, "modules", kversion)
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		// "updates" is reserved for modules coming from kernel-modules
		// components and must not be shipped by the kernel snap itself.
		if e.Name() == "updates" {
			return fmt.Errorf("modules directory %q must not contain an entry named %q, reserved for kernel-modules components", modsDir, e.Name())
		}
	}

	// TODO: validate that "kernel" and "vdso" are present?

	return nil
}

// validateFirmwareTree checks the kernel snap firmware tree for entries
// that would conflict with snapd's runtime handling of the drivers tree.
func validateFirmwareTree(kernelRoot string) error {
	fwDir := filepath.Join(kernelRoot, "firmware")
	// The firmware directory is optional; if there is none, there is
	// nothing to validate here.
	if _, err := os.Stat(fwDir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	// "updates" is reserved for firmware coming from
	// kernel-modules components and must not be shipped by the
	// kernel snap itself.
	updatesDir := filepath.Join(fwDir, "updates")
	e, err := os.Lstat(updatesDir)
	if errors.Is(err, fs.ErrNotExist) {
		// It's fine if the directory does not exist, it would be created at
		// the staging location as needed.
		return nil
	}
	if err != nil {
		return err
	}
	if !e.IsDir() {
		return fmt.Errorf("firmware directory %q must not contain an entry named %q that is not an empty directory", fwDir, e.Name())
	}
	// Some kernel build tooling ships an empty "updates" directory as a
	// harmless placeholder; only a non-empty one actually conflicts with
	// kernel-modules-component firmware at runtime.
	d, err := os.Open(updatesDir)
	if err != nil {
		return err
	}
	defer d.Close()
	if updatesEntries, err := d.Readdir(1); err != nil && !errors.Is(err, io.EOF) {
		return err
	} else if len(updatesEntries) > 0 {
		return fmt.Errorf("firmware directory %q must not contain a non-empty entry named %q, reserved for kernel-modules components", fwDir, e.Name())
	}
	return nil
}

// Validate checks whether the given directory contains valid kernel snap
// metadata and a matching content.
func Validate(kernelRoot string) error {
	info, err := ReadInfo(kernelRoot)
	if err != nil {
		return fmt.Errorf("invalid kernel metadata: %v", err)
	}

	if err := validateAssetsContent(kernelRoot, info); err != nil {
		return err
	}

	if err := validateModulesTree(kernelRoot); err != nil {
		return err
	}

	if err := validateFirmwareTree(kernelRoot); err != nil {
		return err
	}

	return nil
}
