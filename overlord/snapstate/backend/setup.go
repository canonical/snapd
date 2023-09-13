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
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
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

// UndoSetupSnap undoes the work of SetupSnap using RemoveSnapFiles.
func (b Backend) UndoSetupSnap(s snap.PlaceInfo, typ snap.Type, installRecord *InstallRecord, dev snap.Device, meter progress.Meter) error {
	return b.RemoveSnapFiles(s, typ, installRecord, dev, meter)
}

// RemoveSnapInhibitLock removes the file controlling inhibition of "snap run".
func (b Backend) RemoveSnapInhibitLock(instanceName string) error {
	return runinhibit.RemoveLockFile(instanceName)
}
