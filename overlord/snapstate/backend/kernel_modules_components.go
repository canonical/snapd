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

package backend

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/systemd"
)

type NoKernelDriversError struct {
	cref          naming.ComponentRef
	kernelVersion string
}

func (e NoKernelDriversError) Error() string {
	return fmt.Sprintf("%s does not contain firmware or components for %s",
		e.cref, e.kernelVersion)
}

func cleanupMount(mountDir string, meter progress.Meter) error {
	mountDir = filepath.Join(dirs.GlobalRootDir, mountDir)
	// this also ensures that the mount unit stops
	if err := removeMountUnit(mountDir, meter); err != nil {
		return err
	}

	if err := os.RemoveAll(mountDir); err != nil {
		return err
	}

	return nil
}

type kernelModulesCleanupParts struct {
	compMountDir    string
	modulesMountDir string
	rerunDepmod     bool
}

func cleanupKernelModulesSetup(parts *kernelModulesCleanupParts, kernelVersion string, meter progress.Meter) error {
	if parts.modulesMountDir != "" {
		if err := cleanupMount(parts.modulesMountDir, meter); err != nil {
			return err
		}
		if parts.rerunDepmod {
			if err := runDepmod("/usr", kernelVersion); err != nil {
				return err
			}
		}
	}

	if parts.compMountDir != "" {
		if err := cleanupMount(parts.compMountDir, meter); err != nil {
			return err
		}
	}

	return nil
}

func checkKernelModulesCompContent(mountDir, kernelVersion string) (bool, bool) {
	hasModules := osutil.IsDirectory(filepath.Join(mountDir, "modules", kernelVersion))
	hasFirmware := osutil.IsDirectory(filepath.Join(mountDir, "firmware"))
	return hasModules, hasFirmware
}

func componentMountPoint(componentName, kernelVersion string) string {
	return filepath.Join("/run/mnt/kernel-modules/", kernelVersion, componentName)
}

func modulesMountPoint(componentName, kernelVersion string) string {
	return filepath.Join("/usr/lib/modules", kernelVersion, "updates", componentName)
}

// SetupKernelModulesComponent creates and starts mount units for
// kernel-modules components.
func (b Backend) SetupKernelModulesComponent(cpi snap.ContainerPlaceInfo, cref naming.ComponentRef, kernelVersion string, meter progress.Meter) (err error) {
	var sysd systemd.Systemd
	if b.preseed {
		sysd = systemd.NewEmulationMode(dirs.GlobalRootDir)
	} else {
		sysd = systemd.New(systemd.SystemMode, meter)
	}

	// Restore state if something goes wrong
	var cleanOnFailure kernelModulesCleanupParts
	defer func() {
		if err == nil {
			return
		}
		if err := cleanupKernelModulesSetup(&cleanOnFailure, kernelVersion, meter); err != nil {
			logger.Noticef("while cleaning up a failed kernel-modules set-up: %v", err)
		}
	}()

	// Check that the kernel-modules component really has
	// something we must mount on early boot.
	hasModules, hasFirmware := checkKernelModulesCompContent(cpi.MountDir(), kernelVersion)
	if !hasModules && !hasFirmware {
		return &NoKernelDriversError{cref: cref, kernelVersion: kernelVersion}
	}

	// Mount the component itself (we need it early, so the mount in /snap cannot
	// be used).
	componentMount := componentMountPoint(cref.ComponentName, kernelVersion)
	addMountUnitOptions := &systemd.MountUnitOptions{
		MountUnitType: systemd.BeforeDriversLoadMountUnit,
		Lifetime:      systemd.Persistent,
		Description:   fmt.Sprintf("Mount unit for kernel-modules component %s", cref),
		What:          cpi.MountFile(),
		Where:         componentMount,
		Fstype:        "squashfs",
		Options:       []string{"nodev,ro,x-gdu.hide,x-gvfs-hide"},
	}
	_, err = sysd.EnsureMountUnitFileWithOptions(addMountUnitOptions)
	if err != nil {
		return fmt.Errorf("cannot create mount in %q: %w", componentMount, err)
	}
	cleanOnFailure.compMountDir = componentMount

	if hasModules {
		// systemd automatically works out dependencies on the "what"
		// path too so this mount happens after the component one.
		modulesDir := modulesMountPoint(cref.ComponentName, kernelVersion)
		addMountUnitOptions = &systemd.MountUnitOptions{
			MountUnitType: systemd.BeforeDriversLoadMountUnit,
			Lifetime:      systemd.Persistent,
			Description:   fmt.Sprintf("Mount unit for modules from %s", cref.String()),
			What:          filepath.Join(componentMount, "modules", kernelVersion),
			Where:         modulesDir,
			Fstype:        "none",
			Options:       []string{"bind"},
		}
		_, err = sysd.EnsureMountUnitFileWithOptions(addMountUnitOptions)
		if err != nil {
			return fmt.Errorf("cannot create mount in %q: %w", modulesDir, err)
		}
		cleanOnFailure.modulesMountDir = modulesDir

		// Rebuild modinfo files
		if err := runDepmod("/usr", kernelVersion); err != nil {
			return err
		}
		cleanOnFailure.rerunDepmod = true
	}

	if hasFirmware {
		// TODO create recursively symlinks in
		// /usr/lib/firmware/updates while checking for conflicts with
		// existing files.
	}

	return nil
}

var runDepmod = runDepmodImpl

func runDepmodImpl(baseDir, kernelVersion string) error {
	logger.Debugf("running depmod on %q for kernel %s", baseDir, kernelVersion)
	stdout, stderr, err := osutil.RunSplitOutput("depmod", "-b", baseDir, kernelVersion)
	logger.Debugf("depmod stderr:\n%s\n\ndepmod stdout:\n%s",
		string(stderr), string(stdout))
	if err != nil {
		return osutil.OutputErrCombine(stdout, stderr, err)
	}
	return nil
}

// UndoSetupKernelModulesComponent undoes the work of SetupKernelModulesComponent
func (b Backend) UndoSetupKernelModulesComponent(cpi snap.ContainerPlaceInfo, cref naming.ComponentRef, kernelVersion string, meter progress.Meter) error {
	hasModules, hasFirmware := checkKernelModulesCompContent(cpi.MountDir(), kernelVersion)
	var partsToClean kernelModulesCleanupParts
	if hasFirmware {
		// TODO remove recursively symlinks in
		// /usr/lib/firmware/updates (set var in kernelModulesCleanupParts)
	}

	// Remove created mount units
	if hasModules {
		partsToClean.modulesMountDir =
			modulesMountPoint(cref.ComponentName, kernelVersion)
		partsToClean.rerunDepmod = true
	}

	if hasModules || hasFirmware {
		partsToClean.compMountDir =
			componentMountPoint(cref.ComponentName, kernelVersion)
	}

	return cleanupKernelModulesSetup(&partsToClean, kernelVersion, meter)
}
