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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/wrappers"
)

func addMountUnit(s *snap.Info, preseed bool, meter progress.Meter) error {
	squashfsPath := dirs.StripRootDir(s.MountFile())
	whereDir := dirs.StripRootDir(s.MountDir())

	var sysd systemd.Systemd
	if preseed {
		sysd = systemd.NewEmulationMode(dirs.GlobalRootDir)
	} else {
		sysd = systemd.New(systemd.SystemMode, meter)
	}
	_, err := sysd.AddMountUnitFile(s.InstanceName(), s.Revision.String(), squashfsPath, whereDir, "squashfs")
	return err
}

func removeMountUnit(mountDir string, meter progress.Meter) error {
	sysd := systemd.New(systemd.SystemMode, meter)
	return sysd.RemoveMountUnitFile(mountDir)
}

func (b Backend) RemoveSnapMountUnits(s snap.PlaceInfo, meter progress.Meter) error {
	if err := wrappers.RemoveMountUnitFiles(s, "", meter); err != nil {
		logger.Noticef("Cannot remove mount units for %q: %v", s.InstanceName(), err)
	}
	return nil
}
