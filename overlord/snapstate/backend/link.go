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

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
	"github.com/snapcore/snapd/wrappers"
)

var wrappersAddSnapdSnapServices = wrappers.AddSnapdSnapServices

// LinkContext carries additional information about the current or the previous
// state of the snap
type LinkContext struct {
	// FirstInstall indicates whether this is the first time given snap is
	// installed
	FirstInstall bool

	// IsUndo is set when we are installing the previous snap while
	// performing a revert of the latest one that was installed
	IsUndo bool

	// ServiceOptions is used to configure services.
	ServiceOptions *wrappers.SnapServiceOptions

	// RunInhibitHint is used only in Unlink snap, and can be used to
	// establish run inhibition lock for refresh operations.
	RunInhibitHint runinhibit.Hint

	// RequireMountedSnapdSnap indicates that the apps and services
	// generated when linking need to use tooling from the snapd snap mount.
	RequireMountedSnapdSnap bool

	// SkipBinaries indicates that we should skip removing snap binaries,
	// icons and desktop files in UnlinkSnap
	SkipBinaries bool
}

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

// LinkSnap makes the snap available by generating wrappers and setting the current symlinks.
func (b Backend) LinkSnap(info *snap.Info, dev snap.Device, linkCtx LinkContext, tm timings.Measurer) (rebootRequired boot.RebootInfo, e error) {
	if info.Revision.Unset() {
		return boot.RebootInfo{}, fmt.Errorf("cannot link snap %q with unset revision", info.InstanceName())
	}

	osutil.MaybeInjectFault("link-snap")

	var err error
	timings.Run(tm, "generate-wrappers", fmt.Sprintf("generate wrappers for snap %s", info.InstanceName()), func(timings.Measurer) {
		err = b.generateWrappers(info, linkCtx)
	})
	if err != nil {
		return boot.RebootInfo{}, err
	}
	defer func() {
		if e == nil {
			return
		}
		timings.Run(tm, "remove-wrappers", fmt.Sprintf("remove wrappers of snap %s", info.InstanceName()), func(timings.Measurer) {
			removeGeneratedWrappers(info, linkCtx, progress.Null)
		})
	}()

	var rebootInfo boot.RebootInfo
	if !b.preseed {
		bootCtx := boot.NextBootContext{BootWithoutTry: linkCtx.IsUndo}
		rebootInfo, err = boot.Participant(
			info, info.Type(), dev).SetNextBoot(bootCtx)
		if err != nil {
			return boot.RebootInfo{}, err
		}
	}

	if err := updateCurrentSymlinks(info); err != nil {
		return boot.RebootInfo{}, err
	}
	// if anything below here could return error, you need to
	// somehow clean up whatever updateCurrentSymlinks did

	// Stop inhibiting application startup by removing the inhibitor file.
	if err := runinhibit.Unlock(info.InstanceName()); err != nil {
		return boot.RebootInfo{}, err
	}

	return rebootInfo, nil
}

func (b Backend) StartServices(apps []*snap.AppInfo, disabledSvcs []string, meter progress.Meter, tm timings.Measurer) error {
	flags := &wrappers.StartServicesFlags{Enable: true}
	return wrappers.StartServices(apps, disabledSvcs, flags, meter, tm)
}

func (b Backend) StopServices(apps []*snap.AppInfo, reason snap.ServiceStopReason, meter progress.Meter, tm timings.Measurer) error {
	return wrappers.StopServices(apps, nil, reason, meter, tm)
}

func (b Backend) generateWrappers(s *snap.Info, linkCtx LinkContext) error {
	var err error
	var cleanupFuncs []func(*snap.Info) error
	defer func() {
		if err != nil {
			for _, cleanup := range cleanupFuncs {
				cleanup(s)
			}
		}
	}()

	if s.Type() == snap.TypeSnapd {
		// snapd services are handled separately
		return GenerateSnapdWrappers(s, &GenerateSnapdWrappersOptions{b.preseed})
	}

	// add the CLI apps from the snap.yaml
	if err = wrappers.EnsureSnapBinaries(s); err != nil {
		return err
	}
	cleanupFuncs = append(cleanupFuncs, wrappers.RemoveSnapBinaries)

	// add the daemons from the snap.yaml
	ensureOpts := &wrappers.EnsureSnapServicesOptions{
		Preseeding:              b.preseed,
		RequireMountedSnapdSnap: linkCtx.RequireMountedSnapdSnap,
	}
	if err = wrappers.EnsureSnapServices(map[*snap.Info]*wrappers.SnapServiceOptions{
		s: linkCtx.ServiceOptions,
	}, ensureOpts, nil, progress.Null); err != nil {
		return err
	}
	cleanupFuncs = append(cleanupFuncs, func(s *snap.Info) error {
		return wrappers.RemoveSnapServices(s, progress.Null)
	})

	// add D-Bus service activation files
	if err = wrappers.AddSnapDBusActivationFiles(s); err != nil {
		return err
	}
	cleanupFuncs = append(cleanupFuncs, wrappers.RemoveSnapDBusActivationFiles)

	// add the sessions.
	if err = wrappers.EnsureSnapSessionFiles(s); err != nil {
		return err
	}
	cleanupFuncs = append(cleanupFuncs, wrappers.RemoveSnapSessionFiles)

	// add the desktop files
	if err = wrappers.EnsureSnapDesktopFiles([]*snap.Info{s}); err != nil {
		return err
	}
	cleanupFuncs = append(cleanupFuncs, wrappers.RemoveSnapDesktopFiles)

	// add the desktop icons
	if err = wrappers.EnsureSnapIcons(s); err != nil {
		return err
	}
	cleanupFuncs = append(cleanupFuncs, wrappers.RemoveSnapIcons)

	return nil
}

func removeGeneratedWrappers(s *snap.Info, linkCtx LinkContext, meter progress.Meter) error {
	if s.Type() == snap.TypeSnapd {
		return removeGeneratedSnapdWrappers(s, linkCtx.FirstInstall, progress.Null)
	}

	var err1, err2, err3 error
	if !linkCtx.SkipBinaries {
		err1 = wrappers.RemoveSnapBinaries(s)
		if err1 != nil {
			logger.Noticef("Cannot remove binaries for %q: %v", s.InstanceName(), err1)
		}

		err2 = wrappers.RemoveSnapDesktopFiles(s)
		if err2 != nil {
			logger.Noticef("Cannot remove desktop files for %q: %v", s.InstanceName(), err2)
		}

		err3 = wrappers.RemoveSnapIcons(s)
		if err3 != nil {
			logger.Noticef("Cannot remove desktop icons for %q: %v", s.InstanceName(), err3)
		}
	}

	err4 := wrappers.RemoveSnapDBusActivationFiles(s)
	if err4 != nil {
		logger.Noticef("Cannot remove D-Bus activation for %q: %v", s.InstanceName(), err4)
	}

	err5 := wrappers.RemoveSnapServices(s, meter)
	if err5 != nil {
		logger.Noticef("Cannot remove services for %q: %v", s.InstanceName(), err5)
	}

	return firstErr(err1, err2, err3, err4, err5)
}

// GenerateSnapdWrappersOptions carries options for GenerateSnapdWrappers.
type GenerateSnapdWrappersOptions struct {
	Preseeding bool
}

func GenerateSnapdWrappers(s *snap.Info, opts *GenerateSnapdWrappersOptions) error {
	wrappersOpts := &wrappers.AddSnapdSnapServicesOptions{}
	if opts != nil {
		wrappersOpts.Preseeding = opts.Preseeding
	}
	// snapd services are handled separately via an explicit helper
	return wrappersAddSnapdSnapServices(s, wrappersOpts, progress.Null)
}

func removeGeneratedSnapdWrappers(s *snap.Info, firstInstall bool, meter progress.Meter) error {
	if !firstInstall {
		// snapd service units are only removed during first
		// installation of the snapd snap, in other scenarios they are
		// overwritten
		return nil
	}
	return wrappers.RemoveSnapdSnapServicesOnCore(s, meter)
}

// UnlinkSnap makes the snap unavailable to the system removing wrappers and
// symlinks. The firstInstallUndo is true when undoing the first installation of
// the snap.
func (b Backend) UnlinkSnap(info *snap.Info, linkCtx LinkContext, meter progress.Meter) error {
	var err0 error
	if hint := linkCtx.RunInhibitHint; hint != runinhibit.HintNotInhibited {
		// inhibit startup of new programs
		inhibitInfo := runinhibit.InhibitInfo{Previous: info.SnapRevision()}
		err0 = runinhibit.LockWithHint(info.InstanceName(), hint, inhibitInfo)
	}

	// remove generated services, binaries etc
	err1 := removeGeneratedWrappers(info, linkCtx, meter)

	// and finally remove current symlinks
	err2 := removeCurrentSymlinks(info)

	// FIXME: aggregate errors instead
	return firstErr(err0, err1, err2)
}

func (b Backend) QueryDisabledServices(info *snap.Info, pb progress.Meter) ([]string, error) {
	return wrappers.QueryDisabledServices(info, pb)
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
