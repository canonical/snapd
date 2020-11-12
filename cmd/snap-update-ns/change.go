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

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/mount"
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
var changePerform func(*Change, *Assumptions) ([]*Change, error)

// mimicRequired provides information if an error warrants a writable mimic.
//
// The returned path is the location where a mimic should be constructed.
func mimicRequired(err error) (needsMimic bool, path string) {
	switch err.(type) {
	case *ReadOnlyFsError:
		rofsErr := err.(*ReadOnlyFsError)
		return true, rofsErr.Path
	case *TrespassingError:
		tErr := err.(*TrespassingError)
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

	// In case we need to create something, some constants.
	const (
		uid = 0
		gid = 0
	)
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
	}
	if needsMimic, mimicPath := mimicRequired(err); needsMimic && pokeHoles {
		// If the error can be recovered by using a writable mimic
		// then construct one and try again.
		logger.Debugf("need to create writable mimic needed to create path %q (original error: %v)", path, err)
		changes, err = createWritableMimic(mimicPath, path, as)
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
		}
	} else if os.IsNotExist(err) {
		changes, err = c.createPath(path, true, as)
	} else {
		// If we cannot inspect the element let's just bail out.
		err = fmt.Errorf("cannot inspect %q: %v", path, err)
	}
	return changes, err
}

func (c *Change) ensureSource(as *Assumptions) ([]*Change, error) {
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
			}

			// Perform the raw unmount operation.
			if err == nil {
				err = sysUnmount(c.Entry.Dir, flags)
				umountOpts, unknownFlags := mount.UnmountFlagsToOpts(flags)
				if unknownFlags != 0 {
					umountOpts = append(umountOpts, fmt.Sprintf("%#x", unknownFlags))
				}
				logger.Debugf("umount %q %s (error: %v)", c.Entry.Dir, strings.Join(umountOpts, "|"), err)
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
			// If we were removing a directory but it was not empty then just
			// ignore the error. This is the equivalent of the non-empty file
			// check we do above. See rmdir(2) for explanation why we accept
			// more than one errno value.
			if kind == "" && (err == syscall.ENOTEMPTY || err == syscall.EEXIST) {
				return nil
			}
			if features.RobustMountNamespaceUpdates.IsEnabled() {
				// FIXME: This should not be necessary. It is necessary because
				// mimic construction code is not considering all layouts in tandem
				// and doesn't know enough about base file system to construct
				// mimics in the order that would prevent them from nesting.
				//
				// By ignoring EBUSY here and by continuing to tear down the mimic
				// tmpfs entirely (without any reuse) we guarantee that at the end
				// of the day the nested mimic case is entirely removed.
				//
				// In an ideal world we would model this better and could do
				// without this edge case.
				if (kind == "" || kind == "file") && err == syscall.EBUSY {
					logger.Debugf("cannot remove busy mount point %q", path)
					return nil
				}
			}
		}
		return err
	case Keep:
		as.AddChange(c)
		return nil
	}
	return fmt.Errorf("cannot process mount change: unknown action: %q", c.Action)
}

// neededChangesOld is the real implementation of NeededChanges
// This function is used when RobustMountNamespaceUpdate is not enabled.
func neededChangesOld(currentProfile, desiredProfile *osutil.MountProfile) []*Change {
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
			var entry osutil.MountEntry = current[i]
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

	// Mount desired entries not reused.
	for i := range desired {
		if !reuse[desired[i].Dir] {
			changes = append(changes, &Change{Action: Mount, Entry: desired[i]})
		}
	}

	return changes
}

// neededChangesNew is the real implementation of NeededChanges
// This function is used when RobustMountNamespaceUpdate is enabled.
func neededChangesNew(currentProfile, desiredProfile *osutil.MountProfile) []*Change {
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

	// We are now ready to compute the necessary mount changes.
	var changes []*Change

	// Unmount entries in reverse order, so that the most nested element is
	// always processed first.
	for i := len(current) - 1; i >= 0; i-- {
		var entry osutil.MountEntry = current[i]
		entry.Options = append([]string(nil), entry.Options...)
		switch {
		case entry.XSnapdSynthetic() && entry.Type == "tmpfs":
			// Synthetic changes are rooted under a tmpfs, detach that tmpfs to
			// remove them all.
			if !entry.XSnapdDetach() {
				entry.Options = append(entry.Options, osutil.XSnapdDetach())
			}
		case entry.XSnapdSynthetic():
			// Consume all other syn ethic entries without emitting either a
			// mount, unmount or keep change.  This relies on the fact that all
			// synthetic mounts are created by a mimic underneath a tmpfs that
			// is detached, as coded above.
			continue
		case entry.OptBool("rbind") || entry.Type == "tmpfs":
			// Recursive bind mounts and non-mimic tmpfs mounts need to be
			// detached because they can contain other mount points that can
			// otherwise propagate in a self-conflicting way.
			if !entry.XSnapdDetach() {
				entry.Options = append(entry.Options, osutil.XSnapdDetach())
			}
		case entry.OptBool("bind") && entry.XSnapdKind() == "file":
			// Bind mounted files are detached. If a bind mounted file open or
			// mapped into a process as a library, then attempting to unmount
			// it will result in EBUSY.
			//
			// This can happen when a snap has a service, for example one using
			// a library mounted via a bind mount and an absent content
			// connection. Subsequent connection of the content connection will
			// trigger re-population of the mount namespace, which will start
			// by tearing down the existing file bind-mount. To prevent this,
			// detach the mount instead.
			if !entry.XSnapdDetach() {
				entry.Options = append(entry.Options, osutil.XSnapdDetach())
			}
		}
		// Unmount all changes that were not eliminated.
		changes = append(changes, &Change{Action: Unmount, Entry: entry})
	}

	// Mount desired entries.
	for i := range desired {
		changes = append(changes, &Change{Action: Mount, Entry: desired[i]})
	}

	return changes
}

// NeededChanges computes the changes required to change current to desired mount entries.
//
// The algorithm differs depending on the value of the robust mount namespace
// updates feature flag. If the flag is enabled then the current profile is
// entirely undone and the desired profile is constructed from scratch.
//
// If the flag is disabled then a diff-like operation on the mount profile is
// computed. Some of the mount entries from the current profile may be reused.
// The diff approach doesn't function correctly in cases of nested mimics.
var NeededChanges = func(current, desired *osutil.MountProfile) []*Change {
	if features.RobustMountNamespaceUpdates.IsEnabled() {
		return neededChangesNew(current, desired)
	}
	return neededChangesOld(current, desired)
}
