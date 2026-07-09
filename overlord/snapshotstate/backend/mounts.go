// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/systemd"
)

// listMountsAtOrUnder returns active mount points at or under baseDir,
// divided into snapctl and non-snapctl mount groups.
func listMountsAtOrUnder(snapName, baseDir string) (snapctlMPs, nonSnapctlMPs []string, err error) {
	sysd := systemd.New(systemd.SystemMode, nil)
	// mounts created using snapctl have the "mount-control" origin
	// only loaded and active units are needed
	mcMountPoints, err := sysd.ListMountUnits(snapName, "mount-control", systemd.LoadedMountUnits)
	if err != nil {
		return nil, nil, err
	}
	mcMounts := make(map[string]bool, len(mcMountPoints))
	for _, mp := range mcMountPoints {
		mcMounts[mp] = true
	}

	mountInfo, err := osutil.LoadMountInfo()
	if err != nil {
		return nil, nil, err
	}

	for _, entry := range mountInfo {
		mp := entry.MountDir
		if !isPathAtOrUnderDir(mp, baseDir) {
			continue
		}
		if mcMounts[mp] {
			snapctlMPs = append(snapctlMPs, mp)
		} else {
			nonSnapctlMPs = append(nonSnapctlMPs, mp)
		}
	}
	return snapctlMPs, nonSnapctlMPs, nil
}

// stopMountUnits stops the mount units corresponding to the given mount points,
// one by one. It returns an error if any unit could not be stopped or if any
// mount point did not have a corresponding unit file.
// It returns all successfully stopped units (even on partial failure).
func stopMountUnits(mountPoints []string) (stoppedUnits []string, err error) {
	if len(mountPoints) == 0 {
		return nil, nil
	}
	sysd := systemd.New(systemd.SystemMode, nil)
	for _, mp := range mountPoints {
		unitPath := systemd.ExistingMountUnitPath(dirs.StripRootDir(mp))
		if unitPath == "" {
			return stoppedUnits, fmt.Errorf("cannot find mount unit file for mount point %q", mp)
		}

		unit := filepath.Base(unitPath)
		if stopErr := sysd.Stop([]string{unit}); stopErr != nil {
			return stoppedUnits, stopErr
		}
		stoppedUnits = append(stoppedUnits, unit)
	}
	return stoppedUnits, nil
}

func startMountUnits(units []string) error {
	if len(units) == 0 {
		return nil
	}
	sysd := systemd.New(systemd.SystemMode, nil)
	return sysd.Start(units)
}

func isPathAtOrUnderDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	// rel starts with ".." when path is outside dir
	return !strings.HasPrefix(rel, "..")
}
