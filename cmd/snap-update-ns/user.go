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
	// TODO: when persistent user mount namespaces are enabled, grab a lock
	// protecting the snap and freeze snap processes here.
	return func() {}, nil
}

// Assumptions returns information about file system mutability rules.
func (upCtx *UserProfileUpdateContext) Assumptions() *Assumptions {
	// TODO: configure the secure helper and inform it about directories that
	// can be created without trespassing.
	as := &Assumptions{}
	// TODO: Handle /home/*/snap/* when we do per-user mount namespaces and
	// allow defining layout items that refer to SNAP_USER_DATA and
	// SNAP_USER_COMMON.
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
// Per-user mount profiles are not persisted yet.
func (upCtx *UserProfileUpdateContext) SaveCurrentProfile(profile *osutil.MountProfile) error {
	// TODO: when persistent user mount namespaces are enabled save the
	// current, per-user mount profile here.
	return nil
}

// LoadCurrentProfile returns the empty profile.
//
// Per-user mount profiles are not persisted yet.
func (upCtx *UserProfileUpdateContext) LoadCurrentProfile() (*osutil.MountProfile, error) {
	// TODO: when persistent user mount namespaces are enabled load the
	// current, per-user mount profile here.
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
