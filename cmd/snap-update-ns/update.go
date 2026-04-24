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
	"errors"

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

func executeMountProfileUpdate(upCtx MountProfileUpdateContext) error {
	unlock, err := upCtx.Lock()
	if err != nil {
		return err
	}
	defer unlock()

	desired, err := upCtx.LoadDesiredProfile()
	if err != nil {
		return err
	}

	currentBefore, err := upCtx.LoadCurrentProfile()
	if err != nil {
		return err
	}

	// Synthesize mount changes that were applied before for the purpose of the tmpfs detector.
	as := upCtx.Assumptions()
	for _, entry := range currentBefore.Entries {
		as.AddChange(&Change{Action: Mount, Entry: entry})
	}

	// Compute the needed changes and perform each change if
	// needed, collecting those that we managed to perform or that
	// were performed already.
	changesNeeded := NeededChanges(currentBefore, desired)
	// TODO: NeededChanges could return changes grouped by origin (overname,
	// non-layout, layout) instead of a flat list, removing the need to
	// filter by origin in each pass.

	var changesMade []*Change
	changeErr := make([]error, len(changesNeeded))

	var errContinue = errors.New("continue")

	applyOnly := func(
		pred func(c *Change) bool,
		f func(idx int, c *Change) (changesMade []*Change, err error),
	) error {
		for i, change := range changesNeeded {
			if !pred(change) {
				continue
			}

			logger.Debugf("apply: %v", change)
			actualChangesMade, err := f(i, change)
			if err != nil {
				if err == errContinue {
					continue
				}
				return err
			}
			changesMade = append(changesMade, actualChangesMade...)
		}
		return nil
	}

	// Apply all the changes in separate passes in the following order:
	// 1. Unmounts/keeps
	//    Things are either going away or we keep them, establish a new world order before doing
	//    anything else
	// 2. Dependencies:
	// 2.1. overname (parallel installs)
	//    Mock $SNAP_INSTANCE_NAME -> $SNAP mount, must come before everything else
	// 2.2. non-layout dependencies
	//    Everything that is not a layout mount, including content interface mounts
	//    and host filesystem mounts. Content needs to be brought in early as
	//    layouts may distribute it inside the world view.
	// 3. layouts
	//    Everything else the snap ever wanted
	//
	// Passes 2.2 and 3. are split into separate sub-passes that do prepare &
	// apply independently.

	logger.Debugf("1. pass keep/unmount")
	// In the first pass we fully apply keep and unmount changes
	err = applyOnly(
		func(c *Change) bool { return c.Action != Mount },
		func(i int, change *Change) ([]*Change, error) {
			// Non-mount changes (so unmount or keep) do nothing in
			// PrepareToPerform so we can safely call DoPerform which does not
			// return any synthetic changes.
			if err := change.DoPerform(as); err != nil {
				changeErr[i] = err
				logger.Noticef("cannot change mount namespace according to change %s: %s", change, err)
				return nil, errContinue
			}
			return []*Change{change}, nil
		})
	if err != nil {
		return err
	}

	// In the second pass we prepare to perform all mount changes
	logger.Debugf("2. pass prep")
	logger.Debugf("2.1. pass prep+apply (overname)")
	// Keep the invariant that overname (parallel installs mocking) needs to be applied first.
	err = applyOnly(
		func(c *Change) bool {
			return c.Action == Mount && c.Entry.XSnapdOrigin() == "overname"
		},
		func(i int, change *Change) ([]*Change, error) {
			var synthesized []*Change
			synthesized, changeErr[i] = change.PrepareToPerform(as)
			if changeErr[i] == nil {
				changeErr[i] = change.DoPerform(as)
			}
			if err := changeErr[i]; err != nil {
				return synthesized, err
			}
			return append(synthesized, change), nil
		})
	if err != nil {
		return err
	}

	logger.Debugf("2.2.1 pass prep (non-layout)")
	err = applyOnly(
		func(c *Change) bool {
			// Non-layout entries have no explicit origin set, this includes
			// content interface mounts and host filesystem mounts.
			return c.Action == Mount && c.Entry.XSnapdOrigin() == ""
		},
		func(i int, change *Change) ([]*Change, error) {
			var synthesized []*Change
			synthesized, changeErr[i] = change.PrepareToPerform(as)
			// We may have done something even if Perform itself has failed.
			// We need to collect synthesized changes and store them.
			return synthesized, nil
		})
	if err != nil {
		return err
	}

	logger.Debugf("2.2.2 pass apply (non-layout)")
	err = applyOnly(
		func(c *Change) bool {
			return c.Action == Mount && c.Entry.XSnapdOrigin() == ""
		},
		func(i int, change *Change) ([]*Change, error) {
			if changeErr[i] == nil {
				// Only perform the change if preparation has not failed.
				changeErr[i] = change.DoPerform(as)
			}
			if err := changeErr[i]; err != nil {
				if err != ErrIgnoredMissingMount {
					logger.Noticef("cannot change mount namespace according to change %s: %s", change, err)
				}
				return nil, errContinue
			}
			return []*Change{change}, nil
		})
	if err != nil {
		return err
	}

	// In the third and final pass, we perform all the mount changes related to layouts
	logger.Debugf("3. pass mount")
	logger.Debugf("3.1 pass prep (layout)")
	err = applyOnly(
		func(c *Change) bool {
			return c.Action == Mount && c.Entry.XSnapdOrigin() == "layout"
		},
		func(i int, change *Change) ([]*Change, error) {
			var synthesized []*Change
			synthesized, changeErr[i] = change.PrepareToPerform(as)
			// We may have done something even if Perform itself has failed.
			// We need to collect synthesized changes and store them.
			return synthesized, nil
		})
	if err != nil {
		return err
	}

	logger.Debugf("3.2 pass apply (layout)")
	err = applyOnly(
		func(c *Change) bool {
			return c.Action == Mount && c.Entry.XSnapdOrigin() == "layout"
		},
		func(i int, change *Change) ([]*Change, error) {
			if changeErr[i] == nil {
				// Only perform the change if preparation has not failed.
				changeErr[i] = change.DoPerform(as)
			}
			if changeErr[i] == nil {
				return []*Change{change}, nil
			}
			return nil, changeErr[i]
		})
	if err != nil {
		return err
	}

	// Compute the new current profile so that it contains only changes that were made
	// and save it back for next runs.
	currentAfter := CurrentProfileFromChangesMade(changesMade)
	return upCtx.SaveCurrentProfile(&currentAfter)
}

// CurrentProfileFromChangesMade computes a new mount profile a slice of changes.
//
// The return value collects mount profile entries from changes of type "mount"
// and "keep" while discarding the changes of type "unmount". The order in
// which entries are collected depends on their type.
//
// The sequence of changes, as produced by NeededChanges, is computed by
// looking at two mount profiles, examining the most recent change and working
// backwards. Since the most recent change is the last entry in the mount
// profile, the first change describes the last mount entry of the old mount
// profile.
//
// The "keep" changes are thus collected in reverse order - the order of their
// true appearance, while "mount" changes are collected in the order of their
// appearance as this represents the actual order of performed mount
// operations.
func CurrentProfileFromChangesMade(changes []*Change) osutil.MountProfile {
	var profile osutil.MountProfile

	for i := len(changes) - 1; i >= 0; i-- {
		change := changes[i]
		if change.Action == Keep {
			profile.Entries = append(profile.Entries, change.Entry)
		}
	}
	for _, change := range changes {
		if change.Action == Mount {
			profile.Entries = append(profile.Entries, change.Entry)
		}
	}

	return profile
}
