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
	"fmt"

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

	fsType, options, mountUnitType := sysd.ConfigureMountUnitOptions(squashfsPath, "squashfs", mountFlags.StartBeforeDriversLoad)

	rootHash, err := c.DmVerityDigest()
	// TODO: in the future, if verity data is not found in the info, the error should be
	// raised when verity data are required by the system's policy.
	if err == nil {
		hashDevicePath, err := c.DmVerityFile()
		if err != nil {
			return err
		}
		hashDevicePath = dirs.StripRootDir(hashDevicePath)

		// TODO: systemd-mount currently supports only roothash and hashdevice as verity
		// options. The rest of the parameters needed for the dm-verity mount are retrieved
		// from the on-disk superblock. An improvement in the future would be to stop
		// using the parameters from the superblock and directly pass the parameters as
		// they were parsed from the store or the assertion. That would require support
		// in libmount which currently doesn't exist.
		options = append(options, fmt.Sprintf("verity.roothash=%s", rootHash))
		options = append(options, fmt.Sprintf("verity.hashdevice=%s", hashDevicePath))
	}

	mountOptions.Fstype = fsType
	mountOptions.Options = options
	mountOptions.MountUnitType = mountUnitType

	_, err = sysd.EnsureMountUnitFileWithOptions(mountOptions)
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
