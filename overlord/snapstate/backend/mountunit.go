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
	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

func addMountUnit(c snap.ContainerPlaceInfo, mountFlags systemd.EnsureMountUnitFlags, sysd systemd.Systemd) error {
	squashfsPath := dirs.StripRootDir(c.MountFile())
	whereDir := dirs.StripRootDir(c.MountDir())

	_ := mylog.Check2(sysd.EnsureMountUnitFile(c.MountDescription(), squashfsPath, whereDir, "squashfs",
		mountFlags))
	return err
}

func removeMountUnit(mountDir string, meter progress.Meter) error {
	sysd := systemd.New(systemd.SystemMode, meter)
	return sysd.RemoveMountUnitFile(mountDir)
}

func (b Backend) RemoveContainerMountUnits(s snap.ContainerPlaceInfo, meter progress.Meter) error {
	sysd := systemd.New(systemd.SystemMode, meter)
	originFilter := ""
	mountPoints := mylog.Check2(sysd.ListMountUnits(s.ContainerName(), originFilter))

	for _, mountPoint := range mountPoints {
		mylog.Check(sysd.RemoveMountUnitFile(mountPoint))
	}
	return nil
}
