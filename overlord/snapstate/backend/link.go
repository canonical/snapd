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
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
	"github.com/snapcore/snapd/wrappers"
)

func updateCurrentSymlinks(info *snap.Info) (e error) {
	mountDir := info.MountDir()

	currentActiveSymlink := filepath.Join(mountDir, "..", "current")
	if err := os.Remove(currentActiveSymlink); err != nil && !os.IsNotExist(err) {
		logger.Noticef("Cannot remove %q: %v", currentActiveSymlink, err)
	}

	dataDir := info.DataDir()
	currentDataSymlink := filepath.Join(dataDir, "..", "current")
	if err := os.Remove(currentDataSymlink); err != nil && !os.IsNotExist(err) {
		logger.Noticef("Cannot remove %q: %v", currentDataSymlink, err)
	}

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	cleanup := []string{dataDir, ""}[:1] // cleanup has cap(2)
	defer func() {
		if e == nil {
			return
		}
		for _, d := range cleanup {
			if err := os.Remove(d); err != nil {
				logger.Noticef("Cannot clean up %q: %v", d, err)
			}
		}
	}()

	if err := os.Symlink(filepath.Base(dataDir), currentDataSymlink); err != nil {
		return err
	}
	cleanup = append(cleanup, currentDataSymlink)

	return os.Symlink(filepath.Base(mountDir), currentActiveSymlink)
}

func hasFontConfigCache(info *snap.Info) bool {
	if info.GetType() == snap.TypeOS || info.GetType() == snap.TypeSnapd {
		return true
	}
	return false
}

// LinkSnap makes the snap available by generating wrappers and setting the current symlinks.
func (b Backend) LinkSnap(info *snap.Info, model *asserts.Model, tm timings.Measurer) (e error) {
	if info.Revision.Unset() {
		return fmt.Errorf("cannot link snap %q with unset revision", info.InstanceName())
	}

	var err error
	timings.Run(tm, "generate-wrappers", fmt.Sprintf("generate wrappers for snap %s", info.InstanceName()), func(timings.Measurer) {
		err = generateWrappers(info)
	})
	if err != nil {
		return err
	}
	defer func() {
		if e == nil {
			return
		}
		timings.Run(tm, "remove-wrappers", fmt.Sprintf("remove wrappers of snap %s", info.InstanceName()), func(timings.Measurer) {
			removeGeneratedWrappers(info, progress.Null)
		})
	}()

	// fontconfig is only relevant on classic and is carried by 'core' or
	// 'snapd' snaps
	// for non-core snaps, fontconfig cache needs to be updated before the
	// snap applications are runnable
	if release.OnClassic && !hasFontConfigCache(info) {
		timings.Run(tm, "update-fc-cache", "update font config caches", func(timings.Measurer) {
			// XXX: does this need cleaning up? (afaict no)
			if err := updateFontconfigCaches(); err != nil {
				logger.Noticef("cannot update fontconfig cache: %v", err)
			}
		})
	}

	if bp, applicable := boot.Lookup(info, info.GetType(), model, release.OnClassic); applicable {
		if err := bp.SetNextBoot(); err != nil {
			return err
		}
	}

	if err := updateCurrentSymlinks(info); err != nil {
		return err
	}
	// if anything below here could return error, you need to
	// somehow clean up whatever updateCurrentSymlinks did

	// for core snap, fontconfig cache can be updated after the snap has
	// been made available
	if release.OnClassic && hasFontConfigCache(info) {
		timings.Run(tm, "update-fc-cache", "update font config caches", func(timings.Measurer) {
			if err := updateFontconfigCaches(); err != nil {
				logger.Noticef("cannot update fontconfig cache: %v", err)
			}
		})
	}
	return nil
}

func (b Backend) StartServices(apps []*snap.AppInfo, meter progress.Meter, tm timings.Measurer) error {
	return wrappers.StartServices(apps, meter, tm)
}

func (b Backend) StopServices(apps []*snap.AppInfo, reason snap.ServiceStopReason, meter progress.Meter, tm timings.Measurer) error {
	return wrappers.StopServices(apps, reason, meter, tm)
}

func generateWrappers(s *snap.Info) error {
	// add the CLI apps from the snap.yaml
	if err := wrappers.AddSnapBinaries(s); err != nil {
		return err
	}
	// add the daemons from the snap.yaml
	if err := wrappers.AddSnapServices(s, progress.Null); err != nil {
		wrappers.RemoveSnapBinaries(s)
		return err
	}
	// add the desktop files
	if err := wrappers.AddSnapDesktopFiles(s); err != nil {
		wrappers.RemoveSnapServices(s, progress.Null)
		wrappers.RemoveSnapBinaries(s)
		return err
	}

	return nil
}

func removeGeneratedWrappers(s *snap.Info, meter progress.Meter) error {
	err1 := wrappers.RemoveSnapBinaries(s)
	if err1 != nil {
		logger.Noticef("Cannot remove binaries for %q: %v", s.InstanceName(), err1)
	}

	err2 := wrappers.RemoveSnapServices(s, meter)
	if err2 != nil {
		logger.Noticef("Cannot remove services for %q: %v", s.InstanceName(), err2)
	}

	err3 := wrappers.RemoveSnapDesktopFiles(s)
	if err3 != nil {
		logger.Noticef("Cannot remove desktop files for %q: %v", s.InstanceName(), err3)
	}

	return firstErr(err1, err2, err3)
}

// UnlinkSnap makes the snap unavailable to the system removing wrappers and symlinks.
func (b Backend) UnlinkSnap(info *snap.Info, meter progress.Meter) error {
	// remove generated services, binaries etc
	err1 := removeGeneratedWrappers(info, meter)

	// and finally remove current symlinks
	err2 := removeCurrentSymlinks(info)

	// FIXME: aggregate errors instead
	return firstErr(err1, err2)
}

func removeCurrentSymlinks(info snap.PlaceInfo) error {
	var err1, err2 error

	// the snap "current" symlink
	currentActiveSymlink := filepath.Join(info.MountDir(), "..", "current")
	err1 = os.Remove(currentActiveSymlink)
	if err1 != nil && !os.IsNotExist(err1) {
		logger.Noticef("Cannot remove %q: %v", currentActiveSymlink, err1)
	} else {
		err1 = nil
	}

	// the data "current" symlink
	currentDataSymlink := filepath.Join(info.DataDir(), "..", "current")
	err2 = os.Remove(currentDataSymlink)
	if err2 != nil && !os.IsNotExist(err2) {
		logger.Noticef("Cannot remove %q: %v", currentDataSymlink, err2)
	} else {
		err2 = nil
	}

	if err1 != nil && err2 != nil {
		return fmt.Errorf("cannot remove snap current symlink: %v and %v", err1, err2)
	} else if err1 != nil {
		return fmt.Errorf("cannot remove snap current symlink: %v", err1)
	} else if err2 != nil {
		return fmt.Errorf("cannot remove snap current symlink: %v", err2)
	}

	return nil
}
