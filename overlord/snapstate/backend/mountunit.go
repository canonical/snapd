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

	_, err := sysd.EnsureMountUnitFileWithOptions(mountOptions)
	return err
}

func removeMountUnit(mountDir string, meter progress.Meter) error {
	sysd := systemd.New(systemd.SystemMode, meter)
	return sysd.RemoveMountUnitFile(mountDir)
}

func (b Backend) RemoveContainerMountUnits(s snap.ContainerPlaceInfo, meter progress.Meter) error {
	sysd := systemd.New(systemd.SystemMode, meter)
	originFilter := ""
	mountPoints, err := sysd.ListMountUnits(s.ContainerName(), originFilter)
	if err != nil {
		return err
	}
	for _, mountPoint := range mountPoints {
		if err := sysd.RemoveMountUnitFile(mountPoint); err != nil {
			return err
		}
	}
	return nil
}
