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
	"sync"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

// addMountUnit adds a new mount unit for the given snap "s". It requires
// a lock that ensures there is only a single concurrent operation that
// manipulates mount units (see https://github.com/systemd/systemd/issues/10872)
func addMountUnit(s *snap.Info, meter progress.Meter, lock *sync.Mutex) error {
	lock.Lock()
	defer lock.Unlock()

	squashfsPath := dirs.StripRootDir(s.MountFile())
	whereDir := dirs.StripRootDir(s.MountDir())

	sysd := systemd.New(dirs.GlobalRootDir, meter)
	mountUnitName, err := sysd.WriteMountUnitFile(s.InstanceName(), s.Revision.String(), squashfsPath, whereDir, "squashfs")
	if err != nil {
		return err
	}

	// we need to do a daemon-reload here to ensure that systemd really
	// knows about this new mount unit file
	if err := sysd.DaemonReload(); err != nil {
		return err
	}

	if err := sysd.Enable(mountUnitName); err != nil {
		return err
	}

	return sysd.Start(mountUnitName)
}

// removeMountUnit removes the mount unit for the given baseDir. It requires
// a lock that ensures there is only a single concurrent operation that
// manipulates mount units (see https://github.com/systemd/systemd/issues/10872)
func removeMountUnit(baseDir string, meter progress.Meter, lock *sync.Mutex) error {
	lock.Lock()
	defer lock.Unlock()

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
		if isMounted {
			if output, err := exec.Command("umount", "-d", "-l", baseDir).CombinedOutput(); err != nil {
				return osutil.OutputErr(output, err)
			}

			if err := sysd.Stop(filepath.Base(unit), time.Duration(1*time.Second)); err != nil {
				return err
			}
		}
		if err := sysd.Disable(filepath.Base(unit)); err != nil {
			return err
		}
		if err := os.Remove(unit); err != nil {
			return err
		}
		// daemon-reload to ensure that systemd actually really
		// forgets about this mount unit
		if err := sysd.DaemonReload(); err != nil {
			return err
		}
	}

	return nil
}
