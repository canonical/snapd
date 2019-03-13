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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

// MountProfileUpdateContext provides the context of a mount namespace update.
// The context provides a way to synchronize the operation with other users of
// the snap system, to load and save the mount profiles and to provide the file
// system assumptions with which the mount namespace will be modified.
type MountProfileUpdateContext interface {
	// Lock obtains locks appropriate for the update.
	Lock() (unlock func(), err error)
	// Assumptions computes filesystem assumptions under which the update shall operate.
	Assumptions() *Assumptions
	// LoadDesiredProfile loads the mount profile that should be constructed.
	LoadDesiredProfile() (*osutil.MountProfile, error)
	// LoadCurrentProfile loads the mount profile that is currently applied.
	LoadCurrentProfile() (*osutil.MountProfile, error)
	// SaveCurrentProfile saves the mount profile that is currently applied.
	SaveCurrentProfile(*osutil.MountProfile) error
}

func applySystemFstab(ctx MountProfileUpdateContext) error {
	unlock, err := ctx.Lock()
	if err != nil {
		return err
	}
	defer unlock()

	// Read the desired and current mount profiles. Note that missing files
	// count as empty profiles so that we can gracefully handle a mount
	// interface connection/disconnection.
	desired, err := ctx.LoadDesiredProfile()
	if err != nil {
		return err
	}
	debugShowProfile(desired, "desired mount profile")

	currentBefore, err := ctx.LoadCurrentProfile()
	if err != nil {
		return err
	}
	debugShowProfile(currentBefore, "current mount profile (before applying changes)")
	// Synthesize mount changes that were applied before for the purpose of the tmpfs detector.
	as := ctx.Assumptions()
	for _, entry := range currentBefore.Entries {
		as.AddChange(&Change{Action: Mount, Entry: entry})
	}

	currentAfter, err := applyProfile(ctx, currentBefore, desired, as)
	if err != nil {
		return err
	}

	return ctx.SaveCurrentProfile(currentAfter)
}

func applyUserFstab(ctx MountProfileUpdateContext) error {
	desired, err := ctx.LoadDesiredProfile()
	if err != nil {
		return err
	}
	debugShowProfile(desired, "desired mount profile")

	current, err := ctx.LoadCurrentProfile()
	if err != nil {
		return err
	}
	debugShowProfile(current, "current mount profile")

	as := ctx.Assumptions()
	_, err = applyProfile(ctx, current, desired, as)
	return err
}

func applyProfile(ctx MountProfileUpdateContext, currentBefore, desired *osutil.MountProfile, as *Assumptions) (*osutil.MountProfile, error) {
	// Compute the needed changes and perform each change if
	// needed, collecting those that we managed to perform or that
	// were performed already.
	changesNeeded := NeededChanges(currentBefore, desired)
	debugShowChanges(changesNeeded, "mount changes needed")

	logger.Debugf("performing mount changes:")
	var changesMade []*Change
	for _, change := range changesNeeded {
		logger.Debugf("\t * %s", change)
		synthesised, err := change.Perform(as)
		changesMade = append(changesMade, synthesised...)
		if len(synthesised) > 0 {
			logger.Debugf("\tsynthesised additional mount changes:")
			for _, synth := range synthesised {
				logger.Debugf(" * \t\t%s", synth)
			}
		}
		if err != nil {
			// We may have done something even if Perform itself has
			// failed. We need to collect synthesized changes and
			// store them.
			origin := change.Entry.XSnapdOrigin()
			if origin == "layout" || origin == "overname" {
				return nil, err
			} else if err != ErrIgnoredMissingMount {
				logger.Noticef("cannot change mount namespace according to change %s: %s", change, err)
			}
			continue
		}

		changesMade = append(changesMade, change)
	}

	// Compute the new current profile so that it contains only changes that were made
	// and save it back for next runs.
	var currentAfter osutil.MountProfile
	for _, change := range changesMade {
		if change.Action == Mount || change.Action == Keep {
			currentAfter.Entries = append(currentAfter.Entries, change.Entry)
		}
	}
	debugShowProfile(&currentAfter, "current mount profile (after applying changes)")
	return &currentAfter, nil
}
