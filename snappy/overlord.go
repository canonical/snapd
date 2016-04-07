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
	"os"
	"path/filepath"
	"time"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/systemd"
)

// Overlord is responsible for the overall system state.
type Overlord struct {
}

// CheckSnap ensures that the snap can be installed
func CheckSnap(snapFilePath string, flags InstallFlags, meter progress.Meter) error {
	allowGadget := (flags & AllowGadget) != 0
	allowUnauth := (flags & AllowUnauthenticated) != 0

	// we do not Verify() the package here. This is done earlier in
	// NewSnapFile() to ensure that we do not mount/inspect
	// potentially dangerous snaps

	s, blobf, err := openSnapBlob(snapFilePath, allowUnauth, nil)
	if err != nil {
		return err
	}

	// we do not security Verify() (check hashes) the package here.
	// This is done earlier in
	// NewSnapFile() to ensure that we do not mount/inspect
	// potentially dangerous snaps
	return canInstall(s, blobf, allowGadget, meter)
}

// SetupSnap does prepare and mount the snap for further processing
// It returns the installed path and an error
func SetupSnap(snapFilePath string, flags InstallFlags, meter progress.Meter) (string, error) {
	inhibitHooks := (flags & InhibitHooks) != 0
	allowUnauth := (flags & AllowUnauthenticated) != 0

	// XXX: soon need to fill or get a sideinfo with at least revision
	s, blobf, err := openSnapBlob(snapFilePath, allowUnauth, nil)
	if err != nil {
		return "", err
	}
	instdir := s.BaseDir()

	// the "gadget" snaps are special
	if s.Type == snap.TypeGadget {
		if err := installGadgetHardwareUdevRules(s); err != nil {
			return "", err
		}
	}

	if err := os.MkdirAll(instdir, 0755); err != nil {
		logger.Noticef("Can not create %q: %v", instdir, err)
		return instdir, err
	}

	if err := blobf.Install(instdir); err != nil {
		return instdir, err
	}

	// generate the mount unit for the squashfs
	if err := addSquashfsMount(s, inhibitHooks, meter); err != nil {
		return instdir, err
	}

	// FIXME: special handling is bad 'mkay
	if s.Type == snap.TypeKernel {
		if err := extractKernelAssets(s, blobf, flags, meter); err != nil {
			return instdir, fmt.Errorf("failed to install kernel %s", err)
		}
	}

	return instdir, err
}

func addSquashfsMount(s *snap.Info, inhibitHooks bool, inter interacter) error {
	squashfsPath := stripGlobalRootDir(s.MountFile())
	whereDir := stripGlobalRootDir(s.BaseDir())

	sysd := systemd.New(dirs.GlobalRootDir, inter)
	mountUnitName, err := sysd.WriteMountUnitFile(s.Name(), squashfsPath, whereDir)
	if err != nil {
		return err
	}

	// we always enable the mount unit even in inhibit hooks
	if err := sysd.Enable(mountUnitName); err != nil {
		return err
	}

	if !inhibitHooks {
		return sysd.Start(mountUnitName)
	}

	return nil
}

func removeSquashfsMount(baseDir string, inter interacter) error {
	sysd := systemd.New(dirs.GlobalRootDir, inter)
	unit := systemd.MountUnitPath(stripGlobalRootDir(baseDir), "mount")
	if osutil.FileExists(unit) {
		// we ignore errors, nothing should stop removals
		if err := sysd.Disable(filepath.Base(unit)); err != nil {
			logger.Noticef("Failed to disable %q: %s, but continuing anyway.", unit, err)
		}
		if err := sysd.Stop(filepath.Base(unit), time.Duration(1*time.Second)); err != nil {
			logger.Noticef("Failed to stop %q: %s, but continuing anyway.", unit, err)
		}
		if err := os.Remove(unit); err != nil {
			return err
		}
	}

	return nil
}

func UndoSetupSnap(installDir string, meter progress.Meter) {
	// SetupSnap did it not made far enough
	if installDir == "" {
		return
	}

	// SetupSnap made it far enough to mount the snap, easy
	s, err := NewInstalledSnap(filepath.Join(installDir, "meta", "snap.yaml"))
	if err == nil {
		if err := RemoveSnapFiles(s, meter); err != nil {
			logger.Noticef("cannot remove snap files: %s", err)
		}
	}

	snapFile := s.Info().MountFile()

	// remove install dir and the snap blob itself
	for _, path := range []string{
		installDir,
		snapFile,
	} {
		if err := os.RemoveAll(path); err != nil {
			logger.Noticef("cannot remove snap package at %v: %s", installDir, err)
		}
	}

	// FIXME: do we need to undo installGadgetHardwareUdevRules via
	//        cleanupGadgetHardwareUdevRules ? it will go away
	//        and can only be used during install right now
}

// XXX: ideally should go from Info to Info, likely we will move to something else anyway
func currentSnap(newSnap *snap.Info) *Snap {
	currentActiveDir, _ := filepath.EvalSymlinks(filepath.Join(newSnap.BaseDir(), "..", "current"))
	if currentActiveDir == "" {
		return nil
	}

	currentSnap, err := NewInstalledSnap(filepath.Join(currentActiveDir, "meta", "snap.yaml"))
	if err != nil {
		return nil
	}
	return currentSnap
}

func CopyData(newSnap *snap.Info, flags InstallFlags, meter progress.Meter) error {
	dataDir := filepath.Join(dirs.SnapDataDir, newSnap.Name(), newSnap.Version)

	// deal with the data:
	//
	// if there was a previous version, stop it
	// from being active so that it stops running and can no longer be
	// started then copy the data
	//
	// otherwise just create a empty data dir
	oldSnap := currentSnap(newSnap)
	if oldSnap == nil {
		return os.MkdirAll(dataDir, 0755)
	}

	// we need to stop any services and make the commands unavailable
	// so that the data can be safely copied
	if err := UnlinkSnap(oldSnap, meter); err != nil {
		return err
	}

	return copySnapData(newSnap.Name(), oldSnap.Version(), newSnap.Version)
}

func UndoCopyData(newInfo *snap.Info, flags InstallFlags, meter progress.Meter) {
	// XXX we were copying data, assume InhibitHooks was false

	oldSnap := currentSnap(newInfo)
	if oldSnap != nil {
		// reactivate the previously inactivated snap
		if err := ActivateSnap(oldSnap, meter); err != nil {
			logger.Noticef("Setting old version back to active failed: %v", err)
		}
	}

	if err := RemoveSnapData(newInfo.Name(), newInfo.Version); err != nil {
		logger.Noticef("When cleaning up data for %s %s: %v", newInfo.Name(), newInfo.Version, err)
	}
}

func GenerateWrappers(s *Snap, inter interacter) error {
	// add the CLI apps from the snap.yaml
	if err := addPackageBinaries(s.Info()); err != nil {
		return err
	}
	// add the daemons from the snap.yaml
	if err := addPackageServices(s.Info(), inter); err != nil {
		return err
	}
	// add the desktop files
	if err := addPackageDesktopFiles(s.Info()); err != nil {
		return err
	}

	return nil
}

// RemoveGeneratedWrappers removes the generated services, binaries, desktop
// wrappers
func RemoveGeneratedWrappers(s *Snap, inter interacter) error {

	err1 := removePackageBinaries(s.Info())
	if err1 != nil {
		logger.Noticef("Failed to remove binaries for %q: %v", s.Name(), err1)
	}

	err2 := removePackageServices(s.Info(), inter)
	if err2 != nil {
		logger.Noticef("Failed to remove services for %q: %v", s.Name(), err2)
	}

	err3 := removePackageDesktopFiles(s.Info())
	if err3 != nil {
		logger.Noticef("Failed to remove desktop files for %q: %v", s.Name(), err3)
	}

	return firstErr(err1, err2, err3)
}

func UpdateCurrentSymlink(s *Snap, inter interacter) error {
	currentActiveSymlink := filepath.Join(s.basedir, "..", "current")

	if err := os.Remove(currentActiveSymlink); err != nil && !os.IsNotExist(err) {
		logger.Noticef("Failed to remove %q: %v", currentActiveSymlink, err)
	}

	dbase := filepath.Join(dirs.SnapDataDir, s.Name())
	currentDataSymlink := filepath.Join(dbase, "current")
	if err := os.Remove(currentDataSymlink); err != nil && !os.IsNotExist(err) {
		logger.Noticef("Failed to remove %q: %v", currentDataSymlink, err)
	}

	// symlink is relative to parent dir
	if err := os.Symlink(filepath.Base(s.basedir), currentActiveSymlink); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Join(dbase, s.Version()), 0755); err != nil {
		return err
	}

	// FIXME: create {Os,Kernel}Snap type instead of adding special
	//        cases here
	if err := setNextBoot(s); err != nil {
		return err
	}

	return os.Symlink(filepath.Base(s.basedir), currentDataSymlink)
}

func UndoUpdateCurrentSymlink(oldSnap, newSnap *Snap, inter interacter) error {
	if err := removeCurrentSymlink(newSnap, inter); err != nil {
		return err
	}
	return UpdateCurrentSymlink(oldSnap, inter)
}

func removeCurrentSymlink(s *Snap, inter interacter) error {
	var err1, err2 error

	// the snap "current" symlink
	currentActiveSymlink := filepath.Join(s.basedir, "..", "current")
	err1 = os.Remove(currentActiveSymlink)
	if err1 != nil && !os.IsNotExist(err1) {
		logger.Noticef("Failed to remove %q: %v", currentActiveSymlink, err1)
	} else {
		err1 = nil
	}

	// the data "current" symlink
	currentDataSymlink := filepath.Join(dirs.SnapDataDir, s.Name(), "current")
	err2 = os.Remove(currentDataSymlink)
	if err2 != nil && !os.IsNotExist(err2) {
		logger.Noticef("Failed to remove %q: %v", currentDataSymlink, err2)
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
	currentActiveSymlink := filepath.Join(s.basedir, "..", "current")
	currentActiveDir, err := filepath.EvalSymlinks(currentActiveSymlink)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// already active, nothing to do
	if s.basedir == currentActiveDir {
		return nil
	}

	// there is already an active snap
	if currentActiveDir != "" {
		return fmt.Errorf("cannot activate snap while another one is active: %v", currentActiveDir)
	}

	// generate the security policy from the snap.yaml
	// Note that this must happen before binaries/services are
	// generated because serices may get started
	if err := SetupSnapSecurity(s); err != nil {
		return err
	}

	if err := GenerateWrappers(s, inter); err != nil {
		return err
	}

	return UpdateCurrentSymlink(s, inter)
}

// UnlinkSnap deactivates the given active snap.
func UnlinkSnap(s *Snap, inter interacter) error {
	currentSymlink := filepath.Join(s.basedir, "..", "current")
	currentActiveDir, err := filepath.EvalSymlinks(currentSymlink)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrSnapNotActive
		}
		return err
	}
	if s.basedir != currentActiveDir {
		return ErrSnapNotActive
	}

	// remove generated services, binaries, security policy
	err1 := RemoveGeneratedWrappers(s, inter)

	// remove generated security
	err2 := RemoveGeneratedSnapSecurity(s)

	// and finally remove current symlink
	err3 := removeCurrentSymlink(s, inter)

	// FIXME: aggregate errors instead
	return firstErr(err1, err2, err3)
}

// Install installs the given snap file to the system.
//
// It returns the local snap file or an error
func (o *Overlord) Install(snapFilePath string, flags InstallFlags, meter progress.Meter) (sp *snap.Info, err error) {
	if err := CheckSnap(snapFilePath, flags, meter); err != nil {
		return nil, err
	}

	instPath, err := SetupSnap(snapFilePath, flags, meter)
	defer func() {
		if err != nil {
			UndoSetupSnap(instPath, meter)
		}
	}()
	if err != nil {
		return nil, err
	}

	allowUnauth := (flags & AllowUnauthenticated) != 0
	// XXX: soon need optionally to fill or get a sideinfo with at least revision
	newInfo, _, err := openSnapBlob(snapFilePath, allowUnauth, nil)
	if err != nil {
		return nil, err
	}

	// we need this for later
	oldSnap := currentSnap(newInfo)

	// deal with the data
	err = CopyData(newInfo, flags, meter)
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

	// if get this far we know the snap is actually mounted.
	// XXX: use infos further but anyway this is going away mostly
	// once we simplify u-d-f
	newSnap, err := NewInstalledSnap(filepath.Join(instPath, "meta", "snap.yaml"))
	if err != nil {
		return nil, err
	}

	err = ActivateSnap(newSnap, meter)
	defer func() {
		if err != nil && oldSnap != nil {
			if err := ActivateSnap(oldSnap, meter); err != nil {
				logger.Noticef("When setting old %s version back to active: %v", newSnap.Name(), err)
			}
		}
	}()
	if err != nil {
		return nil, err
	}

	return newSnap.Info(), nil
}

// CanInstall checks whether the Snap passes a series of tests required for installation
func canInstall(s *snap.Info, blobf snap.File, allowGadget bool, inter interacter) error {
	// verify we have a valid architecture
	if !arch.IsSupportedArchitecture(s.Architectures) {
		return &ErrArchitectureNotSupported{s.Architectures}
	}

	if s.Type == snap.TypeGadget {
		if !allowGadget {
			if currentGadget, err := getGadget(); err == nil {
				if currentGadget.Name() != s.Name() {
					return ErrGadgetPackageInstall
				}
			} else {
				// there should always be a gadget package now
				return ErrGadgetPackageInstall
			}
		}
	}

	// XXX: can be cleaner later
	currSnap := currentSnap(s)
	var curr *snap.Info
	if currSnap != nil {
		curr = currSnap.Info()
	}

	if err := checkLicenseAgreement(s, blobf, curr, inter); err != nil {
		return err
	}

	return nil
}

// checkLicenseAgreement returns nil if it's ok to proceed with installing the
// package, as deduced from the license agreement (which might involve asking
// the user), or an error that explains the reason why installation should not
// proceed.
func checkLicenseAgreement(s *snap.Info, blobf snap.File, cur *snap.Info, ag agreer) error {
	if s.LicenseAgreement != "explicit" {
		return nil
	}

	if ag == nil {
		return ErrLicenseNotAccepted
	}

	license, err := blobf.MetaMember("license.txt")
	if err != nil || len(license) == 0 {
		return ErrLicenseNotProvided
	}

	// don't ask for the license if
	// * the previous version also asked for license confirmation, and
	// * the license version is the same
	if cur != nil && (cur.LicenseAgreement == "explicit") && cur.LicenseVersion == s.LicenseVersion {
		return nil
	}

	msg := fmt.Sprintf("%s requires that you accept the following license before continuing", s.Name())
	if !ag.Agreed(msg, string(license)) {
		return ErrLicenseNotAccepted
	}

	return nil
}

func CanRemove(s *Snap) bool {
	// Gadget snaps should not be removed as they are a key
	// building block for Gadgets. Prunning non active ones
	// is acceptible.
	if s.m.Type == snap.TypeGadget && s.IsActive() {
		return false
	}

	// You never want to remove an active kernel or OS
	if (s.m.Type == snap.TypeKernel || s.m.Type == snap.TypeOS) && s.IsActive() {
		return false
	}

	if IsBuiltInSoftware(s.Name()) && s.IsActive() {
		return false
	}
	return true
}

// RemoveSnapFiles removes the snap files from the disk
func RemoveSnapFiles(s *Snap, meter progress.Meter) error {
	info := s.Info()
	basedir := info.BaseDir()
	snapFile := info.MountFile()
	// this also ensures that the mount unit stops
	if err := removeSquashfsMount(basedir, meter); err != nil {
		return err
	}

	if err := os.RemoveAll(basedir); err != nil {
		return err
	}

	// best effort(?)
	os.Remove(filepath.Dir(basedir))

	// remove the snap
	if err := os.RemoveAll(snapFile); err != nil {
		return err
	}

	// remove the kernel assets (if any)
	if s.m.Type == snap.TypeKernel {
		if err := removeKernelAssets(info, meter); err != nil {
			logger.Noticef("removing kernel assets failed with %s", err)
		}
	}

	return RemoveAllHWAccess(s.Name())
}

// Uninstall removes the given local snap from the system.
//
// It returns an error on failure
func (o *Overlord) Uninstall(s *Snap, meter progress.Meter) error {
	if !CanRemove(s) {
		return ErrPackageNotRemovable
	}

	if err := UnlinkSnap(s, meter); err != nil && err != ErrSnapNotActive {
		return err
	}

	if err := RemoveSnapFiles(s, meter); err != nil {
		return err
	}

	return RemoveSnapData(s.Name(), s.Version())
}

// SetActive sets the active state of the given snap
//
// It returns an error on failure
func (o *Overlord) SetActive(s *Snap, active bool, meter progress.Meter) error {
	if active {
		// deactivate current first
		if current := ActiveSnapByName(s.Name()); current != nil {
			if err := UnlinkSnap(current, meter); err != nil {
				return err
			}
		}
		return ActivateSnap(s, meter)
	}

	return UnlinkSnap(s, meter)
}

// Configure configures the given snap
//
// It returns an error on failure
func (o *Overlord) Configure(s *Snap, configuration []byte) ([]byte, error) {
	if s.m.Type == snap.TypeOS {
		return coreConfig(configuration)
	}

	return snapConfig(s.basedir, configuration)
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
