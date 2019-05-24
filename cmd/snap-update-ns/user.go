// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2019 Canonical Ltd
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

// UserProfileUpdateContext contains information about update to per-user mount namespace.
type UserProfileUpdateContext struct {
	CommonProfileUpdateContext
	// uid is the numeric user identifier associated with the user for which
	// the update operation is occurring. It may be the current UID but doesn't
	// need to be.
	uid int
}

// NewUserProfileUpdateContext returns encapsulated information for performing a per-user mount namespace update.
func NewUserProfileUpdateContext(instanceName string, fromSnapConfine bool, uid int) *UserProfileUpdateContext {
	return &UserProfileUpdateContext{
		CommonProfileUpdateContext: CommonProfileUpdateContext{
			instanceName:       instanceName,
			fromSnapConfine:    fromSnapConfine,
			currentProfilePath: currentUserProfilePath(instanceName, uid),
			desiredProfilePath: desiredUserProfilePath(instanceName),
		},
		uid: uid,
	}
}

// UID returns the user ID of the mount namespace being updated.
func (upCtx *UserProfileUpdateContext) UID() int {
	return upCtx.uid
}

// Lock acquires locks / freezes needed to synchronize mount namespace changes.
func (upCtx *UserProfileUpdateContext) Lock() (unlock func(), err error) {
	// If persistent user mount namespaces are not enabled then don't acquire
	// any locks. This is for parity with the pre-persistence behavior (to
	// minimise impact before the feature is enabled by default).
	if features.PerUserMountNamespace.IsEnabled() {
		return upCtx.CommonProfileUpdateContext.Lock()
	}
	return func() {}, nil
}

// Assumptions returns information about file system mutability rules.
func (upCtx *UserProfileUpdateContext) Assumptions() *Assumptions {
	// TODO: configure the secure helper and inform it about directories that
	// can be created without trespassing.
	as := &Assumptions{}
	// TODO: When SNAP_USER_DATA and SNAP_USER_COMMON can be used from per-user
	// mount profiles then we need to handle /home/*/snap/*
	//
	// Right now this is not done because we must securely figure out what the
	// $HOME directory is and this must be preemptively allowed by apparmor
	// profile for snap-update-ns (that is per snap but not per user).  In
	// effect this feels like we must grant /home/*/snap/$SNAP_NAME/ anyway.
	// Note that currently using wild-cards in the Assumptions type is not
	// supported.
	as.AddUnrestrictedPaths("/tmp", xdgRuntimeDir(upCtx.uid))
	return as
}

// LoadDesiredProfile loads the desired, per-user mount profile, expanding user-specific variables.
func (upCtx *UserProfileUpdateContext) LoadDesiredProfile() (*osutil.MountProfile, error) {
	profile, err := upCtx.CommonProfileUpdateContext.LoadDesiredProfile()
	if err != nil {
		return nil, err
	}
	// TODO: when SNAP_USER_DATA, SNAP_USER_COMMON or other variables relating
	// to the user name and their home directory need to be expanded then
	// handle them here.
	expandXdgRuntimeDir(profile, upCtx.uid)
	return profile, nil
}

// SaveCurrentProfile does nothing at all.
//
// The profile is really only saved to disk if PerUserMountNamespace feature is
// enabled. This is matched by similar logic in snap-confine, that only
// persists per-user mount namespace if the same feature is enabled.
func (upCtx *UserProfileUpdateContext) SaveCurrentProfile(profile *osutil.MountProfile) error {
	// If persistent user mount namespaces are not enabled then don't
	// write the updated current mount profile because snap-confine
	// is similarly not storing the mount namespace.
	if features.PerUserMountNamespace.IsEnabled() {
		return upCtx.CommonProfileUpdateContext.SaveCurrentProfile(profile)
	}
	return nil
}

// LoadCurrentProfile returns the empty profile.
//
// The profile is really only loaded from disk if PerUserMountNamespace feature
// is enabled. This is matched by similar logic in snap-confine, that only
// persists per-user mount namespace if the same feature is enabled.
func (upCtx *UserProfileUpdateContext) LoadCurrentProfile() (*osutil.MountProfile, error) {
	if features.PerUserMountNamespace.IsEnabled() {
		return upCtx.CommonProfileUpdateContext.LoadCurrentProfile()
	}
	return &osutil.MountProfile{}, nil
}

// desiredUserProfilePath returns the path of the fstab-like file with the desired, user-specific mount profile for a snap.
func desiredUserProfilePath(snapName string) string {
	return fmt.Sprintf("%s/snap.%s.user-fstab", dirs.SnapMountPolicyDir, snapName)
}

// currentUserProfilePath returns the path of the fstab-like file with the applied, user-specific mount profile for a snap.
func currentUserProfilePath(snapName string, uid int) string {
	return fmt.Sprintf("%s/snap.%s.%d.user-fstab", dirs.SnapRunNsDir, snapName, uid)
}
