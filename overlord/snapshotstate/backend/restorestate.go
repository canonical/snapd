// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/randutil"
)

// RestoreState stores information that can be used to cleanly revert (or finish
// cleaning up) a snapshot Restore.
//
// This is useful when a Restore is part of a chain of operations, and a later
// one failing necessitates undoing the Restore.
type RestoreState struct {
	Done    bool     `json:"done,omitempty"`
	Snap    string   `json:"snap,omitempty"`
	Created []string `json:"created,omitempty"`
	Moved   []string `json:"moved,omitempty"`
	// Config is here for convenience; this package doesn't touch it
	Config map[string]any `json:"config,omitempty"`
}

// Cleanup the backed up data from disk.
func (rs *RestoreState) Cleanup() {
	if rs.Done {
		logger.Noticef("Internal error: attempting to clean up a snapshot.RestoreState twice.")
		return
	}
	rs.Done = true
	for _, dir := range rs.Moved {
		if err := os.RemoveAll(dir); err != nil {
			logger.Noticef("Cannot remove directory tree rooted at %q: %v.", dir, err)
		}
	}
}

func restoreStateFilename(fn string) string {
	return fmt.Sprintf("%s.~%s~", fn, randutil.RandomString(9))
}

var restoreStateRx = regexp.MustCompile(`\.~[a-zA-Z0-9]{9}~$`)

func restoreState2orig(fn string) string {
	if idx := restoreStateRx.FindStringIndex(fn); len(idx) > 0 {
		return fn[:idx[0]]
	}
	return ""
}

// Revert the backed up data: remove what was added, move back what was moved aside.
func (rs *RestoreState) Revert() {
	if rs.Done {
		logger.Noticef("Internal error: attempting to revert a snapshot.RestoreState twice.")
		return
	}
	rs.Done = true
	for _, dir := range rs.Created {
		logger.Debugf("Removing %q.", dir)
		// Handle mounts under dir before removing it.
		// * snapctl mounts are stopped and restarted after Revert returns
		//   (i.e., once the Moved loop has put old data back in place).
		// * rs.Snap may be empty for a RestoreState persisted by an older
		//   snapd, in that case skip mount handling.
		if rs.Snap != "" {
			snapctlMPs, nonSnapctlMPs, mountErr := listMountsAtOrUnder(rs.Snap, dir)
			if mountErr != nil {
				logger.Noticef("cannot list mounts for snap %q under %q: %v", rs.Snap, dir, mountErr)
			} else if len(nonSnapctlMPs) > 0 {
				// Non-snapctl mounts are present; RemoveAll will fail on them
				// anyway, so snapctl mounts are not stopped.
				logger.Noticef("cannot remove data with unknown mount(s) under %q: %s",
					dir, strings.Join(nonSnapctlMPs, ", "))
			} else {
				stoppedUnits, stopErr := stopMountUnits(snapctlMPs)
				if stopErr != nil {
					logger.Noticef("cannot stop mount unit(s) for snap %q under %q: %v", rs.Snap, dir, stopErr)
				}
				// Pass dir as a parameter to avoid capturing the loop
				// variable by reference (Go < 1.22 reuses it each iteration).
				defer func(units []string, d string) {
					if startErr := startMountUnits(units); startErr != nil {
						logger.Noticef("cannot restart mount unit(s) for snap %q under %q: %v", rs.Snap, d, startErr)
					}
				}(stoppedUnits, dir)
			}
		}
		if err := os.RemoveAll(dir); err != nil {
			logger.Noticef("While undoing changes because of a previous error: cannot remove %q: %v.", dir, err)
		}
	}
	for _, dir := range rs.Moved {
		orig := restoreState2orig(dir)
		if orig == "" {
			// dir is not restore state?!?
			logger.Debugf("Skipping restore of %q: unrecognised filename.", dir)
			continue
		}
		logger.Debugf("Restoring %q to %q.", dir, orig)
		if err := os.Rename(dir, orig); err != nil {
			logger.Noticef("While undoing changes because of a previous error: cannot restore %q to %q: %v.", dir, orig, err)
		}
	}
}
