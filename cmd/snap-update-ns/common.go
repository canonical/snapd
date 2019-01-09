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

	"github.com/snapcore/snapd/osutil"
)

type commonProfileUpdate struct {
	// instanceName is the name of the snap or instance to update.
	instanceName string

	// fromSnapConfine indicates that the update is triggered by snap-confine
	// and not from snapd. When set, snap-confine is still constructing the user
	// mount namespace and is delegating mount profile application to snap-update-ns.
	fromSnapConfine bool

	currentProfilePath string
	desiredProfilePath string
}

// NeededChanges computes the sequence of mount changes needed to transform current profile to desired profile.
func (up *commonProfileUpdate) NeededChanges(current, desired *osutil.MountProfile) []*Change {
	return NeededChanges(current, desired)
}

// PerformChange performs a given mount namespace change under given filesystem assumptions.
func (up *commonProfileUpdate) PerformChange(change *Change, as *Assumptions) ([]*Change, error) {
	return changePerform(change, as)
}

// LoadDesiredProfile loads the desired, per-user mount profile, expanding user-specific variables.
func (up *commonProfileUpdate) LoadDesiredProfile() (*osutil.MountProfile, error) {
	profile, err := osutil.LoadMountProfile(up.desiredProfilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot load desired mount profile of snap %q: %s", up.instanceName, err)
	}
	return profile, nil
}

// LoadCurrentProfile loads the current, per-user mount profile.
func (up *commonProfileUpdate) LoadCurrentProfile() (*osutil.MountProfile, error) {
	profile, err := osutil.LoadMountProfile(up.currentProfilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot load current mount profile of snap %q: %s", up.instanceName, err)
	}
	return profile, nil
}

// SaveCurrentProfile saves the current, per-user mount profile, if matching feature is enabled.
//
// The profile is really only saved to disk if PerUserMountNamespace feature is
// enabled. This is matched by similar logic in snap-confine, that only
// persists per-user mount namespace if the same feature is enabled.
func (up *commonProfileUpdate) SaveCurrentProfile(profile *osutil.MountProfile) error {
	if err := profile.Save(up.currentProfilePath); err != nil {
		return fmt.Errorf("cannot save current mount profile of snap %q: %s", up.instanceName, err)
	}
	return nil
}
