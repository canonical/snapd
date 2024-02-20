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
	"github.com/snapcore/snapd/osutil/squashfs"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
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
	if err := addMountUnit(s, b.preseed, meter); err != nil {
		return snapType, nil, err
	}

	t := s.Type()
	if !setupOpts.SkipKernelExtraction {
		if err := boot.Kernel(s, t, dev).ExtractKernelAssets(snapf); err != nil {
			return snapType, nil, fmt.Errorf("cannot install kernel: %s", err)
		}
	}

	installRecord = &InstallRecord{TargetSnapExisted: didNothing}
	return t, installRecord, nil
}

func earlyMountOptions() []string {
	return append([]string{"nodev"}, squashfs.StandardOptions()...)
}

// SetupKernelSnap does extra configuration for kernel snaps.
func (b Backend) SetupKernelSnap(instanceName string, rev snap.Revision, meter progress.Meter) (err error) {
	var sysd systemd.Systemd
	if b.preseed {
		sysd = systemd.NewEmulationMode(dirs.GlobalRootDir)
	} else {
		sysd = systemd.New(systemd.SystemMode, meter)
	}

	// Create early mount for the snap
	cpi := snap.MinimalSnapContainerPlaceInfo(instanceName, rev)

	earlyMountDir := kernel.EarlyKernelMountDir(instanceName, rev)
	addMountUnitOptions := &systemd.MountUnitOptions{
		MountUnitType: systemd.BeforeDriversLoadMountUnit,
		Lifetime:      systemd.Persistent,
		Description:   "Early mount unit for kernel snap",
		What:          cpi.MountFile(),
		Where:         dirs.StripRootDir(earlyMountDir),
		Fstype:        "squashfs",
		Options:       earlyMountOptions(),
	}
	_, err = sysd.EnsureMountUnitFileWithOptions(addMountUnitOptions)
	if err != nil {
		return fmt.Errorf("cannot create mount in %q: %w", earlyMountDir, err)
	}

	// Clean-up mount if the kernel tree cannot be built
	defer func() {
		if err == nil {
			return
		}
		if e := sysd.RemoveMountUnitFile(earlyMountDir); e != nil {
			meter.Notify(fmt.Sprintf("while trying to clean up due to previous failure: %v", e))
		}
	}()

	// Build kernel tree that will be mounted from initramfs
	return kernel.EnsureKernelDriversTree(instanceName, rev,
		earlyMountDir, nil, &kernel.KernelDriversTreeOptions{KernelInstall: true})
}

func (b Backend) RemoveKernelSnapSetup(instanceName string, rev snap.Revision, meter progress.Meter) error {
	if err := kernel.RemoveKernelDriversTree(instanceName, rev); err != nil {
		return err
	}

	var sysd systemd.Systemd
	if b.preseed {
		sysd = systemd.NewEmulationMode(dirs.GlobalRootDir)
	} else {
		sysd = systemd.New(systemd.SystemMode, meter)
	}

	earlyMountDir := kernel.EarlyKernelMountDir(instanceName, rev)
	return sysd.RemoveMountUnitFile(earlyMountDir)
}

// SetupComponent prepares and mounts a component for further processing.
func (b Backend) SetupComponent(compFilePath string, compPi snap.ContainerPlaceInfo, dev snap.Device, meter progress.Meter) (installRecord *InstallRecord, err error) {
	// This assumes that the component was already verified or --dangerous was used.

	_, snapf, oErr := OpenComponentFile(compFilePath)
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
	if err := addMountUnit(compPi, b.preseed, meter); err != nil {
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

	// Remove /snap/<snap_instance>/components/<snap_rev>/<comp_name>
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

	// TODO should we check here if there are other components installed
	// for this snap revision or for other revisions and if not delete
	// <snap_rev>/ and maybe also components/<snap_rev>/?

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
	// Remove /snap/<snap_instance>/components/<snap_rev>/<comp_name>
	os.Remove(compMountDir)
	// and /snap/<snap_instance>/components/<snap_rev> (might fail
	// if there are other components installed for this revision)
	os.Remove(filepath.Dir(compMountDir))
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

// SetupKernelModulesComponents changes kernel-modules configuration by adding
// compsToInstall. The components currently active are currentComps, while
// ksnapName and ksnapRev identify the currently active kernel.
func (b Backend) SetupKernelModulesComponents(compsToInstall, currentComps []*snap.ComponentSideInfo, ksnapName string, ksnapRev snap.Revision, meter progress.Meter) (err error) {
	sysd := newSystemd(b.preseed, meter)

	// compsToRemove will contain the old revisions of components in compsToInstall
	newActiveComps, compsToRemove := mergeCompSideInfosUpdatingRev(currentComps, compsToInstall)

	return moveKModsComponentsState(currentComps, newActiveComps,
		compsToInstall, compsToRemove, ksnapName, ksnapRev, sysd,
		"after failure to set-up kernel modules components")
}

// RemoveKernelModulesComponentsSetup changes kernel-modules configuration by
// removing compsToRemove and making the final state consider only finalComps.
func (b Backend) RemoveKernelModulesComponentsSetup(compsToRemove, finalComps []*snap.ComponentSideInfo, ksnapName string, ksnapRev snap.Revision, meter progress.Meter) (err error) {
	sysd := newSystemd(b.preseed, meter)

	// compsToAdd will contain previous revision (that we want to restore)
	// of compsToRemove
	currentActiveComps, compsToAdd := mergeCompSideInfosUpdatingRev(finalComps, compsToRemove)

	return moveKModsComponentsState(currentActiveComps, finalComps,
		compsToAdd, compsToRemove, ksnapName, ksnapRev, sysd,
		"after failure to remove set-up of kernel modules components")
}

// mergeCompSideInfosUpdatingRev returns a merged list from two lists of
// ComponentSideInfo, and a list with the intersection, using the criteria of
// the elements having the same ComponentRef. The rest of the data for an
// element will come from comps2 if ComponentRef is the same in comps1 and
// comps2, that is, the revision is updated in that case.
func mergeCompSideInfosUpdatingRev(comps1, comps2 []*snap.ComponentSideInfo) (merged, instersect []*snap.ComponentSideInfo) {
	numInComps2 := len(comps2)
	comps2Map := make(map[naming.ComponentRef]*snap.ComponentSideInfo, numInComps2)
	for _, cti := range comps2 {
		comps2Map[cti.Component] = cti
	}
	merged = append(merged, comps2...)
	instersect = []*snap.ComponentSideInfo{}
	for _, instComp := range comps1 {
		if _, ok := comps2Map[instComp.Component]; ok {
			// Component is already in "merged"
			instersect = append(instersect, instComp)
		} else {
			// No change for this component
			merged = append(merged, instComp)
		}
	}

	return merged, instersect
}

// moveKModsComponentsState changes kernel-modules set-up from currentComps to
// finalComps, and adds/removes toAdd and toRemove components respectively.
func moveKModsComponentsState(currentComps, finalComps, toAdd, toRemove []*snap.ComponentSideInfo, ksnapName string, ksnapRev snap.Revision, sysd systemd.Systemd, cleanErrMsg string) (err error) {
	// method is idempotent as the called methods are too

	// on error: re-create removed mount units, revert kernel tree,
	// clean-up newly created unit
	removedMnts := []*snap.ComponentSideInfo{}
	haveNewTree := false
	createdMnts := []*snap.ComponentSideInfo{}
	defer func() {
		if err == nil {
			return
		}
		cleanupAfterSetupError(removedMnts, createdMnts, haveNewTree,
			currentComps, ksnapName, ksnapRev, sysd, cleanErrMsg)
	}()

	// 1. Create new mounts
	for _, csi := range toAdd {
		if err := createKModsMount(csi, ksnapName, ksnapRev, sysd); err != nil {
			return err
		}
		createdMnts = append(createdMnts, csi)
	}

	// 2. Build kernel tree with the components in the final state
	if err := kernel.EnsureKernelDriversTree(ksnapName, ksnapRev,
		kernel.EarlyKernelMountDir(ksnapName, ksnapRev), finalComps,
		&kernel.KernelDriversTreeOptions{KernelInstall: false}); err != nil {
		return err
	}
	haveNewTree = true

	// 3. Remove mounts we do not want anymore
	for _, csi := range toRemove {
		if err := removeKModsMount(csi, ksnapName, ksnapRev, sysd); err != nil {
			return err
		}
		removedMnts = append(removedMnts, csi)
	}

	return nil
}

func cleanupAfterSetupError(createMnts, removeMnts []*snap.ComponentSideInfo, switchTree bool, currentComps []*snap.ComponentSideInfo, ksnapName string, ksnapRev snap.Revision, sysd systemd.Systemd, msg string) {
	for _, csi := range createMnts {
		if e := createKModsMount(csi, ksnapName, ksnapRev, sysd); e != nil {
			logger.Noticef("while restoring removed mounts %s: %v", msg, e)
		}
	}
	if switchTree {
		if e := kernel.EnsureKernelDriversTree(ksnapName, ksnapRev,
			kernel.EarlyKernelMountDir(ksnapName, ksnapRev),
			currentComps,
			&kernel.KernelDriversTreeOptions{
				KernelInstall: false}); e != nil {
			logger.Noticef("while restoring kernel tree %s: %v", msg, e)
		}
	}
	for _, csi := range removeMnts {
		if e := removeKModsMount(csi, ksnapName, ksnapRev, sysd); e != nil {
			logger.Noticef("while removing mounts %s: %v", msg, e)
		}
	}
}

func createKModsMount(csi *snap.ComponentSideInfo, ksnapName string, ksnapRev snap.Revision, sysd systemd.Systemd) error {
	compName := csi.Component.ComponentName
	compRev := csi.Revision
	cpi := snap.MinimalComponentContainerPlaceInfo(compName, compRev, ksnapName, ksnapRev)
	earlyMountDir := kernel.EarlyKernelModsComponentMountDir(
		compName, compRev, ksnapName, ksnapRev)

	addMountUnitOptions := &systemd.MountUnitOptions{
		MountUnitType: systemd.BeforeDriversLoadMountUnit,
		Lifetime:      systemd.Persistent,
		Description:   "Early mount for kernel-modules component",
		What:          cpi.MountFile(),
		Where:         dirs.StripRootDir(earlyMountDir),
		Fstype:        "squashfs",
		Options:       earlyMountOptions(),
	}
	_, err := sysd.EnsureMountUnitFileWithOptions(addMountUnitOptions)
	if err != nil {
		return fmt.Errorf("cannot create mount in %q: %w", earlyMountDir, err)
	}
	return nil
}

func removeKModsMount(csi *snap.ComponentSideInfo, ksnapName string, ksnapRev snap.Revision, sysd systemd.Systemd) error {
	compName := csi.Component.ComponentName
	compRev := csi.Revision
	earlyMountDir := kernel.EarlyKernelModsComponentMountDir(
		compName, compRev, ksnapName, ksnapRev)

	if err := sysd.RemoveMountUnitFile(earlyMountDir); err != nil {
		return fmt.Errorf("cannot remove mount in %q: %w", earlyMountDir, err)
	}
	return nil
}

func newSystemd(preseed bool, meter progress.Meter) systemd.Systemd {
	// TODO should we care about preseeding? Installing a component
	// separately from a snap is probably not happening in that case.
	if preseed {
		return systemd.NewEmulationMode(dirs.GlobalRootDir)
	}
	return systemd.New(systemd.SystemMode, meter)
}
