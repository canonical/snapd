// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

	"github.com/snapcore/snapd/cmd/snaplock"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sandbox/cgroup"
)

type CommonProfileUpdateContext struct {
	// instanceName is the name of the snap instance to update.
	instanceName string

	// fromSnapConfine indicates that the update is triggered by snap-confine
	// and not from snapd. When set, snap-confine is still constructing the user
	// mount namespace and is delegating mount profile application to snap-update-ns.
	fromSnapConfine bool

	currentProfilePath string
	desiredProfilePath string
}

// InstanceName returns the snap instance name being updated.
func (upCtx *CommonProfileUpdateContext) InstanceName() string {
	return upCtx.instanceName
}

// Lock acquires locks / freezes needed to synchronize mount namespace changes.
func (upCtx *CommonProfileUpdateContext) Lock() (func(), error) {
	instanceName := upCtx.instanceName

	// Lock the mount namespace so that any concurrently attempted invocations
	// of snap-confine are synchronized and will see consistent state.
	lock, err := snaplock.OpenLock(instanceName)
	if err != nil {
		return nil, fmt.Errorf("cannot open lock file for mount namespace of snap %q: %s", instanceName, err)
	}

	logger.Debugf("locking mount namespace of snap %q", instanceName)
	if upCtx.fromSnapConfine {
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
	if err := cgroup.FreezeSnapProcesses(instanceName); err != nil {
		// If we cannot freeze the processes we should drop the lock.
		lock.Close()
		return nil, err
	}

	unlock := func() {
		logger.Debugf("unlocking mount namespace of snap %q", instanceName)
		lock.Close()
		logger.Debugf("thawing processes of snap %q", instanceName)
		cgroup.ThawSnapProcesses(instanceName)
	}
	return unlock, nil
}

func (upCtx *CommonProfileUpdateContext) Assumptions() *Assumptions {
	return nil
}

// LoadDesiredProfile loads the desired mount profile.
func (upCtx *CommonProfileUpdateContext) LoadDesiredProfile() (*osutil.MountProfile, error) {
	profile, err := osutil.LoadMountProfile(upCtx.desiredProfilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot load desired mount profile of snap %q: %s", upCtx.instanceName, err)
	}
	return profile, nil
}

// LoadCurrentProfile loads the current mount profile.
func (upCtx *CommonProfileUpdateContext) LoadCurrentProfile() (*osutil.MountProfile, error) {
	profile, err := osutil.LoadMountProfile(upCtx.currentProfilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot load current mount profile of snap %q: %s", upCtx.instanceName, err)
	}
	return profile, nil
}

// SaveCurrentProfile saves the current mount profile.
func (upCtx *CommonProfileUpdateContext) SaveCurrentProfile(profile *osutil.MountProfile) error {
	if err := profile.Save(upCtx.currentProfilePath); err != nil {
		return fmt.Errorf("cannot save current mount profile of snap %q: %s", upCtx.instanceName, err)
	}
	return nil
}
