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
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

// InstallRecord keeps a record of what installation effectively did as hints
// about what needs to be undone in case of failure.
type InstallRecord struct {
	// TargetSnapExisted indicates that the target .snap file under /var/lib/snapd/snap already existed when the
	// backend attempted SetupSnap() through squashfs Install() and should be kept.
	TargetSnapExisted bool `json:"target-snap-existed,omitempty"`
}

type SetupSnapOptions struct {
	SkipKernelExtraction bool
}

// SetupSnap does prepare and mount the snap for further processing.
func (b Backend) SetupSnap(snapFilePath, instanceName string, sideInfo *snap.SideInfo, dev snap.Device, setupOpts *SetupSnapOptions, meter progress.Meter) (snapType snap.Type, installRecord *InstallRecord, err error) {
	if setupOpts == nil {
		setupOpts = &SetupSnapOptions{}
	}

	// This assumes that the snap was already verified or --dangerous was used.

	s, snapf, oErr := OpenSnapFile(snapFilePath, sideInfo)
	if oErr != nil {
		return snapType, nil, oErr
	}

	// update instance key to what was requested
	_, s.InstanceKey = snap.SplitInstanceName(instanceName)

	instdir := s.MountDir()

	defer func() {
		if err == nil {
			return
		}

		// this may remove the snap from /var/lib/snapd/snaps depending on installRecord
		if e := b.RemoveSnapFiles(s, s.Type(), installRecord, dev, meter); e != nil {
			meter.Notify(fmt.Sprintf("while trying to clean up due to previous failure: %v", e))
		}
	}()

	if err := os.MkdirAll(instdir, 0755); err != nil {
		return snapType, nil, err
	}

	if s.InstanceKey != "" {
		err := os.MkdirAll(snap.BaseDir(s.SnapName()), 0755)
		if err != nil && !os.IsExist(err) {
			return snapType, nil, err
		}
	}

	// in uc20+ and classic with modes run mode, all snaps must be on the
	// same device
	opts := &snap.InstallOptions{}
	if dev.HasModeenv() && dev.RunMode() {
		opts.MustNotCrossDevices = true
	}

	var didNothing bool
	if didNothing, err = snapf.Install(s.MountFile(), instdir, opts); err != nil {
		return snapType, nil, err
	}

	// generate the mount unit for the squashfs
	t := s.Type()
	mountFlags := systemd.EnsureMountUnitFlags{
		PreventRestartIfModified: false,
		// We need early mounts only for UC20+/hybrid, also 16.04
		// systemd seems to be buggy if we enable this.
		StartBeforeDriversLoad: t == snap.TypeKernel && dev.HasModeenv(),
	}
	if err := addMountUnit(s, mountFlags, newSystemd(b.preseed, meter)); err != nil {
		return snapType, nil, err
	}

	if !setupOpts.SkipKernelExtraction {
		if err := boot.Kernel(s, t, dev).ExtractKernelAssets(snapf); err != nil {
			return snapType, nil, fmt.Errorf("cannot install kernel: %s", err)
		}
	}

	installRecord = &InstallRecord{TargetSnapExisted: didNothing}
	return t, installRecord, nil
}

// SetupKernelSnap does extra configuration for kernel snaps.
func (b Backend) SetupKernelSnap(instanceName string, rev snap.Revision, meter progress.Meter) (err error) {
	// Build kernel tree that will be mounted from initramfs
	cpi := snap.MinimalSnapContainerPlaceInfo(instanceName, rev)
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, instanceName, rev)

	return kernel.EnsureKernelDriversTree(
		kernel.MountPoints{
			Current: cpi.MountDir(),
			Target:  cpi.MountDir()},
		nil, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: true})
}

func (b Backend) RemoveKernelSnapSetup(instanceName string, rev snap.Revision, meter progress.Meter) error {
	kernelTree := kernel.DriversTreeDir(dirs.GlobalRootDir, instanceName, rev)
	return kernel.RemoveKernelDriversTree(kernelTree)
}

// SetupComponent prepares and mounts a component for further processing.
func (b Backend) SetupComponent(compFilePath string, compPi snap.ContainerPlaceInfo, dev snap.Device, meter progress.Meter) (installRecord *InstallRecord, err error) {
	// This assumes that the component was already verified or --dangerous was used.

	compInfo, snapf, oErr := OpenComponentFile(compFilePath, nil, nil)
	if oErr != nil {
		return nil, oErr
	}

	defer func() {
		if err == nil {
			return
		}

		// this may remove the component from /var/lib/snapd/snaps
		// depending on installRecord
		if e := b.RemoveComponentFiles(compPi, installRecord, dev, meter); e != nil {
			meter.Notify(fmt.Sprintf(
				"while trying to clean up due to previous failure: %v", e))
		}
	}()

	// Create mount dir for the component
	mntDir := compPi.MountDir()
	if err := os.MkdirAll(mntDir, 0755); err != nil {
		return nil, err
	}

	// in uc20+ and classic with modes run mode, all snaps must be on the
	// same device
	opts := &snap.InstallOptions{}
	if dev.HasModeenv() && dev.RunMode() {
		opts.MustNotCrossDevices = true
	}

	// Copy file to snaps folder
	var didNothing bool
	if didNothing, err = snapf.Install(compPi.MountFile(), mntDir, opts); err != nil {
		return nil, err
	}

	// generate the mount unit for the squashfs
	mountFlags := systemd.EnsureMountUnitFlags{
		PreventRestartIfModified: false,
		// We need early mounts only for UC20+/hybrid, also 16.04
		// systemd seems to be buggy if we enable this.
		StartBeforeDriversLoad: compInfo.Type == snap.KernelModulesComponent && dev.HasModeenv(),
	}
	if err := addMountUnit(compPi, mountFlags, newSystemd(b.preseed, meter)); err != nil {
		return nil, err
	}

	installRecord = &InstallRecord{TargetSnapExisted: didNothing}
	return installRecord, nil
}

// RemoveSnapFiles removes the snap files from the disk after unmounting the snap.
func (b Backend) RemoveSnapFiles(s snap.PlaceInfo, typ snap.Type, installRecord *InstallRecord, dev snap.Device, meter progress.Meter) error {
	mountDir := s.MountDir()

	// this also ensures that the mount unit stops
	if err := removeMountUnit(mountDir, meter); err != nil {
		return err
	}

	if err := os.RemoveAll(mountDir); err != nil {
		return err
	}

	// snapPath may either be a file or a (broken) symlink to a dir
	snapPath := s.MountFile()
	if _, err := os.Lstat(snapPath); err == nil {
		// remove the kernel assets (if any)
		if err := boot.Kernel(s, typ, dev).RemoveKernelAssets(); err != nil {
			return err
		}

		// don't remove snap path if it existed before snap installation was attempted
		// and is a symlink, which is the case with kernel/core snaps during seeding.
		keepSeededSnap := installRecord != nil && installRecord.TargetSnapExisted && osutil.IsSymlink(snapPath)
		if !keepSeededSnap {
			// remove the snap
			if err := os.RemoveAll(snapPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// RemoveComponentFiles unmounts and removes component files from the disk.
func (b Backend) RemoveComponentFiles(cpi snap.ContainerPlaceInfo, installRecord *InstallRecord, dev snap.Device, meter progress.Meter) error {
	// this also ensures that the mount unit stops
	if err := removeMountUnit(cpi.MountDir(), meter); err != nil {
		return err
	}

	// Remove /snap/<snap_instance>/components/mnt/<comp_name>/<comp_rev>
	if err := os.RemoveAll(cpi.MountDir()); err != nil {
		return err
	}

	compFilePath := cpi.MountFile()
	if _, err := os.Lstat(compFilePath); err == nil {
		// remove the component
		if err := os.RemoveAll(compFilePath); err != nil {
			return err
		}
	}

	return nil
}

func (b Backend) RemoveSnapDir(s snap.PlaceInfo, hasOtherInstances bool) error {
	mountDir := s.MountDir()

	snapName, instanceKey := snap.SplitInstanceName(s.InstanceName())
	if instanceKey != "" {
		// always ok to remove instance specific one, failure to remove
		// is ok, there may be other revisions
		os.Remove(filepath.Dir(mountDir))
	}
	if !hasOtherInstances {
		// remove only if not used by other instances of the same snap,
		// failure to remove is ok, there may be other revisions
		os.Remove(snap.BaseDir(snapName))
	}
	return nil
}

func (b Backend) RemoveComponentDir(cpi snap.ContainerPlaceInfo) error {
	compMountDir := cpi.MountDir()
	// Remove last 3 directories of
	// /snap/<snap_instance>/components/mnt/<comp_name>/ if they
	// are empty (last one should be). Note that subdirectories with snap
	// revisions are handled by UnlinkComponent.
	for i := 0; i < 3; i++ {
		compMountDir = filepath.Dir(compMountDir)
		if err := os.Remove(compMountDir); err != nil {
			break
		}
	}

	return nil
}

// UndoSetupSnap undoes the work of SetupSnap using RemoveSnapFiles.
func (b Backend) UndoSetupSnap(s snap.PlaceInfo, typ snap.Type, installRecord *InstallRecord, dev snap.Device, meter progress.Meter) error {
	return b.RemoveSnapFiles(s, typ, installRecord, dev, meter)
}

// UndoSetupComponent undoes the work of SetupComponent using RemoveComponentFiles.
func (b Backend) UndoSetupComponent(cpi snap.ContainerPlaceInfo, installRecord *InstallRecord, dev snap.Device, meter progress.Meter) error {
	return b.RemoveComponentFiles(cpi, installRecord, dev, meter)
}

// RemoveSnapInhibitLock removes the file controlling inhibition of "snap run".
func (b Backend) RemoveSnapInhibitLock(instanceName string) error {
	return runinhibit.RemoveLockFile(instanceName)
}

// SetupKernelModulesComponents changes kernel-modules configuration by
// installing currentComps. The components currently active are currentComps,
// while ksnapName and ksnapRev identify the currently active kernel.
func (b Backend) SetupKernelModulesComponents(currentComps, finalComps []*snap.ComponentSideInfo, ksnapName string, ksnapRev snap.Revision, meter progress.Meter) (err error) {
	return moveKModsComponentsState(
		currentComps, finalComps, ksnapName, ksnapRev,
		"after failure to set-up kernel modules components")
}

// moveKModsComponentsState changes kernel-modules set-up from currentComps to
// finalComps, for the kernel/revision specified by ksnapName/ksnapRev.
func moveKModsComponentsState(currentComps, finalComps []*snap.ComponentSideInfo, ksnapName string, ksnapRev snap.Revision, cleanErrMsg string) (err error) {
	cpi := snap.MinimalSnapContainerPlaceInfo(ksnapName, ksnapRev)
	kMntPts := kernel.MountPoints{
		Current: cpi.MountDir(),
		Target:  cpi.MountDir(),
	}
	destDir := kernel.DriversTreeDir(dirs.GlobalRootDir, ksnapName, ksnapRev)
	finalCompsMntPts := make([]kernel.ModulesCompMountPoints, len(finalComps))
	for i, csi := range finalComps {
		compPlaceInfo := snap.MinimalComponentContainerPlaceInfo(csi.Component.ComponentName,
			csi.Revision, ksnapName)
		finalCompsMntPts[i] = kernel.ModulesCompMountPoints{
			Name: csi.Component.ComponentName,
			MountPoints: kernel.MountPoints{
				Current: compPlaceInfo.MountDir(),
				Target:  compPlaceInfo.MountDir(),
			},
		}
	}

	if err := kernel.EnsureKernelDriversTree(kMntPts, finalCompsMntPts, destDir,
		&kernel.KernelDriversTreeOptions{KernelInstall: false}); err != nil {

		currentCompsConts := make([]snap.ContainerPlaceInfo, len(currentComps))
		for i, csi := range currentComps {
			currentCompsConts[i] = snap.MinimalComponentContainerPlaceInfo(
				csi.Component.ComponentName, csi.Revision, ksnapName)
		}
		if e := kernel.EnsureKernelDriversTree(kMntPts, finalCompsMntPts, destDir,
			&kernel.KernelDriversTreeOptions{KernelInstall: false}); e != nil {
			logger.Noticef("while restoring kernel tree %s: %v", cleanErrMsg, e)
		}

		return err
	}

	return nil
}

func newSystemd(preseed bool, meter progress.Meter) systemd.Systemd {
	if preseed {
		return systemd.NewEmulationMode(dirs.GlobalRootDir)
	}
	return systemd.New(systemd.SystemMode, meter)
}
