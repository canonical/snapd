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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
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

func updateCurrentSymlinks(info *snap.Info) (revert func(), e error) {
	mountDir := info.MountDir()
	dataDir := info.DataDir()

	var previousActiveSymlinkTarget string
	var previousDataSymlinkTarget string
	currentActiveSymlink := filepath.Join(filepath.Dir(mountDir), "current")
	currentDataSymlink := filepath.Join(filepath.Dir(dataDir), "current")
	revertFunc := func() {
		if previousActiveSymlinkTarget != "" {
			mylog.Check(osutil.AtomicSymlink(previousActiveSymlinkTarget, currentActiveSymlink))
		}
		if previousDataSymlinkTarget != "" {
			mylog.Check(osutil.AtomicSymlink(previousDataSymlinkTarget, currentDataSymlink))
		}
	}
	defer func() {
		if e != nil {
			revertFunc()
		}
	}()

	if info.Type() == snap.TypeSnapd {

		previousActiveSymlinkTarget = mylog.Check2(os.Readlink(currentActiveSymlink))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			logger.Noticef("Cannot read %q: %v", currentActiveSymlink, err)
		}
		previousDataSymlinkTarget = mylog.Check2(os.Readlink(currentDataSymlink))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			logger.Noticef("Cannot read %q: %v", currentDataSymlink, err)
		}
	}
	mylog.Check(os.MkdirAll(dataDir, 0755))

	defer func() {
		if e != nil {
			mylog.Check(os.Remove(dataDir))
		}
	}()
	mylog.Check(osutil.AtomicSymlink(filepath.Base(dataDir), currentDataSymlink))
	mylog.Check(osutil.AtomicSymlink(filepath.Base(mountDir), currentActiveSymlink))

	return revertFunc, nil
}

// LinkSnap makes the snap available by generating wrappers and setting the current symlinks.
func (b Backend) LinkSnap(info *snap.Info, dev snap.Device, linkCtx LinkContext, tm timings.Measurer) (rebootRequired boot.RebootInfo, e error) {
	if info.Revision.Unset() {
		return boot.RebootInfo{}, fmt.Errorf("cannot link snap %q with unset revision", info.InstanceName())
	}

	osutil.MaybeInjectFault("link-snap")

	var restart wrappers.SnapdRestart
	timings.Run(tm, "generate-wrappers", fmt.Sprintf("generate wrappers for snap %s", info.InstanceName()), func(timings.Measurer) {
		restart = mylog.Check2(b.generateWrappers(info, linkCtx))
	})

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
		rebootInfo = mylog.Check2(boot.Participant(
			info, info.Type(), dev).SetNextBoot(bootCtx))

	}

	revertSymlinks := mylog.Check2(updateCurrentSymlinks(info))

	// if anything below here could return error, you need to
	// somehow clean up whatever updateCurrentSymlinks did

	if restart != nil {
		mylog.Check(restart.Restart())
	}
	mylog.Check(

		// Stop inhibiting application startup by removing the inhibitor file.
		runinhibit.Unlock(info.InstanceName()))

	return rebootInfo, nil
}

func componentLinkPath(cpi snap.ContainerPlaceInfo, snapRev snap.Revision) string {
	instanceName, compName, _ := strings.Cut(cpi.ContainerName(), "+")
	compBase := snap.ComponentsBaseDir(instanceName)
	return filepath.Join(compBase, snapRev.String(), compName)
}

func (b Backend) LinkComponent(cpi snap.ContainerPlaceInfo, snapRev snap.Revision) error {
	mountDir := cpi.MountDir()
	linkPath := componentLinkPath(cpi, snapRev)

	// Create components directory
	compsDir := filepath.Dir(linkPath)
	mylog.Check(os.MkdirAll(compsDir, 0755))

	// Work out relative path to go from the dir where the symlink lives to
	// the mount dir
	linkTarget := mylog.Check2(filepath.Rel(compsDir, mountDir))

	return osutil.AtomicSymlink(linkTarget, linkPath)
}

func (b Backend) StartServices(apps []*snap.AppInfo, disabledSvcs *wrappers.DisabledServices, meter progress.Meter, tm timings.Measurer) error {
	flags := &wrappers.StartServicesFlags{Enable: true}
	return wrappers.StartServices(apps, disabledSvcs, flags, meter, tm)
}

func (b Backend) StopServices(apps []*snap.AppInfo, reason snap.ServiceStopReason, meter progress.Meter, tm timings.Measurer) error {
	return wrappers.StopServices(apps, nil, reason, meter, tm)
}

func (b Backend) generateWrappers(s *snap.Info, linkCtx LinkContext) (wrappers.SnapdRestart, error) {
	var cleanupFuncs []func(*snap.Info) error
	defer func() {
	}()

	if s.Type() == snap.TypeSnapd {
		// snapd services are handled separately
		return GenerateSnapdWrappers(s, &GenerateSnapdWrappersOptions{b.preseed})
	}
	mylog.Check(

		// add the CLI apps from the snap.yaml
		wrappers.EnsureSnapBinaries(s))

	cleanupFuncs = append(cleanupFuncs, wrappers.RemoveSnapBinaries)

	// add the daemons from the snap.yaml
	ensureOpts := &wrappers.EnsureSnapServicesOptions{
		Preseeding:              b.preseed,
		RequireMountedSnapdSnap: linkCtx.RequireMountedSnapdSnap,
	}
	mylog.Check(wrappers.EnsureSnapServices(map[*snap.Info]*wrappers.SnapServiceOptions{
		s: linkCtx.ServiceOptions,
	}, ensureOpts, nil, progress.Null))

	cleanupFuncs = append(cleanupFuncs, func(s *snap.Info) error {
		return wrappers.RemoveSnapServices(s, progress.Null)
	})
	mylog.Check(

		// add D-Bus service activation files
		wrappers.AddSnapDBusActivationFiles(s))

	cleanupFuncs = append(cleanupFuncs, wrappers.RemoveSnapDBusActivationFiles)
	mylog.Check(

		// add the desktop files
		wrappers.EnsureSnapDesktopFiles([]*snap.Info{s}))

	cleanupFuncs = append(cleanupFuncs, wrappers.RemoveSnapDesktopFiles)
	mylog.Check(

		// add the desktop icons
		wrappers.EnsureSnapIcons(s))

	cleanupFuncs = append(cleanupFuncs, wrappers.RemoveSnapIcons)

	return nil, nil
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

func GenerateSnapdWrappers(s *snap.Info, opts *GenerateSnapdWrappersOptions) (wrappers.SnapdRestart, error) {
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

func (b Backend) QueryDisabledServices(info *snap.Info, pb progress.Meter) (*wrappers.DisabledServices, error) {
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

func (b Backend) UnlinkComponent(cpi snap.ContainerPlaceInfo, snapRev snap.Revision) error {
	linkPath := componentLinkPath(cpi, snapRev)
	mylog.Check(os.Remove(linkPath))

	// Try also to remove the <snap_rev>/ subdirectory, as this might be
	// the only installed component. But simply ignore if not empty.
	os.Remove(filepath.Dir(linkPath))

	return nil
}
