// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"github.com/snapcore/snapd/cmd/snaplock"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

func (b Backend) RunInhibitSnapForUnlink(info *snap.Info, hint runinhibit.Hint, decision func() error) (lock *osutil.FileLock, err error) {
	// A process may be created after the soft refresh done upon
	// the request to refresh a snap. If such process is alive by
	// the time this code is reached the refresh process is stopped.

	// Grab per-snap lock to prevent new processes from starting. This is
	// sufficient to perform the check, even though individual processes
	// may fork or exit, we will have per-security-tag information about
	// what is running.
	lock, err = snaplock.OpenLock(info.InstanceName())
	if err != nil {
		return nil, err
	}
	// Keep a copy of lock, so that we can close it in the function below.
	// The regular lock variable is assigned to by return, due to the named
	// return values.
	lockToClose := lock
	defer func() {
		// If we have a lock but we are returning an error then unlock the lock
		// by closing it.
		if lockToClose != nil && err != nil {
			lockToClose.Close()
		}
	}()
	if err := lock.Lock(); err != nil {
		return nil, err
	}
	//
	if err := decision(); err != nil {
		return nil, err
	}
	// Decision function did not fail so we can, while we still hold the snap
	// lock, install the snap run inhibition hint, returning the snap lock to
	// the caller.
	//
	// XXX: should we move this logic to the place that calls the "soft"
	// check instead? Doing so would somewhat change the semantic of soft
	// and hard checks, as it would effectively make hard check a no-op,
	// but it might provide a nicer user experience.
	inhibitInfo := runinhibit.InhibitInfo{Previous: info.SnapRevision()}
	if err := runinhibit.LockWithHint(info.InstanceName(), hint, inhibitInfo); err != nil {
		return nil, err
	}
	return lock, nil
}

// WithSnapLock executes given action with the snap lock held.
//
// The lock is also used by snap-confine during pre-snap mount namespace
// initialization. Holding it allows to ensure mutual exclusion during the
// process of preparing a new snap app or hook processes. It does not prevent
// existing application or hook processes from forking.
//
// Note that this is not a method of the Backend type, so that it can be
// invoked from doInstall, which does not have access to a backend object.
func WithSnapLock(info *snap.Info, action func() error) error {
	lock, err := snaplock.OpenLock(info.InstanceName())
	if err != nil {
		return err
	}
	// Closing the lock also unlocks it, if locked.
	defer lock.Close()
	if err := lock.Lock(); err != nil {
		return err
	}
	return action()
}
