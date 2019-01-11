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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

type MountProfileUpdate interface {
	Lock() (unlock func(), err error)
	Assumptions() *Assumptions
	LoadDesiredProfile() (*osutil.MountProfile, error)
	LoadCurrentProfile() (*osutil.MountProfile, error)
	SaveCurrentProfile(*osutil.MountProfile) error
	NeededChanges(old, new *osutil.MountProfile) []*Change
	PerformChange(*Change, *Assumptions) ([]*Change, error)
}

func executeMountProfileUpdate(up MountProfileUpdate) error {
	unlock, err := up.Lock()
	if err != nil {
		return err
	}
	defer unlock()

	desired, err := up.LoadDesiredProfile()
	if err != nil {
		return err
	}
	debugShowProfile(desired, "desired mount profile")

	currentBefore, err := up.LoadCurrentProfile()
	if err != nil {
		return err
	}
	debugShowProfile(currentBefore, "current mount profile (before applying changes)")

	// Synthesize mount changes that were applied before for the purpose of the tmpfs detector.
	as := up.Assumptions()
	for _, entry := range currentBefore.Entries {
		as.AddChange(&Change{Action: Mount, Entry: entry})
	}

	// Compute the needed changes and perform each change if
	// needed, collecting those that we managed to perform or that
	// were performed already.
	changesNeeded := up.NeededChanges(currentBefore, desired)
	debugShowChanges(changesNeeded, "mount changes needed")

	logger.Debugf("performing mount changes:")
	var changesMade []*Change
	for _, change := range changesNeeded {
		logger.Debugf("\t * %s", change)
		synthesised, err := up.PerformChange(change, as)
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
				// TODO: convert the test to a method over origin.
				return err
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
	return up.SaveCurrentProfile(&currentAfter)
}
