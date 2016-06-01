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

package snappy

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/wrappers"
)

// Overlord is responsible for the overall system state.
type Overlord struct {
}

// featureSet contains the flag values that can be listed in assumes entries
// that this ubuntu-core actually provides.
var featureSet = map[string]bool{
	// Support for common data directory across revisions of a snap.
	"common-data-dir": true,
}

func checkAssumes(s *snap.Info) error {
	missing := ([]string)(nil)
	for _, flag := range s.Assumes {
		if !featureSet[flag] {
			missing = append(missing, flag)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("snap %q assumes unsupported features: %s (try new ubuntu-core)", s.Name(), strings.Join(missing, ", "))
	}
	return nil
}

// CheckSnap ensures that the snap can be installed
func CheckSnap(snapFilePath string, curInfo *snap.Info, flags InstallFlags, meter progress.Meter) error {
	allowGadget := (flags & AllowGadget) != 0
	allowUnauth := (flags & AllowUnauthenticated) != 0

	// we do not Verify() the package here. This is done earlier in
	// openSnapFile() to ensure that we do not mount/inspect
	// potentially dangerous snaps

	s, snapf, err := openSnapFile(snapFilePath, allowUnauth, nil)
	if err != nil {
		return err
	}

	err = checkAssumes(s)
	if err != nil {
		return err
	}

	// we do not security Verify() (check hashes) the package here.
	// This is done earlier in
	// openSnapFile() to ensure that we do not mount/inspect
	// potentially dangerous snaps
	return canInstall(s, snapf, curInfo, allowGadget, meter)
}

// SetupSnap does prepare and mount the snap for further processing
// It returns the installed path and an error
func SetupSnap(snapFilePath string, sideInfo *snap.SideInfo, flags InstallFlags, meter progress.Meter) (snap.PlaceInfo, error) {
	inhibitHooks := (flags & InhibitHooks) != 0
	allowUnauth := (flags & AllowUnauthenticated) != 0

	s, snapf, err := openSnapFile(snapFilePath, allowUnauth, sideInfo)
	if err != nil {
		return nil, err
	}
	instdir := s.MountDir()

	if err := os.MkdirAll(instdir, 0755); err != nil {
		logger.Noticef("Can not create %q: %v", instdir, err)
		return s, err
	}

	if err := snapf.Install(s.MountFile(), instdir); err != nil {
		return s, err
	}

	// generate the mount unit for the squashfs
	if err := addMountUnit(s, inhibitHooks, meter); err != nil {
		return s, err
	}

	// FIXME: special handling is bad 'mkay
	if s.Type == snap.TypeKernel {
		if err := extractKernelAssets(s, flags, meter); err != nil {
			return s, fmt.Errorf("cannot install kernel: %s", err)
		}
	}

	return s, err
}

type interacter interface {
	Notify(status string)
}

func addMountUnit(s *snap.Info, noMount bool, inter interacter) error {
	squashfsPath := stripGlobalRootDir(s.MountFile())
	whereDir := stripGlobalRootDir(s.MountDir())

	sysd := systemd.New(dirs.GlobalRootDir, inter)
	mountUnitName, err := sysd.WriteMountUnitFile(s.Name(), squashfsPath, whereDir)
	if err != nil {
		return err
	}

	// we always enable the mount unit even in inhibit hooks
	if err := sysd.Enable(mountUnitName); err != nil {
		return err
	}

	if !noMount {
		return sysd.Start(mountUnitName)
	}

	return nil
}

func removeMountUnit(baseDir string, inter interacter) error {
	sysd := systemd.New(dirs.GlobalRootDir, inter)
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
	}

	return nil
}

func UndoSetupSnap(s snap.PlaceInfo, meter progress.Meter) {
	// SetupSnap did it not made far enough
	if !osutil.FileExists(s.MountDir()) {
		return
	}

	if err := RemoveSnapFiles(s, meter); err != nil {
		logger.Noticef("cannot remove snap files: %s", err)
	}

	mountDir := s.MountDir()
	snapPath := s.MountFile()

	// remove install dir and the snap blob itself
	for _, path := range []string{
		mountDir,
		snapPath,
	} {
		if err := os.RemoveAll(path); err != nil {
			logger.Noticef("cannot remove snap package at %v: %s", mountDir, err)
		}
	}

	// FIXME: do we need to undo installGadgetHardwareUdevRules via
	//        cleanupGadgetHardwareUdevRules ? it will go away
	//        and can only be used during install right now
}

func CopyData(newSnap, oldSnap *snap.Info, flags InstallFlags, meter progress.Meter) error {
	// deal with the old data or
	// otherwise just create a empty data dir

	// Make sure the common data directory exists, even if this isn't a new
	// install.
	if err := os.MkdirAll(newSnap.CommonDataDir(), 0755); err != nil {
		return err
	}

	if oldSnap == nil {
		return os.MkdirAll(newSnap.DataDir(), 0755)
	}

	return copySnapData(oldSnap, newSnap)
}

func UndoCopyData(newInfo *snap.Info, flags InstallFlags, meter progress.Meter) {
	// XXX we were copying data, assume InhibitHooks was false

	if err := RemoveSnapData(newInfo); err != nil {
		logger.Noticef("When cleaning up data for %s %s: %v", newInfo.Name(), newInfo.Version, err)
	}

}

func generateWrappers(s *snap.Info, inter interacter) error {
	// add the CLI apps from the snap.yaml
	if err := wrappers.AddSnapBinaries(s); err != nil {
		return err
	}
	// add the daemons from the snap.yaml
	if err := wrappers.AddSnapServices(s, inter); err != nil {
		return err
	}
	// add the desktop files
	if err := wrappers.AddSnapDesktopFiles(s); err != nil {
		return err
	}

	return nil
}

// removeGeneratedWrappers removes the generated services, binaries, desktop
// wrappers
func removeGeneratedWrappers(s *snap.Info, inter interacter) error {
	err1 := wrappers.RemoveSnapBinaries(s)
	if err1 != nil {
		logger.Noticef("Cannot remove binaries for %q: %v", s.Name(), err1)
	}

	err2 := wrappers.RemoveSnapServices(s, inter)
	if err2 != nil {
		logger.Noticef("Cannot remove services for %q: %v", s.Name(), err2)
	}

	err3 := wrappers.RemoveSnapDesktopFiles(s)
	if err3 != nil {
		logger.Noticef("Cannot remove desktop files for %q: %v", s.Name(), err3)
	}

	return firstErr(err1, err2, err3)
}

func updateCurrentSymlink(info *snap.Info, inter interacter) error {
	mountDir := info.MountDir()

	currentActiveSymlink := filepath.Join(mountDir, "..", "current")
	if err := os.Remove(currentActiveSymlink); err != nil && !os.IsNotExist(err) {
		logger.Noticef("Cannot remove %q: %v", currentActiveSymlink, err)
		return err
	}

	dataDir := info.DataDir()
	dbase := filepath.Dir(dataDir)
	currentDataSymlink := filepath.Join(dbase, "current")
	if err := os.Remove(currentDataSymlink); err != nil && !os.IsNotExist(err) {
		logger.Noticef("Cannot remove %q: %v", currentDataSymlink, err)
		return err
	}

	// symlink is relative to parent dir
	if err := os.Symlink(filepath.Base(mountDir), currentActiveSymlink); err != nil {
		return err
	}

	if err := os.MkdirAll(info.DataDir(), 0755); err != nil {
		return err
	}

	// FIXME: create {Os,Kernel}Snap type instead of adding special
	//        cases here
	if err := SetNextBoot(info); err != nil {
		return err
	}

	return os.Symlink(filepath.Base(dataDir), currentDataSymlink)
}

func removeCurrentSymlink(info snap.PlaceInfo, inter interacter) error {
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
	currentDataSymlink := filepath.Join(filepath.Dir(info.DataDir()), "current")
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

// ActivateSnap is a wrapper around
// (generate-security-profile, generate-wrappers, update-current-symlink)
//
// Note that the snap must not be activated when this is called.
func ActivateSnap(s *Snap, inter interacter) error {
	info := s.Info()
	mountDir := info.MountDir()
	currentActiveSymlink := filepath.Join(mountDir, "..", "current")
	currentActiveDir, err := filepath.EvalSymlinks(currentActiveSymlink)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// already active, nothing to do
	if mountDir == currentActiveDir {
		return nil
	}

	// there is already an active snap
	if currentActiveDir != "" {
		return fmt.Errorf("cannot activate snap while another one is active: %v", currentActiveDir)
	}

	// security setup was done here!

	return linkSnap(s.Info(), inter)
}

func linkSnap(s *snap.Info, inter interacter) error {
	if err := generateWrappers(s, inter); err != nil {
		return err
	}

	return updateCurrentSymlink(s, inter)
}

// unlinkSnap deactivates the given active snap.
func unlinkSnap(info *snap.Info, inter interacter) error {
	mountDir := info.MountDir()

	currentSymlink := filepath.Join(mountDir, "..", "current")
	currentActiveDir, err := filepath.EvalSymlinks(currentSymlink)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrSnapNotActive
		}
		return err
	}
	if mountDir != currentActiveDir {
		return ErrSnapNotActive
	}

	// remove generated services, binaries, security policy
	err1 := removeGeneratedWrappers(info, inter)

	// removing security setup move here!

	// and finally remove current symlink
	err2 := removeCurrentSymlink(info, inter)

	// FIXME: aggregate errors instead
	return firstErr(err1, err2)
}

// Install installs the given snap file to the system.
//
// It returns the local snap file or an error
func (o *Overlord) Install(snapFilePath string, flags InstallFlags, meter progress.Meter) (sp *snap.Info, err error) {
	return o.InstallWithSideInfo(snapFilePath, nil, flags, meter)
}

// InstallWithSideInfo installs the given snap file to the system
// considering the provided side info.
//
// It returns the local snap file or an error
func (o *Overlord) InstallWithSideInfo(snapFilePath string, sideInfo *snap.SideInfo, flags InstallFlags, meter progress.Meter) (sp *snap.Info, err error) {
	var oldInfo *snap.Info

	if sideInfo != nil {
		oldSnap := ActiveSnapByName(sideInfo.OfficialName)
		if oldSnap != nil {
			oldInfo = oldSnap.Info()
		}
	}

	if err := CheckSnap(snapFilePath, oldInfo, flags, meter); err != nil {
		return nil, err
	}

	minInfo, err := SetupSnap(snapFilePath, sideInfo, flags, meter)
	defer func() {
		if err != nil {
			if minInfo != nil {
				UndoSetupSnap(minInfo, meter)
			}
		}
	}()
	if err != nil {
		return nil, err
	}

	allowUnauth := (flags & AllowUnauthenticated) != 0
	newInfo, _, err := openSnapFile(snapFilePath, allowUnauth, sideInfo)
	if err != nil {
		return nil, err
	}

	// XXX: this is still done for now for this legacy Install to
	// keep unit tests as they are working and as strawman
	// behavior for current u-d-f
	if newInfo.Revision.Store() {
		if err := SaveManifest(newInfo); err != nil {
			return nil, err
		}
	}

	if oldInfo != nil {
		// we need to stop any services and make the commands unavailable
		// so that copying data and later activating the new revision
		// can work
		err = unlinkSnap(oldInfo, meter)
		defer func() {
			if err != nil {
				if err := linkSnap(oldInfo, meter); err != nil {
					logger.Noticef("When linking old revision: %v", newInfo.Name(), err)
				}
			}
		}()
		if err != nil {
			return nil, err
		}
	}

	// deal with the data
	err = CopyData(newInfo, oldInfo, flags, meter)
	defer func() {
		if err != nil {
			UndoCopyData(newInfo, flags, meter)
		}
	}()
	if err != nil {
		return nil, err
	}

	// and finally make active

	if (flags & InhibitHooks) != 0 {
		// XXX: kill InhibitHooks flag but used by u-d-f atm
		return newInfo, nil
	}

	err = linkSnap(newInfo, meter)
	defer func() {
		if err != nil {
			if err := unlinkSnap(newInfo, meter); err != nil {
				logger.Noticef("When unlinking failed new snap revision: %v", newInfo.Name(), err)
			}
		}
	}()
	if err != nil {
		return nil, err
	}

	return newInfo, nil
}

// CanInstall checks whether the Snap passes a series of tests required for installation
func canInstall(s *snap.Info, snapf snap.Container, curInfo *snap.Info, allowGadget bool, inter interacter) error {
	// verify we have a valid architecture
	if !arch.IsSupportedArchitecture(s.Architectures) {
		return &ErrArchitectureNotSupported{s.Architectures}
	}

	// assume allowGadget, this is only used by u-d-f now

	return nil
}

func CanRemove(s *snap.Info, active bool) bool {
	// Gadget snaps should not be removed as they are a key
	// building block for Gadgets. Prunning non active ones
	// is acceptible.
	if s.Type == snap.TypeGadget && active {
		return false
	}

	// You never want to remove an active kernel or OS
	if (s.Type == snap.TypeKernel || s.Type == snap.TypeOS) && active {
		return false
	}

	return true
}

// RemoveSnapFiles removes the snap files from the disk
func RemoveSnapFiles(s snap.PlaceInfo, meter progress.Meter) error {
	mountDir := s.MountDir()

	// XXX: nicer way to do this
	typ := snap.TypeApp
	content, err := ioutil.ReadFile(filepath.Join(mountDir, "meta", "/snap.yaml"))
	if err == nil {
		info, err := snap.InfoFromSnapYaml(content)
		if err == nil {
			typ = info.Type
		}
	}

	snapPath := s.MountFile()
	// this also ensures that the mount unit stops
	if err := removeMountUnit(mountDir, meter); err != nil {
		return err
	}

	if err := os.RemoveAll(mountDir); err != nil {
		return err
	}

	// best effort(?)
	os.Remove(filepath.Dir(mountDir))

	// remove the snap
	if err := os.RemoveAll(snapPath); err != nil {
		return err
	}

	// remove the kernel assets (if any)
	if typ == snap.TypeKernel {
		if err := removeKernelAssets(s, meter); err != nil {
			logger.Noticef("removing kernel assets failed with %s", err)
		}
	}

	return nil
}

// Uninstall removes the given local snap from the system.
//
// It returns an error on failure
func (o *Overlord) Uninstall(s *Snap, meter progress.Meter) error {
	if !CanRemove(s.Info(), s.IsActive()) {
		return ErrPackageNotRemovable
	}

	if err := unlinkSnap(s.Info(), meter); err != nil && err != ErrSnapNotActive {
		return err
	}

	if err := RemoveSnapFiles(s.Info(), meter); err != nil {
		return err
	}

	return RemoveSnapData(s.Info())
}

// SetActive sets the active state of the given snap
//
// It returns an error on failure
func (o *Overlord) SetActive(s *Snap, active bool, meter progress.Meter) error {
	if active {
		// deactivate current first
		if current := ActiveSnapByName(s.Name()); current != nil {
			if err := unlinkSnap(current.Info(), meter); err != nil {
				return err
			}
		}
		return ActivateSnap(s, meter)
	}

	return unlinkSnap(s.Info(), meter)
}

// Installed returns the installed snaps from this repository
func (o *Overlord) Installed() ([]*Snap, error) {
	globExpr := filepath.Join(dirs.SnapSnapsDir, "*", "*", "meta", "snap.yaml")
	snaps, err := o.snapsForGlobExpr(globExpr)
	if err != nil {
		return nil, fmt.Errorf("Can not get the installed snaps: %s", err)

	}

	return snaps, nil
}

func (o *Overlord) snapsForGlobExpr(globExpr string) (snaps []*Snap, err error) {
	matches, err := filepath.Glob(globExpr)
	if err != nil {
		return nil, err
	}

	for _, yamlfile := range matches {
		// skip "current" and similar symlinks
		realpath, err := filepath.EvalSymlinks(yamlfile)
		if err != nil {
			return nil, err
		}
		if realpath != yamlfile {
			continue
		}

		snap, err := NewInstalledSnap(realpath)
		if err != nil {
			return nil, err
		}
		snaps = append(snaps, snap)
	}

	return snaps, nil
}
