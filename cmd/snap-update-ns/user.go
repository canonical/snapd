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
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/osutil"
)

// UserProfileUpdate contains information about update to per-user mount namespace.
type UserProfileUpdate struct {
	commonProfileUpdate
	// uid is the numeric user identifier associated with the user for which
	// the update operation is occurring. It may be the current UID but doesn't
	// need to be.
	uid int
}

// NewUserProfileUpdate returns encapsulated information for performing a per-user mount namespace update.
func NewUserProfileUpdate(instanceName string, fromSnapConfine bool, uid int) *UserProfileUpdate {
	return &UserProfileUpdate{
		commonProfileUpdate: commonProfileUpdate{
			instanceName:       instanceName,
			fromSnapConfine:    fromSnapConfine,
			currentProfilePath: currentUserProfilePath(instanceName, uid),
			desiredProfilePath: desiredUserProfilePath(instanceName),
		},
		uid: uid,
	}
}

// InstanceName returns the snap instance name being updated.
func (up *UserProfileUpdate) InstanceName() string {
	return up.commonProfileUpdate.instanceName
}

// UID returns the user ID of the mount namespace being updated.
func (up *UserProfileUpdate) UID() int {
	return up.uid
}

// Lock acquires locks / freezes needed to synchronize mount namespace changes.
func (up *UserProfileUpdate) Lock() (unlock func(), err error) {
	// TODO: grab per-snap lock, freeze all processes.
	// This is hard to do when not running as root.
	return func() {}, nil
}

// Assumptions returns information about file system mutability rules.
//
// User mount profiles can write to /tmp (this is required for constructing
// writable mimics) and to /run/user/UID/
func (up *UserProfileUpdate) Assumptions() *Assumptions {
	// TODO: When SNAP_USER_DATA and SNAP_USER_COMMON can be used from per-user
	// mount profiles then we need to handle /home/*/snap/*
	//
	// Right now this is not done because we must securely figure out what the
	// $HOME directory is and this must be preemptively allowed by apparmor
	// profile for snap-update-ns (that is per snap but not per user).  In
	// effect this feels like we must grant /home/*/snap/$SNAP_NAME/ anyway.
	// Note that currently using wild-cards in the Assumptions type is not
	// supported.
	as := &Assumptions{}
	as.AddUnrestrictedPaths("/tmp", xdgRuntimeDir(up.uid))
	return as
}

// LoadDesiredProfile loads the desired, per-user mount profile, expanding user-specific variables.
func (up *UserProfileUpdate) LoadDesiredProfile() (*osutil.MountProfile, error) {
	profile, err := up.commonProfileUpdate.LoadDesiredProfile()
	if err != nil {
		return nil, err
	}
	// TODO: when SNAP_USER_DATA, SNAP_USER_COMMON or other variables relating
	// to the user name and their home directory need to be expanded then
	// handle them here.
	expandXdgRuntimeDir(profile, up.uid)
	return profile, nil
}

// SaveCurrentProfile saves the current, per-user mount profile, if matching feature is enabled.
//
// The profile is really only saved to disk if PerUserMountNamespace feature is
// enabled. This is matched by similar logic in snap-confine, that only
// persists per-user mount namespace if the same feature is enabled.
func (up *UserProfileUpdate) SaveCurrentProfile(profile *osutil.MountProfile) error {
	// If persistent user mount namespaces are not enabled then don't
	// write the updated current mount profile because snap-confine
	// is similarly not storing the mount namespace.
	if features.PerUserMountNamespace.IsEnabled() {
		return up.commonProfileUpdate.SaveCurrentProfile(profile)
	}
	return nil
}

// desiredUserProfilePath returns the path of the fstab-like file with the desired, user-specific mount profile for a snap.
func desiredUserProfilePath(snapName string) string {
	return fmt.Sprintf("%s/snap.%s.user-fstab", dirs.SnapMountPolicyDir, snapName)
}

// currentUserProfilePath returns the path of the fstab-like file with the applied, user-specific mount profile for a snap.
func currentUserProfilePath(snapName string, uid int) string {
	return fmt.Sprintf("%s/snap.%s.%d.user-fstab", dirs.SnapRunNsDir, snapName, uid)
}
