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

	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

type CommonProfileUpdate struct {
	// instanceName is the name of the snap or instance to update.
	instanceName string

	// fromSnapConfine indicates that the update is triggered by snap-confine
	// and not from snapd. When set, snap-confine is still constructing the user
	// mount namespace and is delegating mount profile application to snap-update-ns.
	fromSnapConfine bool

	currentProfilePath string
	desiredProfilePath string
}

// InstanceName returns the snap instance name being updated.
func (up *CommonProfileUpdate) InstanceName() string {
	return up.instanceName
}

// Lock acquires locks / freezes needed to synchronize mount namespace changes.
func (up *CommonProfileUpdate) Lock() (func(), error) {
	instanceName := up.instanceName

	// Lock the mount namespace so that any concurrently attempted invocations
	// of snap-confine are synchronized and will see consistent state.
	lock, err := mount.OpenLock(instanceName)
	if err != nil {
		return nil, fmt.Errorf("cannot open lock file for mount namespace of snap %q: %s", instanceName, err)
	}

	logger.Debugf("locking mount namespace of snap %q", instanceName)
	if up.fromSnapConfine {
		// When --from-snap-confine is passed then we just ensure that the
		// namespace is locked. This is used by snap-confine to use
		// snap-update-ns to apply mount profiles.
		if err := lock.TryLock(); err != osutil.ErrAlreadyLocked {
			// If we managed to grab the lock we should drop it.
			lock.Close()
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

// NeededChanges computes the sequence of mount changes needed to transform current profile to desired profile.
func (up *CommonProfileUpdate) NeededChanges(current, desired *osutil.MountProfile) []*Change {
	return NeededChanges(current, desired)
}

// PerformChange performs a given mount namespace change under given filesystem assumptions.
func (up *CommonProfileUpdate) PerformChange(change *Change, as *Assumptions) ([]*Change, error) {
	return changePerform(change, as)
}

// LoadDesiredProfile loads the desired mount profile.
func (up *CommonProfileUpdate) LoadDesiredProfile() (*osutil.MountProfile, error) {
	profile, err := osutil.LoadMountProfile(up.desiredProfilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot load desired mount profile of snap %q: %s", up.instanceName, err)
	}
	return profile, nil
}

// LoadCurrentProfile loads the current mount profile.
func (up *CommonProfileUpdate) LoadCurrentProfile() (*osutil.MountProfile, error) {
	profile, err := osutil.LoadMountProfile(up.currentProfilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot load current mount profile of snap %q: %s", up.instanceName, err)
	}
	return profile, nil
}

// SaveCurrentProfile saves the current mount profile.
func (up *CommonProfileUpdate) SaveCurrentProfile(profile *osutil.MountProfile) error {
	if err := profile.Save(up.currentProfilePath); err != nil {
		return fmt.Errorf("cannot save current mount profile of snap %q: %s", up.instanceName, err)
	}
	return nil
}
