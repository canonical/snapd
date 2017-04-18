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

package mount

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// Action represents a mount action (mount, remount, unmount, etc).
type Action string

const (
	// Mount represents an action that results in mounting something somewhere.
	Mount Action = "mount"
	// Unmount represents an action that results in unmounting something from somewhere.
	Unmount Action = "umount"
	// Remount when needed
)

// Change describes a change to the mount table (action and the entry to act on).
type Change struct {
	Entry  Entry
	Action Action
}

// String formats mount change to a human-readable line.
func (c Change) String() string {
	return fmt.Sprintf("%s (%s)", c.Action, c.Entry)
}

// Needed returns true if the change needs to be performed in the context of mount table.
func (c Change) Needed(mounted []*InfoEntry) bool {
	// Look through what is mounted and see if we shold perform the change. If
	// the entry is already mounted then we don't need to mount it, if the
	// entry is already unmounted then we don't need to unmount it.

	// TODO: implement this
	return true
}

func (c Change) Perform() error {
	// TODO merge https://github.com/snapcore/snapd/pull/3138
	return nil
}

// NeededChanges computes the changes required to change current to desired mount entries.
//
// The current and desired profiles is a fstab like list of mount entries. The
// lists are processed and a "diff" of mount changes is produced. The mount
// changes, when applied in order, transform the current profile into the
// desired profile.
func NeededChanges(currentProfile, desiredProfile []Entry) []Change {
	// Copy both as we will want to mutate them.
	current := make([]Entry, len(currentProfile))
	copy(current, currentProfile)
	desired := make([]Entry, len(desiredProfile))
	copy(desired, desiredProfile)

	// Clean the directory part of both profiles. This is done so that we can
	// easily test if a given directory is a subdirectory with
	// strings.HasPrefix coupled with an extra slash character.
	for i := range current {
		current[i].Dir = path.Clean(current[i].Dir)
	}
	for i := range desired {
		desired[i].Dir = path.Clean(desired[i].Dir)
	}

	// Sort both lists by directory name with implicit trailing slash.
	sort.Sort(byMagicDir(current))
	sort.Sort(byMagicDir(desired))

	// Construct a desired directory map.
	desiredMap := make(map[string]*Entry)
	for i := range desired {
		desiredMap[desired[i].Dir] = &desired[i]
	}

	// Compute reusable entries: those which are equal in current and desired and which
	// are not prefixed by another entry that changed.
	var reuse map[string]bool
	var skipDir string
	for i := range current {
		dir := current[i].Dir
		if skipDir != "" && strings.HasPrefix(dir, skipDir) {
			continue
		}
		skipDir = "" // reset skip prefix as it no longer applies
		if entry, ok := desiredMap[dir]; ok && current[i].Equal(entry) {
			if reuse == nil {
				reuse = make(map[string]bool)
			}
			reuse[dir] = true
			continue
		}
		skipDir = strings.TrimSuffix(dir, "/") + "/"
	}

	// We are now ready to compute the necessary mount changes.
	var changes []Change

	// Unmount entries not reused in reverse to handle children before their parent.
	for i := len(current) - 1; i >= 0; i-- {
		if !reuse[current[i].Dir] {
			changes = append(changes, Change{Action: Unmount, Entry: current[i]})
		}
	}

	// Mount desired entries not reused.
	for i := range desired {
		if !reuse[desired[i].Dir] {
			changes = append(changes, Change{Action: Mount, Entry: desired[i]})
		}
	}

	return changes
}
