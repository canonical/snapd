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
	"github.com/snapcore/snapd/osutil/mount"
	"github.com/snapcore/snapd/osutil/sys"
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
	// function calls for mocking
	osutilIsDirectory = osutil.IsDirectory
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
var changePerform func(*Change, *Assumptions) ([]*Change, error)

// mimicRequired provides information if an error warrants a writable mimic.
//
// The returned path is the location where a mimic should be constructed.
func mimicRequired(err error) (needsMimic bool, path string) {
	switch err := err.(type) {
	case *ReadOnlyFsError:
		rofsErr := err
		return true, rofsErr.Path
	case *TrespassingError:
		tErr := err
		return true, tErr.ViolatedPath
	}
	return false, ""
}

func (c *Change) createPath(path string, pokeHoles bool, as *Assumptions) ([]*Change, error) {
	// If we've been asked to create a missing path, and the mount
	// entry uses the ignore-missing option, return an error.
	if c.Entry.XSnapdIgnoreMissing() {
		return nil, ErrIgnoredMissingMount
	}

	var err error
	var changes []*Change

	// Root until proven otherwise
	var uid sys.UserID = 0
	var gid sys.GroupID = 0
	mode := as.ModeForPath(path)

	// If the element doesn't exist we can attempt to create it.  We will
	// create the parent directory and then the final element relative to it.
	// The traversed space may be writable so we just try to create things
	// first.
	kind := c.Entry.XSnapdKind()

	// TODO: re-factor this, if possible, with inspection and preemptive
	// creation after the current release ships. This should be possible but
	// will affect tests heavily (churn, not safe before release).
	rs := as.RestrictionsFor(path)
	switch kind {
	case "":
		err = MkdirAll(path, mode, uid, gid, rs)
	case "file":
		err = MkfileAll(path, mode, uid, gid, rs)
	case "symlink":
		err = MksymlinkAll(path, mode, uid, gid, c.Entry.XSnapdSymlink(), rs)
	case "ensure-dir":
		uid = sysGetuid()
		gid = sysGetgid()
		// https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html
		// Mode hints cannot be used here because it does not support specifying all
		// directory paths within a given directory.
		mode = 0700
		err = MkdirAllWithin(path, c.Entry.XSnapdMustExistDir(), mode, uid, gid, rs)
	}
	if needsMimic, mimicPath := mimicRequired(err); needsMimic && pokeHoles {
		// If the error can be recovered by using a writable mimic
		// then construct one and try again.
		logger.Debugf("need to create writable mimic needed to create path %q (mount entry id: %q) (original error: %v)", path, c.Entry.XSnapdEntryID(), err)
		changes, err = createWritableMimic(mimicPath, c.Entry.XSnapdEntryID(), as)
		if err != nil {
			err = fmt.Errorf("cannot create writable mimic over %q: %s", mimicPath, err)
		} else {
			// Try once again. Note that we care *just* about the error. We have already
			// performed the hole poking and thus additional changes must be nil.
			_, err = c.createPath(path, false, as)
		}
	}
	return changes, err
}

func (c *Change) ensureTarget(as *Assumptions) ([]*Change, error) {
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
				_, err = c.createPath(path, false, as)
			} else {
				err = fmt.Errorf("cannot create symlink in %q: existing file in the way", path)
			}
		case "ensure-dir":
			if !fi.Mode().IsDir() {
				err = fmt.Errorf("cannot create ensure-dir target %q: existing file in the way", path)
			}
		}
	} else if os.IsNotExist(err) {
		pokeHoles := kind != "ensure-dir"
		changes, err = c.createPath(path, pokeHoles, as)
	} else {
		// If we cannot inspect the element let's just bail out.
		err = fmt.Errorf("cannot inspect %q: %v", path, err)
	}
	return changes, err
}

func (c *Change) ensureSource(as *Assumptions) ([]*Change, error) {
	var changes []*Change

	kind := c.Entry.XSnapdKind()

	// Source is not relevant to ensure-dir mounts that are intended for
	// creating missing directories based on the mount target.
	if kind == "ensure-dir" {
		return nil, nil
	}

	// We only have to do ensure bind mount source exists.
	// This also rules out symlinks.
	flags, _ := osutil.MountOptsToCommonFlags(c.Entry.Options)
	if flags&syscall.MS_BIND == 0 {
		return nil, nil
	}

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
		changes, err = c.createPath(path, pokeHoles, as)
	} else {
		// If we cannot inspect the element let's just bail out.
		err = fmt.Errorf("cannot inspect %q: %v", path, err)
	}

	return changes, err
}

// changePerformImpl is the real implementation of Change.Perform
func changePerformImpl(c *Change, as *Assumptions) (changes []*Change, err error) {
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
		changesTarget, err = c.ensureTarget(as)
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
		changesSource, err = c.ensureSource(as)
		// NOTE: we are collecting changes even if things fail. This is so that
		// upper layers can perform undo correctly.
		changes = append(changes, changesSource...)
		if err != nil {
			return changes, err
		}
	}

	// Perform the underlying mount / unmount / unlink call.
	err = c.lowLevelPerform(as)
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
func (c *Change) Perform(as *Assumptions) ([]*Change, error) {
	return changePerform(c, as)
}

// lowLevelPerform is simple bridge from Change to mount / unmount syscall.
func (c *Change) lowLevelPerform(as *Assumptions) error {
	var err error

	kind := c.Entry.XSnapdKind()
	// ensure-dir mounts attempts to create a potentially missing target directory during the ensureTarget step
	// and does not require any low-level actions. Directories created with ensure-dir mounts should never be removed.
	if kind == "ensure-dir" {
		return nil
	}

	switch c.Action {
	case Mount:
		kind := c.Entry.XSnapdKind()
		switch kind {
		case "symlink":
			// symlinks are handled in createInode directly, nothing to do here.
		case "", "file":
			flags, unparsed := osutil.MountOptsToCommonFlags(c.Entry.Options)
			// Split the mount flags from the event propagation changes.
			// Those have to be applied separately.
			const propagationMask = syscall.MS_SHARED | syscall.MS_SLAVE | syscall.MS_PRIVATE | syscall.MS_UNBINDABLE
			maskedFlagsRecursive := flags & syscall.MS_REC
			maskedFlagsPropagation := flags & propagationMask
			maskedFlagsNotPropagationNotRecursive := flags & ^(propagationMask | syscall.MS_REC)

			var flagsForMount uintptr
			if flags&syscall.MS_BIND == syscall.MS_BIND {
				// bind / rbind mount
				flagsForMount = uintptr(maskedFlagsNotPropagationNotRecursive | maskedFlagsRecursive)
				err = BindMount(c.Entry.Name, c.Entry.Dir, uint(flagsForMount))
			} else {
				// normal mount, not bind / rbind, not propagation change
				flagsForMount = uintptr(maskedFlagsNotPropagationNotRecursive)
				err = sysMount(c.Entry.Name, c.Entry.Dir, c.Entry.Type, uintptr(flagsForMount), strings.Join(unparsed, ","))
			}
			mountOpts, unknownFlags := mount.MountFlagsToOpts(int(flagsForMount))
			if unknownFlags != 0 {
				mountOpts = append(mountOpts, fmt.Sprintf("%#x", unknownFlags))
			}
			logger.Debugf("mount name:%q dir:%q type:%q opts:%s unparsed:%q (error: %v)",
				c.Entry.Name, c.Entry.Dir, c.Entry.Type, strings.Join(mountOpts, "|"), strings.Join(unparsed, ","), err)
			if err == nil && maskedFlagsPropagation != 0 {
				// now change mount propagation (shared/rshared, private/rprivate,
				// slave/rslave, unbindable/runbindable).
				flagsForMount := uintptr(maskedFlagsPropagation | maskedFlagsRecursive)
				mountOpts, unknownFlags := mount.MountFlagsToOpts(int(flagsForMount))
				if unknownFlags != 0 {
					mountOpts = append(mountOpts, fmt.Sprintf("%#x", unknownFlags))
				}
				err = sysMount("none", c.Entry.Dir, "", flagsForMount, "")
				logger.Debugf("mount name:%q dir:%q type:%q opts:%s unparsed:%q (error: %v)",
					"none", c.Entry.Dir, "", strings.Join(mountOpts, "|"), strings.Join(unparsed, ","), err)
			}
			if err == nil {
				as.AddChange(c)
			}
		}
		return err
	case Unmount:
		kind := c.Entry.XSnapdKind()
		switch kind {
		case "symlink":
			err = osRemove(c.Entry.Dir)
			logger.Debugf("remove %q (error: %v)", c.Entry.Dir, err)
		case "", "file":
			// Unmount and remount operations can fail with EINVAL if the given
			// mount does not exist; since here we only care about the
			// resulting configuration, let's not treat such situations as
			// errors.
			clearMissingMountError := func(err error) error {
				if err == syscall.EINVAL {
					// We attempted to unmount but got an EINVAL, one of the
					// possibilities and the only one unless we provided wrong
					// flags, is that the mount no longer exists.
					//
					// We can verify that now by scanning mountinfo:
					entries, _ := osutil.LoadMountInfo()
					for _, entry := range entries {
						if entry.MountDir == c.Entry.Dir {
							// Mount point still exists, EINVAL was unexpected.
							return err
						}
					}
					// We didn't find a mount point at the location we tried to
					// unmount. The EINVAL we observed indicates that the mount
					// profile no longer agrees with reality. The mount point
					// no longer exists. As such, consume the error and carry on.
					logger.Debugf("ignoring EINVAL from unmount, %q is not mounted", c.Entry.Dir)
					err = nil
				}
				return err
			}
			// Detach the mount point instead of unmounting it if requested.
			flags := umountNoFollow
			if c.Entry.XSnapdDetach() {
				flags |= syscall.MNT_DETACH
				// If we are detaching something then before performing the actual detach
				// switch the entire hierarchy to private event propagation (that is,
				// none). This works around a bit of peculiar kernel behavior when the
				// kernel reports EBUSY during a detach operation, because the changes
				// propagate in a way that conflicts with itself. This is also documented
				// in umount(2).
				err = sysMount("none", c.Entry.Dir, "", syscall.MS_REC|syscall.MS_PRIVATE, "")
				logger.Debugf("mount --make-rprivate %q (error: %v)", c.Entry.Dir, err)
				err = clearMissingMountError(err)
			}

			// Perform the raw unmount operation.
			if err == nil {
				err = sysUnmount(c.Entry.Dir, flags)
				umountOpts, unknownFlags := mount.UnmountFlagsToOpts(flags)
				if unknownFlags != 0 {
					umountOpts = append(umountOpts, fmt.Sprintf("%#x", unknownFlags))
				}
				logger.Debugf("umount %q %s (error: %v)", c.Entry.Dir, strings.Join(umountOpts, "|"), err)
				err = clearMissingMountError(err)
				if err != nil {
					return err
				}
			}
			if err == nil {
				as.AddChange(c)
			}

			// Open a path of the file we are considering the removal of.
			path := c.Entry.Dir
			var fd int
			fd, err = OpenPath(path)
			// If the place does not exist anymore, we are done.
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			defer sysClose(fd)

			// Don't attempt to remove anything from squashfs.
			// Note that this is not a perfect check and we also handle EROFS below.
			var statfsBuf syscall.Statfs_t
			err = sysFstatfs(fd, &statfsBuf)
			if err != nil {
				return err
			}
			if statfsBuf.Type == SquashfsMagic {
				return nil
			}

			if kind == "file" {
				// Don't attempt to remove non-empty files since they cannot be
				// the placeholders we created.
				var statBuf syscall.Stat_t
				err = sysFstat(fd, &statBuf)
				if err != nil {
					return err
				}
				if statBuf.Size != 0 {
					return nil
				}
			}

			// Remove the file or directory while using the full path. There's
			// no way to avoid a race here since there's no way to unlink a
			// file solely by file descriptor.
			err = osRemove(path)
			logger.Debugf("remove %q (error: %v)", path, err)
			// Unpack the low-level error that osRemove wraps into PathError.
			if packed, ok := err.(*os.PathError); ok {
				err = packed.Err
			}
			if err == syscall.EROFS {
				// If the underlying medium is read-only then ignore the error.
				// Instead of checking up front we just try to remove because
				// of https://bugs.launchpad.net/snapd/+bug/1867752 which showed us
				// two important properties:
				// 1) inside containers we cannot detect squashfs reliably and
				//    will always see FUSE instead. The problem is that there's no
				//    indication as to what is really mounted via statfs(2) and
				//    we would have to deduce that from mountinfo, trusting
				//    that fuse.<name> is not spoofed (as in, the name is not
				//    spoofed).
				// 2) rmdir of a bind mount (from a normal writable filesystem like ext4)
				//    over a read-only filesystem also yields EROFS without any indication
				//    that this is to be expected.
				logger.Debugf("cannot remove a mount point on read-only filesystem %q", path)
				return nil
			}
			if err == syscall.EBUSY {
				// It's still unclear how this can happen. For the time being
				// let the operation succeed and log the event.
				logger.Noticef("cannot remove mount point, got EBUSY: %q", path)
				if isMount, err := osutil.IsMounted(path); isMount {
					mounts, _ := osutil.LoadMountInfo()
					logger.Noticef("%q is still a mount point:\n%s", path, mounts)
				} else if err != nil {
					logger.Noticef("cannot read mountinfo: %v", err)
				}
				return nil
			}
			// If we were removing a directory but it was not empty then just
			// ignore the error. This is the equivalent of the non-empty file
			// check we do above. See rmdir(2) for explanation why we accept
			// more than one errno value.
			if kind == "" && (err == syscall.ENOTEMPTY || err == syscall.EEXIST) {
				return nil
			}
		}
		return err
	case Keep:
		as.AddChange(c)
		return nil
	}
	return fmt.Errorf("cannot process mount change: unknown action: %q", c.Action)
}

// Using dir is not enough to identify the mount entry, because when
// using layouts some directories could be used as mount points more
// than once. This can happen when, say, we have a layout for /dir/sd1
// and another one for /dir/sd2/sd3, being the case that /dir/sd1 and
// /dir/sd2/sd3 do not exist (but their parent dirs do exist) -
// /dir/sd2 will be one of the bind mounted directories of the tmpfs
// that is created in /dir to have a layout on /dir/sd1, while at the
// same time a tmpfs will be mounted in /dir/sd2 so we can have a
// layout in /dir/sd2/sd3. So /dir/sd2 is used twice with different
// filesystem types (none and tmpfs). As we make sure that mimics are
// created only once per directory, we should only have one entry per
// dir+fstype, being fstype either none or tmpfs.
//
// TODO Ideally we should have only one mount per mountpoint, but we
// perform mounts as we create the changes in Change.Perform which
// makes it difficult to create the full list of changes and then
// clean-up repeated mountpoints. In any case using this is still
// needed to handle mount namespaces created by older snapd versions.
type mountEntryId struct {
	dir    string
	fsType string
}

// neededChanges is the real implementation of NeededChanges
func neededChanges(currentProfile, desiredProfile *osutil.MountProfile) []*Change {
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

	// Make yet another copy of the current entries, to retain their original
	// order (the "current" variable is going to be sorted soon); just using
	// currentProfile.Entries is not reliable because it didn't undergo the
	// cleanup of the Dir paths.
	unsortedCurrent := make([]osutil.MountEntry, len(current))
	copy(unsortedCurrent, current)

	dumpMountEntries := func(entries []osutil.MountEntry, pfx string) {
		logger.Debug(pfx)
		for _, en := range entries {
			logger.Debugf("- %v", en)
		}
	}
	dumpMountEntries(current, "current mount entries")
	// Sort only the desired lists by directory name with implicit trailing
	// slash and the mount kind.
	// Note that the current profile is a log of what was applied and should
	// not be sorted at all.
	sort.Sort(byOriginAndMountPoint(desired))
	dumpMountEntries(desired, "desired mount entries (sorted)")

	// Construct a desired directory map.
	desiredMap := make(map[string]*osutil.MountEntry)
	for i := range desired {
		desiredMap[desired[i].Dir] = &desired[i]
	}

	// Indexed by mount point path.
	reuse := make(map[mountEntryId]bool)
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
	// sort them first
	sort.Sort(byOvernameAndMountPoint(current))
	for i := range current {
		dir := current[i].Dir
		if skipDir != "" && strings.HasPrefix(dir, skipDir) {
			logger.Debugf("skipping entry %q", current[i])
			continue
		}
		skipDir = "" // reset skip prefix as it no longer applies

		mountId := mountEntryId{dir, current[i].Type}
		if current[i].XSnapdOrigin() == "rootfs" {
			// This is the rootfs setup by snap-confine, we should not touch it
			logger.Debugf("reusing rootfs")
			reuse[mountId] = true
			continue
		}

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
			reuse[mountId] = true
			continue
		}

		// Reuse entries that are desired and identical in the current profile.
		if entry, ok := desiredMap[dir]; ok && current[i].Equal(entry) {
			logger.Debugf("reusing unchanged entry %q", current[i])
			reuse[mountId] = true
			continue
		}

		skipDir = strings.TrimSuffix(dir, "/") + "/"
	}

	logger.Debugf("desiredIDs: %v", desiredIDs)
	logger.Debugf("reuse: %v", reuse)

	// We are now ready to compute the necessary mount changes.
	var changes []*Change

	// Unmount entries not reused in reverse to handle children before their parent.
	unmountOrder := unsortedCurrent
	for i := len(unmountOrder) - 1; i >= 0; i-- {
		if reuse[mountEntryId{unmountOrder[i].Dir, unmountOrder[i].Type}] {
			changes = append(changes, &Change{Action: Keep, Entry: unmountOrder[i]})
		} else {
			var entry osutil.MountEntry = unmountOrder[i]
			entry.Options = append([]string(nil), entry.Options...)
			// If the mount entry can potentially host nested mount points then detach
			// rather than unmount, since detach will always succeed.
			shouldDetach := entry.Type == "tmpfs" || entry.OptBool("bind") || entry.OptBool("rbind")
			if shouldDetach && !entry.XSnapdDetach() {
				entry.Options = append(entry.Options, osutil.XSnapdDetach())
			}
			changes = append(changes, &Change{Action: Unmount, Entry: entry})
		}
	}

	var desiredNotReused []osutil.MountEntry
	for _, entry := range desired {
		if !reuse[mountEntryId{entry.Dir, entry.Type}] {
			desiredNotReused = append(desiredNotReused, entry)
		}
	}

	// Mount desired entries not reused, ordering by the mimic directories they
	// need created
	// We proceeds in three steps:
	// 1. Perform the mounts for the "overname" entries
	// 2. Perform the mounts for the entries which need a mimic
	// 3. Perform all the remaining desired mounts

	var newDesiredEntries []osutil.MountEntry
	var newIndependentDesiredEntries []osutil.MountEntry
	// Indexed by mount point path.
	addedDesiredEntries := make(map[string]bool)
	// This function is idempotent, it won't add the same entry twice
	addDesiredEntry := func(entry osutil.MountEntry) {
		if !addedDesiredEntries[entry.Dir] {
			logger.Debugf("adding entry: %s", entry)
			newDesiredEntries = append(newDesiredEntries, entry)
			addedDesiredEntries[entry.Dir] = true
		}
	}
	addIndependentDesiredEntry := func(entry osutil.MountEntry) {
		if !addedDesiredEntries[entry.Dir] {
			logger.Debugf("adding independent entry: %s", entry)
			newIndependentDesiredEntries = append(newIndependentDesiredEntries, entry)
			addedDesiredEntries[entry.Dir] = true
		}
	}

	logger.Debugf("processing mount entries")
	// Create a map of the target directories (mimics) needed for the visited
	// entries
	affectedTargetCreationDirs := map[string][]osutil.MountEntry{}
	for _, entry := range desiredNotReused {
		if entry.XSnapdOrigin() == "overname" {
			addIndependentDesiredEntry(entry)
		}

		// collect all entries, so that we know what mimics are needed
		parentTargetDir := filepath.Dir(entry.Dir)
		affectedTargetCreationDirs[parentTargetDir] = append(affectedTargetCreationDirs[parentTargetDir], entry)
	}

	if len(affectedTargetCreationDirs) != 0 {
		entriesForMimicDir := map[string][]osutil.MountEntry{}
		for parentTargetDir, entriesNeedingDir := range affectedTargetCreationDirs {
			// First check if any of the mount entries for the changes will potentially
			// result in creating a mimic. Note that to actually know if a given mount
			// entry will require a mimic when the mount target doesn't exist, we would
			// have to try and create a file/directory/symlink at the desired target,
			// however that would be a destructive change which is not appropriate here
			// (that is done in ChangePerform() instead), but for our purposes of sorting
			// mount entries it is sufficient to use this assumption.
			// We check if a mount entry would result in a potential mimic by just
			// checking if the file/dir/symlink that is the target of the mount exists
			// already in the form we need to to bind mount on top of it. If it
			// doesn't then we need to create a mimic and so we then go looking for
			// where to create the mimic.
			for _, entry := range entriesNeedingDir {
				exists := true
				switch entry.XSnapdKind() {
				case "":
					exists = osutilIsDirectory(entry.Dir)
				case "file":
					exists = osutil.FileExists(entry.Dir)
				case "symlink":
					exists = osutil.IsSymlink(entry.Dir)
				}

				// if it doesn't exist we may need a mimic
				if !exists {
					neededMimicDir := findFirstRootDirectoryThatExists(parentTargetDir)
					entriesForMimicDir[neededMimicDir] = append(entriesForMimicDir[neededMimicDir], entry)
					logger.Debugf("entry that requires %q: %v", neededMimicDir, entry)
				} else {
					// entry is independent
					addIndependentDesiredEntry(entry)
				}
			}
		}

		// sort the mimic creation dirs to get the correct ordering of mimics to
		// create dirs in: the sorting algorithm places parent directories
		// before children.
		allMimicCreationDirs := []string{}
		for mimicDir := range entriesForMimicDir {
			allMimicCreationDirs = append(allMimicCreationDirs, mimicDir)
		}

		sort.Strings(allMimicCreationDirs)

		logger.Debugf("all mimics:")
		for _, mimicDir := range allMimicCreationDirs {
			logger.Debugf("- %v", mimicDir)
		}

		for _, mimicDir := range allMimicCreationDirs {
			// make sure to sort the entries for each mimic dir in a consistent
			// order
			entries := entriesForMimicDir[mimicDir]
			sort.Sort(byOriginAndMountPoint(entries))
			for _, entry := range entries {
				addDesiredEntry(entry)
			}
		}
	}

	sort.Sort(byOriginAndMountPoint(newIndependentDesiredEntries))
	allEntries := append(newIndependentDesiredEntries, newDesiredEntries...)
	dumpMountEntries(allEntries, "mount entries ordered as they will be applied")
	for _, entry := range allEntries {
		changes = append(changes, &Change{Action: Mount, Entry: entry})
	}

	return changes
}

func findFirstRootDirectoryThatExists(desiredParentDir string) string {
	// trivial case - the dir already exists
	if osutilIsDirectory(desiredParentDir) {
		return desiredParentDir
	}

	// otherwise we need to recurse up to find the first dir that exists where
	// we would place the mimic - note that this cannot recurse infinitely,
	// since at some point we will reach "/" which always exists
	return findFirstRootDirectoryThatExists(filepath.Dir(desiredParentDir))
}

// NeededChanges computes the changes required to change current to desired mount entries.
//
// A diff-like operation on the mount profile is computed. Some of the mount
// entries from the current profile may be reused.
var NeededChanges = func(current, desired *osutil.MountProfile) []*Change {
	return neededChanges(current, desired)
}
