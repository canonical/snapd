// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package main

import (
	"fmt"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// SystemProfileUpdate contains information about update to system-wide mount namespace.
type SystemProfileUpdate struct {
	commonProfileUpdate
}

// NewSystemProfileUpdate returns encapsulated information for performing a per-user mount namespace update.
func NewSystemProfileUpdate(instanceName string, fromSnapConfine bool) *SystemProfileUpdate {
	return &SystemProfileUpdate{commonProfileUpdate: commonProfileUpdate{
		instanceName:       instanceName,
		fromSnapConfine:    fromSnapConfine,
		currentProfilePath: currentSystemProfilePath(instanceName),
		desiredProfilePath: desiredSystemProfilePath(instanceName),
	}}
}

// InstanceName returns the snap instance name being updated.
func (up *SystemProfileUpdate) InstanceName() string {
	return up.commonProfileUpdate.instanceName
}

// Lock acquires locks / freezes needed to synchronize mount namespace changes.
func (up *SystemProfileUpdate) Lock() (func(), error) {
	instanceName := up.commonProfileUpdate.instanceName

	// Lock the mount namespace so that any concurrently attempted invocations
	// of snap-confine are synchronized and will see consistent state.
	lock, err := mount.OpenLock(instanceName)
	if err != nil {
		return nil, fmt.Errorf("cannot open lock file for mount namespace of snap %q: %s", instanceName, err)
	}

	logger.Debugf("locking mount namespace of snap %q", instanceName)
	if up.commonProfileUpdate.fromSnapConfine {
		// When --from-snap-confine is passed then we just ensure that the
		// namespace is locked. This is used by snap-confine to use
		// snap-update-ns to apply mount profiles.
		if err := lock.TryLock(); err != osutil.ErrAlreadyLocked {
			return nil, fmt.Errorf("mount namespace of snap %q is not locked but --from-snap-confine was used", instanceName)
		}
	} else {
		if err := lock.Lock(); err != nil {
			return nil, fmt.Errorf("cannot lock mount namespace of snap %q: %s", instanceName, err)
		}
	}

	// Freeze the mount namespace and unfreeze it later. This lets us perform
	// modifications without snap processes attempting to construct
	// symlinks or perform other malicious activity (such as attempting to
	// introduce a symlink that would cause us to mount something other
	// than what we expected).
	logger.Debugf("freezing processes of snap %q", instanceName)
	if err := freezeSnapProcesses(instanceName); err != nil {
		// If we cannot freeze the processes we should drop the lock.
		lock.Close()
		return nil, err
	}

	unlock := func() {
		logger.Debugf("unlocking mount namespace of snap %q", instanceName)
		lock.Close()
		logger.Debugf("thawing processes of snap %q", instanceName)
		thawSnapProcesses(instanceName)
	}
	return unlock, nil
}

// Assumptions returns information about file system mutability rules.
//
// System mount profiles can write to /tmp (this is required for constructing
// writable mimics) to /var/snap (where $SNAP_DATA is for services), /snap/$SNAP_NAME,
// and, in case of instances, /snap/$SNAP_INSTANCE_NAME.
func (up *SystemProfileUpdate) Assumptions() *Assumptions {
	// Allow creating directories related to this snap name.
	//
	// Note that we allow /var/snap instead of /var/snap/$SNAP_NAME because
	// content interface connections can readily create missing mount points on
	// both sides of the interface connection.
	//
	// We scope /snap/$SNAP_NAME because only one side of the connection can be
	// created, as snaps are read-only, the mimic construction will kick-in and
	// create the missing directory but this directory is only visible from the
	// snap that we are operating on (either plug or slot side, the point is,
	// the mount point is not universally visible).
	//
	// /snap/$SNAP_NAME needs to be there as the code that creates such mount
	// points must traverse writable host filesystem that contains /snap/*/ and
	// normally such access is off-limits. This approach allows /snap/foo
	// without allowing /snap/bin, for example.
	//
	// /snap/$SNAP_INSTANCE_NAME and /snap/$SNAP_NAME are added to allow
	// remapping for parallel installs only when the snap has an instance key
	as := &Assumptions{}
	instanceName := up.commonProfileUpdate.instanceName
	as.AddUnrestrictedPaths("/tmp", "/var/snap", "/snap/"+instanceName)
	if snapName := snap.InstanceSnap(instanceName); snapName != instanceName {
		as.AddUnrestrictedPaths("/snap/" + snapName)
	}
	return as
}

// desiredSystemProfilePath returns the path of the fstab-like file with the desired, system-wide mount profile for a snap.
func desiredSystemProfilePath(snapName string) string {
	return fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapMountPolicyDir, snapName)
}

// currentSystemProfilePath returns the path of the fstab-like file with the applied, system-wide mount profile for a snap.
func currentSystemProfilePath(snapName string) string {
	return fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapRunNsDir, snapName)
}
