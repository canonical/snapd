// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

// MountUnitFlags contains flags that modify behavior of addMountUnit
type MountUnitFlags struct {
	// PreventRestartIfModified is set if we do not want to restart the
	// mount unit if even though it was modified
	PreventRestartIfModified bool
	// StartBeforeDriversLoad is set if the unit is needed before
	// udevd starts to run rules
	StartBeforeDriversLoad bool
}

func addMountUnit(c snap.ContainerPlaceInfo, sysd systemd.Systemd, mountFlags MountUnitFlags) error {
	squashfsPath := dirs.StripRootDir(c.MountFile())
	whereDir := dirs.StripRootDir(c.MountDir())

	mountOptions := &systemd.MountUnitOptions{
		Lifetime:                 systemd.Persistent,
		Description:              c.MountDescription(),
		What:                     squashfsPath,
		Where:                    whereDir,
		PreventRestartIfModified: mountFlags.PreventRestartIfModified,
	}

	if err := sysd.ConfigureMountUnitOptions(mountOptions, "squashfs", mountFlags.StartBeforeDriversLoad); err != nil {
		return err
	}

	_, err := sysd.EnsureMountUnitFile(mountOptions)
	return err
}

func removeMountUnit(mountDir string, meter progress.Meter) error {
	sysd := systemd.New(systemd.SystemMode, meter)
	return sysd.RemoveMountUnitFile(mountDir)
}

// RemoveContainerMountUnits removes mount units for the given container. Only
// units whose origin label matches origin are considered (pass "" to match all
// origins). If baseDirs is non-empty, only units whose mount point lies strictly
// inside one of those directories are removed; units at or above a base
// directory are left untouched. Removal stops and returns an error immediately
// if any unit cannot be removed.
func (b Backend) RemoveContainerMountUnits(s snap.ContainerPlaceInfo, meter progress.Meter, origin string, baseDirs []string) error {
	sysd := systemd.New(systemd.SystemMode, meter)
	// Get installed mount units which includes the unloaded units.
	// Using systemd.InstalledMountUnits here ensures that we get the
	// mount units even if they are not currently loaded in systemd's
	// memory (e.g. if it was stopped and garbage-collected).
	mountPoints, err := sysd.ListMountUnits(s.ContainerName(), origin, systemd.InstalledMountUnits)
	if err != nil {
		return err
	}
	for _, mountPoint := range mountPoints {
		opts := isUnderAnyDirOptions{AllowExactMatch: false}
		if len(baseDirs) > 0 && !isUnderAnyDir(mountPoint, baseDirs, opts) {
			continue
		}
		if err := sysd.RemoveMountUnitFile(mountPoint); err != nil {
			return err
		}
	}
	return nil
}

// isUnderAnyDirOptions configures the behavior of isUnderAnyDir.
type isUnderAnyDirOptions struct {
	// AllowExactMatch controls whether path == dir counts as a match.
	AllowExactMatch bool
}

// isUnderAnyDir reports whether path is a subdirectory of any of the provided
// directories. If opts.AllowExactMatch is true, an exact match (path == dir)
// also counts.
func isUnderAnyDir(path string, dirs []string, opts isUnderAnyDirOptions) bool {
	for _, d := range dirs {
		rel, err := filepath.Rel(d, path)
		if err != nil {
			continue
		}
		// rel starts with ".." when path is outside d
		if strings.HasPrefix(rel, "..") {
			continue
		}
		// rel is "." when path == d
		if rel == "." && !opts.AllowExactMatch {
			continue
		}
		return true
	}
	return false
}

// ListNonSnapctlMountsInSnapRevDataDirs returns the active mount points
// that are at or under the snap's revision-specific data directories and are
// not created using snapctl.
func (b Backend) ListNonSnapctlMountsInSnapRevDataDirs(info *snap.Info, opts *dirs.SnapDirOptions) ([]string, error) {
	revDirs, err := snapDataDirs(info, opts)
	if err != nil {
		return nil, err
	}
	return listNonSnapctlMounts(info, revDirs)
}

// ListNonSnapctlMountsInSnapAllDataDirs returns the active mount points
// that are at or under any of the snap's base data directories and are not
// created using snapctl.
func (b Backend) ListNonSnapctlMountsInSnapAllDataDirs(info *snap.Info, opts *dirs.SnapDirOptions) ([]string, error) {
	baseDirs, err := snapBaseDataDirs(info.InstanceName(), opts)
	if err != nil {
		return nil, err
	}
	return listNonSnapctlMounts(info, baseDirs)
}

func listNonSnapctlMounts(info *snap.Info, baseDirs []string) ([]string, error) {
	sysd := systemd.New(systemd.SystemMode, nil)
	// Mounts created using snapctl have the "mount-control" origin.
	// Only active units are needed here but systemd.LoadedMountUnits
	// lists loaded units which may be active or inactive. This is still
	// fine because mcMountPoints is only used to filter the mountInfo
	// list which only contains active mounts.
	mcMountPoints, err := sysd.ListMountUnits(info.ContainerName(), "mount-control", systemd.LoadedMountUnits)
	if err != nil {
		return nil, err
	}
	mcMounts := make(map[string]bool, len(mcMountPoints))
	for _, mp := range mcMountPoints {
		mcMounts[mp] = true
	}

	mountInfo, err := osutil.LoadMountInfo()
	if err != nil {
		return nil, err
	}

	var nonmcMountPoints []string
	for _, entry := range mountInfo {
		mp := entry.MountDir
		// also include mounts over the base dirs themselves
		opts := isUnderAnyDirOptions{AllowExactMatch: true}
		if !isUnderAnyDir(mp, baseDirs, opts) {
			continue
		}
		if mcMounts[mp] {
			continue
		}
		nonmcMountPoints = append(nonmcMountPoints, mp)
	}
	return nonmcMountPoints, nil
}
