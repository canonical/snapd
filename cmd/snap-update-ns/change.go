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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
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

var (
	// ErrIgnoredMissingMount is returned when a mount entry has
	// been marked with x-snapd.ignore-missing, and the mount
	// source or target do not exist.
	ErrIgnoredMissingMount = errors.New("mount source or target are missing")
)

// Change describes a change to the mount table (action and the entry to act on).
type Change struct {
	Entry  osutil.MountEntry
	Action Action
}

// String formats mount change to a human-readable line.
func (c Change) String() string {
	return fmt.Sprintf("%s (%s)", c.Action, c.Entry)
}

// changePerform is Change.Perform that can be mocked for testing.
var changePerform func(*Change) ([]*Change, error)

// mimicRequired provides information if an error warrants a writable mimic.
//
// The returned path is the location where a mimic should be constructed.
func mimicRequired(err error) (needsMimic bool, path string) {
	switch err.(type) {
	case *ReadOnlyFsError:
		rofsErr := err.(*ReadOnlyFsError)
		return true, rofsErr.Path
	}
	return false, ""
}

func (c *Change) createPath(path string, pokeHoles bool) ([]*Change, error) {
	// If we've been asked to create a missing path, and the mount
	// entry uses the ignore-missing option, return an error.
	if c.Entry.XSnapdIgnoreMissing() {
		return nil, ErrIgnoredMissingMount
	}

	var err error
	var changes []*Change

	// In case we need to create something, some constants.
	const (
		mode = 0755
		uid  = 0
		gid  = 0
	)

	// If the element doesn't exist we can attempt to create it.  We will
	// create the parent directory and then the final element relative to it.
	// The traversed space may be writable so we just try to create things
	// first.
	kind := c.Entry.XSnapdKind()

	// TODO: re-factor this, if possible, with inspection and preemptive
	// creation after the current release ships. This should be possible but
	// will affect tests heavily (churn, not safe before release).
	switch kind {
	case "":
		err = MkdirAll(path, mode, uid, gid)
	case "file":
		err = MkfileAll(path, mode, uid, gid)
	case "symlink":
		err = MksymlinkAll(path, mode, uid, gid, c.Entry.XSnapdSymlink())
	}
	if needsMimic, mimicPath := mimicRequired(err); needsMimic && pokeHoles {
		// If the error can be recovered by using a writable mimic
		// then construct one and try again.
		changes, err = createWritableMimic(mimicPath, path)
		if err != nil {
			err = fmt.Errorf("cannot create writable mimic over %q: %s", mimicPath, err)
		} else {
			// Try once again. Note that we care *just* about the error. We have already
			// performed the hole poking and thus additional changes must be nil.
			_, err = c.createPath(path, false)
		}
	}
	return changes, err
}

func (c *Change) ensureTarget() ([]*Change, error) {
	var changes []*Change

	kind := c.Entry.XSnapdKind()
	path := c.Entry.Dir

	// We use lstat to ensure that we don't follow a symlink in case one was
	// set up by the snap. Note that at the time this is run, all the snap's
	// processes are frozen but if the path is a directory controlled by the
	// user (typically in /home) then we may still race with user processes
	// that change it.
	fi, err := osLstat(path)

	if err == nil {
		// If the element already exists we just need to ensure it is of
		// the correct type. The desired type depends on the kind of entry
		// we are working with.
		switch kind {
		case "":
			if !fi.Mode().IsDir() {
				err = fmt.Errorf("cannot use %q as mount point: not a directory", path)
			}
		case "file":
			if !fi.Mode().IsRegular() {
				err = fmt.Errorf("cannot use %q as mount point: not a regular file", path)
			}
		case "symlink":
			if fi.Mode()&os.ModeSymlink == os.ModeSymlink {
				// Create path verifies the symlink or fails if it is not what we wanted.
				_, err = c.createPath(path, false)
			} else {
				err = fmt.Errorf("cannot create symlink in %q: existing file in the way", path)
			}
		}
	} else if os.IsNotExist(err) {
		changes, err = c.createPath(path, true)
	} else {
		// If we cannot inspect the element let's just bail out.
		err = fmt.Errorf("cannot inspect %q: %v", path, err)
	}
	return changes, err
}

func (c *Change) ensureSource() ([]*Change, error) {
	var changes []*Change

	// We only have to do ensure bind mount source exists.
	// This also rules out symlinks.
	flags, _ := osutil.MountOptsToCommonFlags(c.Entry.Options)
	if flags&syscall.MS_BIND == 0 {
		return nil, nil
	}

	kind := c.Entry.XSnapdKind()
	path := c.Entry.Name
	fi, err := osLstat(path)

	if err == nil {
		// If the element already exists we just need to ensure it is of
		// the correct type. The desired type depends on the kind of entry
		// we are working with.
		switch kind {
		case "":
			if !fi.Mode().IsDir() {
				err = fmt.Errorf("cannot use %q as bind-mount source: not a directory", path)
			}
		case "file":
			if !fi.Mode().IsRegular() {
				err = fmt.Errorf("cannot use %q as bind-mount source: not a regular file", path)
			}
		}
	} else if os.IsNotExist(err) {
		// NOTE: This createPath is using pokeHoles, to make read-only places
		// writable, but only for layouts and not for other (typically content
		// sharing) mount entries.
		//
		// This is done because the changes made with pokeHoles=true are only
		// visible in this current mount namespace and are not generally
		// visible from other snaps because they inhabit different namespaces.
		//
		// In other words, changes made here are only observable by the single
		// snap they apply to. As such they are useless for content sharing but
		// very much useful to layouts.
		pokeHoles := c.Entry.XSnapdOrigin() == "layout"
		changes, err = c.createPath(path, pokeHoles)
	} else {
		// If we cannot inspect the element let's just bail out.
		err = fmt.Errorf("cannot inspect %q: %v", path, err)
	}

	return changes, err
}

// changePerformImpl is the real implementation of Change.Perform
func changePerformImpl(c *Change) (changes []*Change, err error) {
	if c.Action == Mount {
		var changesSource, changesTarget []*Change
		// We may be asked to bind mount a file, bind mount a directory, mount
		// a filesystem over a directory, or create a symlink (which is abusing
		// the "mount" concept slightly). That actual operation is performed in
		// c.lowLevelPerform. Here we just set the stage to make that possible.
		//
		// As a result of this ensure call we may need to make the medium writable
		// and that's why we may return more changes as a result of performing this
		// one.
		changesTarget, err = c.ensureTarget()
		// NOTE: we are collecting changes even if things fail. This is so that
		// upper layers can perform undo correctly.
		changes = append(changes, changesTarget...)
		if err != nil {
			return changes, err
		}

		// At this time we can be sure that the target element (for files and
		// directories) exists and is of the right type or that it (for
		// symlinks) doesn't exist but the parent directory does.
		// This property holds as long as we don't interact with locations that
		// are under the control of regular (non-snap) processes that are not
		// suspended and may be racing with us.
		changesSource, err = c.ensureSource()
		// NOTE: we are collecting changes even if things fail. This is so that
		// upper layers can perform undo correctly.
		changes = append(changes, changesSource...)
		if err != nil {
			return changes, err
		}
	}

	// Perform the underlying mount / unmount / unlink call.
	err = c.lowLevelPerform()
	return changes, err
}

func init() {
	changePerform = changePerformImpl
}

// Perform executes the desired mount or unmount change using system calls.
// Filesystems that depend on helper programs or multiple independent calls to
// the kernel (--make-shared, for example) are unsupported.
//
// Perform may synthesize *additional* changes that were necessary to perform
// this change (such as mounted tmpfs or overlayfs).
func (c *Change) Perform() ([]*Change, error) {
	return changePerform(c)
}

// lowLevelPerform is simple bridge from Change to mount / unmount syscall.
func (c *Change) lowLevelPerform() error {
	var err error
	switch c.Action {
	case Mount:
		kind := c.Entry.XSnapdKind()
		switch kind {
		case "symlink":
			// symlinks are handled in createInode directly, nothing to do here.
		case "", "file":
			flags, unparsed := osutil.MountOptsToCommonFlags(c.Entry.Options)
			// Use Secure.BindMount for bind mounts
			if flags&syscall.MS_BIND == syscall.MS_BIND {
				err = BindMount(c.Entry.Name, c.Entry.Dir, uint(flags))
			} else {
				err = sysMount(c.Entry.Name, c.Entry.Dir, c.Entry.Type, uintptr(flags), strings.Join(unparsed, ","))
			}
			logger.Debugf("mount %q %q %q %d %q (error: %v)", c.Entry.Name, c.Entry.Dir, c.Entry.Type, uintptr(flags), strings.Join(unparsed, ","), err)
		}
		return err
	case Unmount:
		kind := c.Entry.XSnapdKind()
		switch kind {
		case "symlink":
			err = osRemove(c.Entry.Dir)
			logger.Debugf("remove %q (error: %v)", c.Entry.Dir, err)
		case "", "file":
			flags := umountNoFollow
			if c.Entry.XSnapdDetach() {
				flags |= syscall.MNT_DETACH
			}
			err = sysUnmount(c.Entry.Dir, flags)
			logger.Debugf("umount %q (error: %v)", c.Entry.Dir, err)
		}
		return err
	case Keep:
		return nil
	}
	return fmt.Errorf("cannot process mount change: unknown action: %q", c.Action)
}

// NeededChanges computes the changes required to change current to desired mount entries.
//
// The current and desired profiles is a fstab like list of mount entries. The
// lists are processed and a "diff" of mount changes is produced. The mount
// changes, when applied in order, transform the current profile into the
// desired profile.
func NeededChanges(currentProfile, desiredProfile *osutil.MountProfile) []*Change {
	// Copy both profiles as we will want to mutate them.
	current := make([]osutil.MountEntry, len(currentProfile.Entries))
	copy(current, currentProfile.Entries)
	desired := make([]osutil.MountEntry, len(desiredProfile.Entries))
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
	sort.Sort(byOriginAndMagicDir(current))
	sort.Sort(byOriginAndMagicDir(desired))

	// Construct a desired directory map.
	desiredMap := make(map[string]*osutil.MountEntry)
	for i := range desired {
		desiredMap[desired[i].Dir] = &desired[i]
	}

	// Indexed by mount point path.
	reuse := make(map[string]bool)
	// Indexed by entry ID
	desiredIDs := make(map[string]bool)
	var skipDir string

	// Collect the IDs of desired changes.
	// We need that below to keep implicit changes from the current profile.
	for i := range desired {
		desiredIDs[desired[i].XSnapdEntryID()] = true
	}

	// Compute reusable entries: those which are equal in current and desired and which
	// are not prefixed by another entry that changed.
	for i := range current {
		dir := current[i].Dir
		if skipDir != "" && strings.HasPrefix(dir, skipDir) {
			logger.Debugf("skipping entry %q", current[i])
			continue
		}
		skipDir = "" // reset skip prefix as it no longer applies

		// Reuse synthetic entries if their needed-by entry is desired.
		// Synthetic entries cannot exist on their own and always couple to a
		// non-synthetic entry.

		// NOTE: Synthetic changes have a special purpose.
		//
		// They are a "shadow" of mount events that occurred to allow one of
		// the desired mount entries to be possible. The changes have only one
		// goal: tell snap-update-ns how those mount events can be undone in
		// case they are no longer needed. The actual changes may have been
		// different and may have involved steps not represented as synthetic
		// mount entires as long as those synthetic entries can be undone to
		// reverse the effect. In reality each non-tmpfs synthetic entry was
		// constructed using a temporary bind mount that contained the original
		// mount entries of a directory that was hidden with a tmpfs, but this
		// fact was lost.
		if current[i].XSnapdSynthetic() && desiredIDs[current[i].XSnapdNeededBy()] {
			logger.Debugf("reusing synthetic entry %q", current[i])
			reuse[dir] = true
			continue
		}

		// Reuse entries that are desired and identical in the current profile.
		if entry, ok := desiredMap[dir]; ok && current[i].Equal(entry) {
			logger.Debugf("reusing unchanged entry %q", current[i])
			reuse[dir] = true
			continue
		}

		skipDir = strings.TrimSuffix(dir, "/") + "/"
	}

	logger.Debugf("desiredIDs: %v", desiredIDs)
	logger.Debugf("reuse: %v", reuse)

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
