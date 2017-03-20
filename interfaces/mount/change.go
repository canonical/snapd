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
	"path"
	"sort"
	"strings"
)

// Action represents a mount action (mount, remount, unmount, etc).
type Action string

const (
	Mount   Action = "mount"
	Unmount Action = "umount"
	// Remount when needed
)

// Change describes a change to the mount table (action and the entry to act on).
type Change struct {
	Entry  Entry
	Action Action
}

// NeededChanges computes the changes required to change current to desired mount entries.
//
// The current and desired profiles is a fstab like list of mount entries. The
// lists are processed and a "diff" of mount changes is produced. The mount
// changes, when applied in order, transform the current profile into the
// desired profile.
func NeededChanges(currentProfile, desiredProfile []Entry) []Change {
	var changes []Change
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

	// Sort both lists by directory name.
	sort.Sort(byDir(current))
	sort.Sort(byDir(desired))

	// Construct a desired directory map.
	// Maps from a directory to a pointer to an Entry from the desired list.
	desiredMap := make(map[string]*Entry)
	for i := range desired {
		desiredMap[desired[i].Dir] = &desired[i]
	}

	// Reuse map, indexed by Entry.Dir.
	// All reused entries will not be unmounted or mounted. Reused entries must
	// naturally exist in the desired and current maps or no reuse is possible.
	reuse := make(map[string]bool)

	// See if there are any directories that we can reuse. Go over all the
	// entries in the current entries and if there's an identical desired entry
	// then mark this directory / entry for reuse.
	//
	// Don't reuse any children if their parent changes.
	var skipPrefix string
	for i := range current {
		dir := current[i].Dir
		if skipPrefix != "" && strings.HasPrefix(dir, skipPrefix) && dir[len(skipPrefix)] == '/' {
			continue
		}
		if entry, ok := desiredMap[dir]; ok && EqualEntries(&current[i], entry) {
			reuse[dir] = true
			continue
		}
		skipPrefix = dir
	}

	// Unmount all the current entries (unless flagged for reuse).
	// Because c is sorted by directory name we can iterate in reverse
	// to ensure we unmount children before we try to unmount parents.
	for i := len(current) - 1; i >= 0; i-- {
		if !reuse[current[i].Dir] {
			changes = append(changes, Change{Action: Unmount, Entry: current[i]})
		}
	}

	// Mount all the desired entries (unless flagged for reuse).
	for i := range desired {
		if !reuse[desired[i].Dir] {
			changes = append(changes, Change{Action: Mount, Entry: desired[i]})
		}
	}

	return changes
}
