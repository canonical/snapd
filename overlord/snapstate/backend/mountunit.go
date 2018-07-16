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
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

func addMountUnit(s *snap.Info, meter progress.Meter) error {
	squashfsPath := dirs.StripRootDir(s.MountFile())
	whereDir := dirs.StripRootDir(s.MountDir())

	sysd := systemd.New(dirs.GlobalRootDir, meter)
	mountUnitName, err := sysd.WriteMountUnitFile(s.InstanceName(), s.Revision.String(), squashfsPath, whereDir, "squashfs")
	if err != nil {
		return err
	}

	// occasionally we need to do a daemon-reload here to ensure that systemd really
	// knows about this new mount unit file
	if err := sysd.DaemonReloadIfNeeded(true, mountUnitName); err != nil {
		return err
	}

	if err := sysd.Enable(mountUnitName); err != nil {
		return err
	}

	return sysd.Start(mountUnitName)
}

func removeMountUnit(baseDir string, meter progress.Meter) error {
	sysd := systemd.New(dirs.GlobalRootDir, meter)
	unit := systemd.MountUnitPath(dirs.StripRootDir(baseDir))
	if osutil.FileExists(unit) {
		// use umount -d (cleanup loopback devices) -l (lazy) to ensure that even busy mount points
		// can be unmounted.
		// note that the long option --lazy is not supported on trusty.
		// the explicit -d is only needed on trusty.
		isMounted, err := osutil.IsMounted(baseDir)
		if err != nil {
			return err
		}
		mountUnitName := filepath.Base(unit)
		if isMounted {
			if output, err := exec.Command("umount", "-d", "-l", baseDir).CombinedOutput(); err != nil {
				return osutil.OutputErr(output, err)
			}

			if err := sysd.Stop(mountUnitName, time.Duration(1*time.Second)); err != nil {
				return err
			}
		}
		if err := sysd.Disable(mountUnitName); err != nil {
			return err
		}

		if err := sysd.ResetFailedIfNeeded(mountUnitName); err != nil {
			return err
		}

		if err := os.Remove(unit); err != nil {
			return err
		}

		if err := sysd.DaemonReloadIfNeeded(false, mountUnitName); err != nil {
			return err
		}
	}

	return nil
}
