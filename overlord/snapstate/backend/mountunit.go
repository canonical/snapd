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
	mountPoints, err := sysd.ListMountUnits(s.ContainerName(), origin)
	if err != nil {
		return err
	}
	for _, mountPoint := range mountPoints {
		if len(baseDirs) > 0 && !isUnderAnyDir(mountPoint, baseDirs) {
			continue
		}
		if err := sysd.RemoveMountUnitFile(mountPoint); err != nil {
			return err
		}
	}
	return nil
}

// isUnderAnyDir reports whether path is a strict subdirectory of any of the
// provided candidate directories (the path itself being equal to a candidate
// does not count).
func isUnderAnyDir(path string, candidates []string) bool {
	for _, c := range candidates {
		rel, err := filepath.Rel(c, path)
		if err != nil {
			continue
		}
		// rel is "." when path == c
		// rel starts with ".." when path is outside c
		if rel != "." && !strings.HasPrefix(rel, "..") {
			return true
		}
	}
	return false
}
