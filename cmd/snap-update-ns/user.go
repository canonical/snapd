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
			desiredProfilePath: desiredUserProfilePath(instanceName),
		},
		uid: uid,
	}
}

// Lock acquires locks / freezes needed to synchronize mount namespace changes.
func (ctx *UserProfileUpdateContext) Lock() (unlock func(), err error) {
	// TODO: when persistent user mount namespaces are enabled, grab a lock
	// protecting the snap and freeze snap processes here.
	return func() {}, nil
}

// Assumptions returns information about file system mutability rules.
func (ctx *UserProfileUpdateContext) Assumptions() *Assumptions {
	// TODO: configure the secure helper and inform it about directories that
	// can be created without trespassing.
	as := &Assumptions{}
	// TODO: Handle /home/*/snap/* when we do per-user mount namespaces and
	// allow defining layout items that refer to SNAP_USER_DATA and
	// SNAP_USER_COMMON.
	return as
}

// LoadDesiredProfile loads the desired, per-user mount profile, expanding user-specific variables.
func (ctx *UserProfileUpdateContext) LoadDesiredProfile() (*osutil.MountProfile, error) {
	profile, err := ctx.CommonProfileUpdateContext.LoadDesiredProfile()
	if err != nil {
		return nil, err
	}
	// TODO: when SNAP_USER_DATA, SNAP_USER_COMMON or other variables relating
	// to the user name and their home directory need to be expanded then
	// handle them here.
	expandXdgRuntimeDir(profile, ctx.uid)
	return profile, nil
}

func applyUserFstab(ctx MountProfileUpdateContext) error {
	desired, err := ctx.LoadDesiredProfile()
	if err != nil {
		return err
	}
	debugShowProfile(desired, "desired mount profile")
	as := ctx.Assumptions()
	_, err = applyProfile(ctx, &osutil.MountProfile{}, desired, as)
	return err
}

// desiredUserProfilePath returns the path of the fstab-like file with the desired, user-specific mount profile for a snap.
func desiredUserProfilePath(snapName string) string {
	return fmt.Sprintf("%s/snap.%s.user-fstab", dirs.SnapMountPolicyDir, snapName)
}
