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

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/squashfs"
)

// Overlord is responsible for the overall system state.
type Overlord struct {
}

// CheckSnap ensures that the snap can be installed
func CheckSnap(snapFilePath, developer string, flags InstallFlags, meter progress.Meter) error {
	allowGadget := (flags & AllowGadget) != 0
	allowUnauth := (flags & AllowUnauthenticated) != 0

	// we do not Verify() the package here. This is done earlier in
	// NewSnapFile() to ensure that we do not mount/inspect
	// potentially dangerous snaps

	// warning: NewSnapFile generates a new sideloaded version
	//          everytime it is run.
	//          so all paths on disk are different even if the same snap
	s, err := NewSnapFile(snapFilePath, developer, allowUnauth)
	if err != nil {
		return err
	}

	// we do not security Verify() (check hashes) the package here.
	// This is done earlier in
	// NewSnapFile() to ensure that we do not mount/inspect
	// potentially dangerous snaps
	return canInstall(s, allowGadget, meter)
}

// SetupSnap does prepare and mount the snap for further processing
// It returns the installed path and an error
func SetupSnap(snapFilePath, developer string, flags InstallFlags, meter progress.Meter) (string, error) {
	inhibitHooks := (flags & InhibitHooks) != 0
	allowUnauth := (flags & AllowUnauthenticated) != 0

	// warning: NewSnapFile generates a new sideloaded version
	//          everytime it is run
	//          so all paths on disk are different even if the same snap
	s, err := NewSnapFile(snapFilePath, developer, allowUnauth)
	if err != nil {
		return s.instdir, err
	}

	// the "gadget" snaps are special
	if s.Type() == snap.TypeGadget {
		if err := installGadgetHardwareUdevRules(s.m); err != nil {
			return s.instdir, err
		}
	}

	if err := os.MkdirAll(s.instdir, 0755); err != nil {
		logger.Noticef("Can not create %q: %v", s.instdir, err)
		return s.instdir, err
	}

	if err := s.deb.Install(s.instdir); err != nil {
		return s.instdir, err
	}

	// generate the mount unit for the squashfs
	if err := addSquashfsMount(s.m, s.instdir, inhibitHooks, meter); err != nil {
		return s.instdir, err
	}

	// FIXME: special handling is bad 'mkay
	if s.m.Type == snap.TypeKernel {
		if err := extractKernelAssets(s, meter, flags); err != nil {
			return s.instdir, fmt.Errorf("failed to install kernel %s", err)
		}
	}

	return s.instdir, err
}

func UndoSetupSnap(installDir, developer string, meter progress.Meter) {
	if s, err := NewInstalledSnap(filepath.Join(installDir, "meta", "snap.yaml"), developer); err == nil {
		if s.Type() == snap.TypeKernel {
			if err := removeKernelAssets(s, meter); err != nil {
				logger.Noticef("Failed to cleanup kernel assets %q: %v", installDir, err)
			}
		}
		if err := removeSquashfsMount(s.m, s.basedir, meter); err != nil {
			fullName := QualifiedName(s.Info())
			logger.Noticef("Failed to remove mount unit for  %s: %s", fullName, err)
		}
	}
	if err := os.RemoveAll(installDir); err != nil && !os.IsNotExist(err) {
		logger.Noticef("Failed to remove %q: %v", installDir, err)
	}

	// FIXME: do we need to undo installGadgetHardwareUdevRules via
	//        cleanupGadgetHardwareUdevRules ? it will go away
	//        and can only be used during install right now
}

func currentSnap(newSnap *Snap) *Snap {
	currentActiveDir, _ := filepath.EvalSymlinks(filepath.Join(newSnap.basedir, "..", "current"))
	if currentActiveDir == "" {
		return nil
	}

	currentSnap, err := NewInstalledSnap(filepath.Join(currentActiveDir, "meta", "snap.yaml"), newSnap.developer)
	if err != nil {
		return nil
	}
	return currentSnap
}

func CopyData(newSnap *Snap, flags InstallFlags, meter progress.Meter) error {
	inhibitHooks := (flags & InhibitHooks) != 0

	fullName := QualifiedName(newSnap.Info())
	dataDir := filepath.Join(dirs.SnapDataDir, fullName, newSnap.Version())

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

	// we need to stop making it active
	if err := oldSnap.deactivate(inhibitHooks, meter); err != nil {
		return err
	}

	return copySnapData(fullName, oldSnap.Version(), newSnap.Version())
}

func UndoCopyData(newSnap *Snap, flags InstallFlags, meter progress.Meter) {
	inhibitHooks := (flags & InhibitHooks) != 0

	fullName := QualifiedName(newSnap.Info())
	oldSnap := currentSnap(newSnap)
	if oldSnap != nil {
		if err := oldSnap.activate(inhibitHooks, meter); err != nil {
			logger.Noticef("Setting old version back to active failed: %v", err)
		}
	}

	if err := removeSnapData(fullName, newSnap.Version()); err != nil {
		logger.Noticef("When cleaning up data for %s %s: %v", newSnap.Name(), newSnap.Version(), err)
	}
}

func FinalizeSnap(newSnap *Snap, flags InstallFlags, meter progress.Meter) error {
	inhibitHooks := (flags & InhibitHooks) != 0
	if inhibitHooks {
		return nil
	}
	return ActivateSnap(newSnap, inhibitHooks, meter)
}

func UndoFinalizeSnap(oldSnap, newSnap *Snap, flags InstallFlags, meter progress.Meter) {
	inhibitHooks := (flags & InhibitHooks) != 0
	if oldSnap == nil {
		return
	}
	if err := ActivateSnap(oldSnap, inhibitHooks, meter); err != nil {
		logger.Noticef("When setting old %s version back to active: %v", newSnap.Name(), err)
	}
}

func ActivateSnap(s *Snap, inhibitHooks bool, inter interacter) error {
	currentActiveSymlink := filepath.Join(s.basedir, "..", "current")
	currentActiveDir, _ := filepath.EvalSymlinks(currentActiveSymlink)

	// already active, nothing to do
	if s.basedir == currentActiveDir {
		return nil
	}

	// there is already an active snap
	if currentActiveDir != "" {
		// TODO: support switching developers
		oldYaml := filepath.Join(currentActiveDir, "meta", "snap.yaml")
		oldSnap, err := NewInstalledSnap(oldYaml, s.developer)
		if err != nil {
			return err
		}
		if err := oldSnap.deactivate(inhibitHooks, inter); err != nil {
			return err
		}
	}

	// generate the security policy from the snap.yaml
	// Note that this must happen before binaries/services are
	// generated because serices may get started
	appsDir := filepath.Join(dirs.SnapSnapsDir, QualifiedName(s.Info()), s.Version())
	if err := generatePolicy(s.m, appsDir); err != nil {
		return err
	}

	// add the CLI apps from the snap.yaml
	if err := addPackageBinaries(s.m, s.basedir); err != nil {
		return err
	}
	// add the daemons from the snap.yaml
	if err := addPackageServices(s.m, s.basedir, inhibitHooks, inter); err != nil {
		return err
	}
	// add the desktop files
	if err := addPackageDesktopFiles(s.m, s.basedir); err != nil {
		return err
	}

	if err := os.Remove(currentActiveSymlink); err != nil && !os.IsNotExist(err) {
		logger.Noticef("Failed to remove %q: %v", currentActiveSymlink, err)
	}

	dbase := filepath.Join(dirs.SnapDataDir, QualifiedName(s.Info()))
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

func DeactivateSnap(s *Snap, inhibitHooks bool, inter interacter) error {
	currentSymlink := filepath.Join(s.basedir, "..", "current")

	// sanity check
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
	if err := removePackageBinaries(s.m, s.basedir); err != nil {
		return err
	}

	if err := removePackageServices(s.m, s.basedir, inter); err != nil {
		return err
	}

	if err := removePackageDesktopFiles(s.m); err != nil {
		return err
	}

	if err := removePolicy(s.m, s.basedir); err != nil {
		return err
	}

	// and finally the current symlink
	if err := os.Remove(currentSymlink); err != nil {
		logger.Noticef("Failed to remove %q: %v", currentSymlink, err)
	}

	currentDataSymlink := filepath.Join(dirs.SnapDataDir, QualifiedName(s.Info()), "current")
	if err := os.Remove(currentDataSymlink); err != nil && !os.IsNotExist(err) {
		logger.Noticef("Failed to remove %q: %v", currentDataSymlink, err)
	}

	return nil
}

// Install installs the given snap file to the system.
//
// It returns the local snap file or an error
func (o *Overlord) Install(snapFilePath string, developer string, flags InstallFlags, meter progress.Meter) (sp *Snap, err error) {
	if err := CheckSnap(snapFilePath, developer, flags, meter); err != nil {
		return nil, err
	}

	instPath, err := SetupSnap(snapFilePath, developer, flags, meter)
	defer func() {
		if err != nil {
			UndoSetupSnap(instPath, developer, meter)
		}
	}()
	if err != nil {
		return nil, err
	}

	// we have an installed snap at this point
	newSnap, err := NewInstalledSnap(filepath.Join(instPath, "meta", "snap.yaml"), developer)
	if err != nil {
		return nil, err
	}

	// we need this for later
	oldSnap := currentSnap(newSnap)

	// deal with the data
	err = CopyData(newSnap, flags, meter)
	defer func() {
		if err != nil {
			UndoCopyData(newSnap, flags, meter)
		}
	}()
	if err != nil {
		return nil, err
	}

	// and finally make active
	err = FinalizeSnap(newSnap, flags, meter)
	defer func() {
		if err != nil {
			UndoFinalizeSnap(oldSnap, newSnap, flags, meter)
		}
	}()
	if err != nil {
		return nil, err
	}

	return newSnap, nil
}

// CanInstall checks whether the Snap passes a series of tests required for installation
func canInstall(s *SnapFile, allowGadget bool, inter interacter) error {
	if err := checkForPackageInstalled(s.m, s.Developer()); err != nil {
		return err
	}

	// verify we have a valid architecture
	if !arch.IsSupportedArchitecture(s.m.Architectures) {
		return &ErrArchitectureNotSupported{s.m.Architectures}
	}

	if s.Type() == snap.TypeGadget {
		if !allowGadget {
			if currentGadget, err := getGadget(); err == nil {
				if currentGadget.Name != s.Name() {
					return ErrGadgetPackageInstall
				}
			} else {
				// there should always be a gadget package now
				return ErrGadgetPackageInstall
			}
		}
	}

	curr, _ := filepath.EvalSymlinks(filepath.Join(s.instdir, "..", "current"))
	if err := checkLicenseAgreement(s.m, inter, s.deb, curr); err != nil {
		return err
	}

	return nil
}

// Uninstall removes the given local snap from the system.
//
// It returns an error on failure
func (o *Overlord) Uninstall(s *Snap, meter progress.Meter) error {
	// Gadget snaps should not be removed as they are a key
	// building block for Gadgets. Prunning non active ones
	// is acceptible.
	if s.m.Type == snap.TypeGadget && s.IsActive() {
		return ErrPackageNotRemovable
	}

	// You never want to remove an active kernel or OS
	if (s.m.Type == snap.TypeKernel || s.m.Type == snap.TypeOS) && s.IsActive() {
		return ErrPackageNotRemovable
	}

	if IsBuiltInSoftware(s.Name()) && s.IsActive() {
		return ErrPackageNotRemovable
	}

	if err := s.deactivate(false, meter); err != nil && err != ErrSnapNotActive {
		return err
	}

	// ensure mount unit stops
	if err := removeSquashfsMount(s.m, s.basedir, meter); err != nil {
		return err
	}

	if err := os.RemoveAll(s.basedir); err != nil {
		return err
	}

	// best effort(?)
	os.Remove(filepath.Dir(s.basedir))

	// remove the snap
	if err := os.RemoveAll(squashfs.BlobPath(s.basedir)); err != nil {
		return err
	}

	// remove the kernel assets (if any)
	if s.m.Type == snap.TypeKernel {
		if err := removeKernelAssets(s, meter); err != nil {
			logger.Noticef("removing kernel assets failed with %s", err)
		}
	}

	return RemoveAllHWAccess(QualifiedName(s.Info()))
}

// SetActive sets the active state of the given snap
//
// It returns an error on failure
func (o *Overlord) SetActive(s *Snap, active bool, meter progress.Meter) error {
	if active {
		return s.activate(false, meter)
	}

	return s.deactivate(false, meter)
}

// Configure configures the given snap
//
// It returns an error on failure
func (o *Overlord) Configure(s *Snap, configuration []byte) ([]byte, error) {
	if s.m.Type == snap.TypeOS {
		return coreConfig(configuration)
	}

	return snapConfig(s.basedir, s.developer, configuration)
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

		developer, _ := developerFromYamlPath(realpath)
		snap, err := NewInstalledSnap(realpath, developer)
		if err != nil {
			return nil, err
		}
		snaps = append(snaps, snap)
	}

	return snaps, nil
}
