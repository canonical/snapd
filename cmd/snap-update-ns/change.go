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
	"os"
	"path"
	"sort"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/interfaces/mount"
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

var (
	sysMount   = syscall.Mount
	sysUnmount = syscall.Unmount
	osLstat    = os.Lstat
	osMkdirAll = os.MkdirAll
	osChown    = os.Chown
)

const unmountNoFollow = 8

const AT_FDCWD = -100 // not available through syscall

// SecureMkdirAll is the secure variant of os.MkdirAll.
//
// Unlike the regular version this implementation does not follow any symbolic
// links. At all times the new directory segment is created using mkdirat(2)
// while holding an open file descriptor to the parent directory.
//
// The only handled error is mkdirat(2) that fails with EEXIST. All other
// errors are fatal but there is no attempt to undo anything that was created.
func SecureMkdirAll(name string, perm os.FileMode) error {
	// XXX: use O_PATH here?
	const openFlags = syscall.O_NOFOLLOW | syscall.O_CLOEXEC | syscall.O_DIRECTORY

	// Declare var and don't assign-declare below to ensure we don't swallow
	// any errors by mistake.
	var err error
	// Start at the current working directory by default.
	var fd int = AT_FDCWD

	// Keep track of the number of open/close calls
	var openCloseCount int

	// If path is absolute then open the root directory and start there.
	if path.IsAbs(name) {
		fd, err = syscall.Open("/", openFlags, 0)
		if err != nil {
			return fmt.Errorf("cannot open root directory, %v", err)
		}
		openCloseCount += 1
	}

	// Split the path by entries and create each element using mkdirat() using
	// the parent directory as reference. Each time we open the newly created
	// segment using the O_NOFOLLOW and O_DIRECTORY flag so that symlink
	// attacks are impossible to carry out.
	for _, segment := range strings.Split(name, "/") {
		if segment == "" {
			// Skip empty element corresponding to the leading slash.
			continue
		}
		if err = syscall.Mkdirat(fd, segment, uint32(perm)); err != nil {
			if err != syscall.EEXIST {
				return fmt.Errorf("cannot mkdir path segment %q, %v", segment, err)
			}
		}
		previousFd := fd
		fd, err = syscall.Openat(fd, segment, openFlags, 0)
		openCloseCount += 1
		if previousFd != AT_FDCWD {
			if err := syscall.Close(previousFd); err != nil {
				return fmt.Errorf("cannot close previous file descriptor, %v", err)
			}
			openCloseCount -= 1
		}
		if err != nil {
			return fmt.Errorf("cannot open path segment %q, %v", segment, err)
		}
	}
	if fd != AT_FDCWD {
		if err = syscall.Close(fd); err != nil {
			return fmt.Errorf("cannot close file descriptor, %v", err)
		}
		openCloseCount -= 1
	}
	if openCloseCount != 0 {
		panic(fmt.Sprintf("BUG in SecureMkdirAll, open-close count not balanced, %d", openCloseCount))
	}
	return nil
}

// Perform executes the desired mount or unmount change using system calls.
// Filesystems that depend on helper programs or multiple independent calls to
// the kernel (--make-shared, for example) are unsupported.
func (c *Change) Perform() error {
	switch c.Action {
	case Mount:
		flags, err := mount.OptsToFlags(c.Entry.Options)
		if err != nil {
			return err
		}
		// If the mount point is not present then create a directory in its
		// place.  This is very naive, doesn't handle read-only file systems
		// but it is a good starting point for people working with things like
		// $SNAP_DATA/subdirectory.
		//
		// We use lstat to ensure that we don't follow the symlink in case one
		// was set up by the snap but this is not coded defensively as the code
		// may execute concurrently with snap application processes.
		//
		// To have better defense we may consider freezing the freezer cgroup
		// belonging to the snap during the execution of snap-update-ns.
		if _, err := osLstat(c.Entry.Dir); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			// TODO: use the right mode and ownership.
			if err := osMkdirAll(c.Entry.Dir, 0755); err != nil {
				return err
			}
			if err := osChown(c.Entry.Dir, 0, 0); err != nil {
				return err
			}
		}
		return sysMount(c.Entry.Name, c.Entry.Dir, c.Entry.Type, uintptr(flags), "")
	case Unmount:
		return sysUnmount(c.Entry.Dir, unmountNoFollow)
	}
	return fmt.Errorf("cannot process mount change, unknown action: %q", c.Action)
}

// NeededChanges computes the changes required to change current to desired mount entries.
//
// The current and desired profiles is a fstab like list of mount entries. The
// lists are processed and a "diff" of mount changes is produced. The mount
// changes, when applied in order, transform the current profile into the
// desired profile.
func NeededChanges(currentProfile, desiredProfile *mount.Profile) []Change {
	// Copy both profiles as we will want to mutate them.
	current := make([]mount.Entry, len(currentProfile.Entries))
	copy(current, currentProfile.Entries)
	desired := make([]mount.Entry, len(desiredProfile.Entries))
	copy(desired, desiredProfile.Entries)

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
	var changes []Change

	// Unmount entries not reused in reverse to handle children before their parent.
	for i := len(current) - 1; i >= 0; i-- {
		if reuse[current[i].Dir] {
			changes = append(changes, Change{Action: Keep, Entry: current[i]})
		} else {
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
