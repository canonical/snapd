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
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/logger"
)

// Action represents a mount action (mount, remount, unmount, etc).
type Action string

const (
	// Keep indicates that a given mount entry should be kept as-is.
	Keep Action = "keep"
	// Mount represents an action that results in mounting something somewhere.
	Mount Action = "mount"
	// Unmount represents an action that results in unmounting something from somewhere.
	Unmount Action = "unmount"
	// Remount when needed
)

// Change describes a change to the mount table (action and the entry to act on).
type Change struct {
	Entry  mount.Entry
	Action Action
}

// String formats mount change to a human-readable line.
func (c Change) String() string {
	return fmt.Sprintf("%s (%s)", c.Action, c.Entry)
}

// changePerform is Change.Perform that can be mocked for testing.
var changePerform = (*Change).Perform

// Perform executes the desired mount or unmount change using system calls.
// Filesystems that depend on helper programs or multiple independent calls to
// the kernel (--make-shared, for example) are unsupported.
//
// Perform may synthesize *additional* changes that were necessary to perform
// this change (such as mounted tmpfs or overlayfs).
func (c *Change) Perform() ([]*Change, error) {
	if c.Action == Mount {
		const (
			mode = 0755
			uid  = 0
			gid  = 0
		)
		// Create target mount directory if needed.
		if err := ensureMountPoint(c.Entry.Dir, mode, uid, gid); err != nil {
			return nil, err
		}
		// If this is a bind mount then create the source directory as well.
		// This allows snaps to share a subset of their data easily.
		flags, _ := mount.OptsToCommonFlags(c.Entry.Options)
		if flags&syscall.MS_BIND != 0 {
			if err := ensureMountPoint(c.Entry.Name, mode, uid, gid); err != nil {
				return nil, err
			}
		}
	}
	return nil, c.lowLevelPerform()
}

// lowLevelPerform is simple bridge from Change to mount / unmount syscall.
func (c *Change) lowLevelPerform() error {
	switch c.Action {
	case Mount:
		flags, unparsed := mount.OptsToCommonFlags(c.Entry.Options)
		err := sysMount(c.Entry.Name, c.Entry.Dir, c.Entry.Type, uintptr(flags), strings.Join(unparsed, ","))
		logger.Debugf("mount %q %q %q %d %q -> %s", c.Entry.Name, c.Entry.Dir, c.Entry.Type, uintptr(flags), strings.Join(unparsed, ","), err)
		return err
	case Unmount:
		err := sysUnmount(c.Entry.Dir, umountNoFollow)
		logger.Debugf("umount %q -> %v", c.Entry.Dir, err)
		return err
	case Keep:
		return nil
	}
	return fmt.Errorf("cannot process mount change, unknown action: %q", c.Action)
}

// NeededChanges computes the changes required to change current to desired mount entries.
//
// The current and desired profiles is a fstab like list of mount entries. The
// lists are processed and a "diff" of mount changes is produced. The mount
// changes, when applied in order, transform the current profile into the
// desired profile.
func NeededChanges(currentProfile, desiredProfile *mount.Profile) []*Change {
	// Copy both profiles as we will want to mutate them.
	current := make([]mount.Entry, len(currentProfile.Entries))
	copy(current, currentProfile.Entries)
	desired := make([]mount.Entry, len(desiredProfile.Entries))
	copy(desired, desiredProfile.Entries)

	// Clean the directory part of both profiles. This is done so that we can
	// easily test if a given directory is a subdirectory with
	// strings.HasPrefix coupled with an extra slash character.
	for i := range current {
		current[i].Dir = filepath.Clean(current[i].Dir)
	}
	for i := range desired {
		desired[i].Dir = filepath.Clean(desired[i].Dir)
	}

	// Sort both lists by directory name with implicit trailing slash.
	sort.Sort(byMagicDir(current))
	sort.Sort(byMagicDir(desired))

	// Construct a desired directory map.
	desiredMap := make(map[string]*mount.Entry)
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
	var changes []*Change

	// Unmount entries not reused in reverse to handle children before their parent.
	for i := len(current) - 1; i >= 0; i-- {
		if reuse[current[i].Dir] {
			changes = append(changes, &Change{Action: Keep, Entry: current[i]})
		} else {
			changes = append(changes, &Change{Action: Unmount, Entry: current[i]})
		}
	}

	// Mount desired entries not reused.
	for i := range desired {
		if !reuse[desired[i].Dir] {
			changes = append(changes, &Change{Action: Mount, Entry: desired[i]})
		}
	}

	return changes
}
