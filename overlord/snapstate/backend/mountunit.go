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
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

func stripGlobalRootDir(dir string) string {
	if dirs.GlobalRootDir == "/" {
		return dir
	}

	return dir[len(dirs.GlobalRootDir):]
}

func addMountUnit(s *snap.Info, meter progress.Meter) error {
	squashfsPath := stripGlobalRootDir(s.MountFile())
	whereDir := stripGlobalRootDir(s.MountDir())

	sysd := systemd.New(dirs.GlobalRootDir, meter)
	mountUnitName, err := sysd.WriteMountUnitFile(s.Name(), squashfsPath, whereDir, "squashfs")
	if err != nil {
		return err
	}

	// systemd needs a little nudge
	if err := sysd.DaemonReload(); err != nil {
		return err
	}

	// we always enable the mount unit even in inhibit hooks
	if err := sysd.Enable(mountUnitName); err != nil {
		return err
	}

	return sysd.Start(mountUnitName)
}

func removeMountUnit(baseDir string, meter progress.Meter) error {
	sysd := systemd.New(dirs.GlobalRootDir, meter)
	unit := systemd.MountUnitPath(stripGlobalRootDir(baseDir), "mount")
	if osutil.FileExists(unit) {
		if err := sysd.Stop(filepath.Base(unit), time.Duration(1*time.Second)); err != nil {
			return err
		}
		if err := sysd.Disable(filepath.Base(unit)); err != nil {
			return err
		}
		if err := os.Remove(unit); err != nil {
			return err
		}
		// systemd needs a little nudge
		if err := sysd.DaemonReload(); err != nil {
			return err
		}
	}

	return nil
}
