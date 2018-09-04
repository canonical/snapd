// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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
	"strings"
	"syscall"
)

// Assumptions track the assumptions about the state of the filesystem.
//
// Assumptions constitute the global part of the write restriction management.
// Assumptions are global in the sense that they span multiple distinct write
// operations. In contrast, Restrictions track per-operation state.
type Assumptions struct {
	unrestrictedPaths []string
	pastChanges       []*Change
}

// AddUnrestrictedPaths adds a list of directories where writing is allowed
// even if it would hit the real host filesystem (or transit through the host
// filesystem). This is intended to be used with certain well-known locations
// such as /tmp, $SNAP_DATA and $SNAP.
func (as *Assumptions) AddUnrestrictedPaths(paths ...string) {
	as.unrestrictedPaths = append(as.unrestrictedPaths, paths...)
}

// isRestricted returns true if a path follows restricted writing scheme.
//
// Writing to a restricted path results in step-by-step validation of each
// directory, starting from the root of the file system. Unless writing is
// allowed a mimic must be constructed to ensure that writes are not visible in
// undesired locations of the host filesystem.
//
// Provided path is the full, absolute path of the entity that needs to be
// created (directory, file or symbolic link).
func (as *Assumptions) isRestricted(path string) bool {
	// Anything rooted at one of the unrestricted paths is not restricted.
	// Those are for things like /var/snap/, for example.
	for _, p := range as.unrestrictedPaths {
		if p == "/" || p == path || strings.HasPrefix(path, filepath.Clean(p)+"/") {
			return false
		}

	}
	// All other paths are restricted
	return true
}

// MockUnrestrictedPaths replaces the set of path paths without any restrictions.
func (as *Assumptions) MockUnrestrictedPaths(paths ...string) (restore func()) {
	old := as.unrestrictedPaths
	as.unrestrictedPaths = paths
	return func() {
		as.unrestrictedPaths = old
	}
}

// AddChange records the fact that a change was applied to the system.
func (as *Assumptions) AddChange(change *Change) {
	as.pastChanges = append(as.pastChanges, change)
}

// canWriteToDirectory returns true if writing to a given directory is allowed.
//
// Writing is allowed in one of thee cases:
// 1) The directory is in one of the explicitly permitted locations.
//    This is the strongest permission as it explicitly allows writing to
//    places that may show up on the host, one of the examples being $SNAP_DATA.
// 2) The directory is on a read-only filesystem.
// 3) The directory is on a tmpfs created by snapd.
func (as *Assumptions) canWriteToDirectory(dirFd int, dirName string) (bool, error) {
	if !as.isRestricted(dirName) {
		return true, nil
	}
	var fsData syscall.Statfs_t
	if err := sysFstatfs(dirFd, &fsData); err != nil {
		return false, fmt.Errorf("cannot fstatfs %q: %s", dirName, err)
	}
	// Writing to read only directories is allowed because EROFS is handled
	// by each of the writing helpers already.
	if ok := isReadOnly(dirName, &fsData); ok {
		return true, nil
	}
	// Writing to a trusted tmpfs is allowed because those are not leaking to
	// the host.
	if ok := isSnapdCreatedPrivateTmpfs(dirName, &fsData, as.pastChanges); ok {
		return true, nil
	}
	// If writing is not not allowed by one of the three rules above then it is
	// disallowed.
	return false, nil
}

// RestrictionsFor computes restrictions for the desired path.
func (as *Assumptions) RestrictionsFor(desiredPath string) *Restrictions {
	if as.isRestricted(desiredPath) {
		return &Restrictions{assumptions: as, desiredPath: desiredPath, restricted: true}
	}
	return nil
}

// Restrictions contains meta-data of a compound write operation.
//
// This structure helps various functions that write to the filesystem to keep
// track of the ultimate destination across several calls (e.g. the function
// that creates a file needs to call helpers to create subsequent directories).
// Keeping track of the desired path aids in constructing useful error messages.
//
// In addition the structure keeps track of the restricted write mode flag which
// is based on the full path of the desired object being constructed. This allows
// various write helpers to avoid trespassing on host filesystem in places that
// are not expected to be written to by snapd (e.g. outside of $SNAP_DATA).
type Restrictions struct {
	assumptions *Assumptions
	desiredPath string
	restricted  bool
}

// Check verifies if writing to a directory would trespass on the host.
//
// The check is only performed in restricted mode. If the check fails a
// TrespassingError is returned.
func (rs *Restrictions) Check(dirFd int, dirName string) error {
	if rs == nil || !rs.restricted {
		return nil
	}
	// In restricted mode check the directory before attempting to write to it.
	ok, err := rs.assumptions.canWriteToDirectory(dirFd, dirName)
	if ok || err != nil {
		return err
	}
	if dirName == "/" {
		// If writing to / is not allowed then we are in a tough spot because
		// we cannot construct a writable mimic over /. This should never
		// happen in normal circumstances because the root filesystem is some
		// kind of base snap.
		return fmt.Errorf("cannot recover from trespassing over /")
	}
	return &TrespassingError{ViolatedPath: dirName, DesiredPath: rs.desiredPath}
}

// Lift lifts write restrictions for the desired path.
//
// This function should be called when, as subsequent components of a path are
// either discovered or created, the conditions for using restricted mode are
// no longer true.
func (rs *Restrictions) Lift() {
	if rs != nil {
		rs.restricted = false
	}
}

// TrespassingError is an error when filesystem operation would affect the host.
type TrespassingError struct {
	ViolatedPath string
	DesiredPath  string
}

// Error returns a formatted error message.
func (e *TrespassingError) Error() string {
	return fmt.Sprintf("cannot write to %q because it would affect the host in %q", e.DesiredPath, e.ViolatedPath)
}

// isReadOnly returns true if a directory is ready only.
//
// Directories are read only when they reside on file systems mounted in read
// only mode or when the underlying file system itself is inherently read only.
func isReadOnly(dirName string, fsData *syscall.Statfs_t) bool {
	// If something is mounted with f_flags & ST_RDONLY then is read-only.
	if fsData.Flags&StReadOnly == StReadOnly {
		return true
	}
	// If something is a known read-only file-system then it is safe.
	// Older copies of snapd were not mounting squashfs as read only.
	if fsData.Type == SquashfsMagic {
		return true
	}
	return false
}

// isSnapdCreatedPrivateTmpfs returns true if a directory is a tmpfs mounted by snapd.
//
// The function inspects the directory and a list of changes that were applied
// to the mount namespace. A directory is trusted if it is a tmpfs that was
// mounted by snap-confine or snapd-update-ns. Note that sub-directories of a
// trusted tmpfs are not considered trusted by this function.
func isSnapdCreatedPrivateTmpfs(dirName string, fsData *syscall.Statfs_t, changes []*Change) bool {
	// If something is not a tmpfs it cannot be the trusted tmpfs we are looking for.
	if fsData.Type != TmpfsMagic {
		return false
	}
	// Any of the past changes that mounted a tmpfs exactly at the directory we
	// are inspecting is considered as trusted. This is conservative because it
	// doesn't trust sub-directories of a trusted tmpfs. This approach is
	// sufficient for the intended use.
	//
	// The algorithm goes over all the changes in reverse and picks up the
	// first tmpfs mount or unmount action that matches the directory name.
	// The set of constraints in snap-update-ns and snapd prevent from mounting
	// over an existing mount point so we don't need to consider e.g. a bind
	// mount shadowing an active tmpfs.
	for i := len(changes) - 1; i >= 0; i-- {
		change := changes[i]
		if change.Entry.Type == "tmpfs" && change.Entry.Dir == dirName {
			return change.Action == Mount
		}
	}
	// TODO: As a special exception, assume that a tmpfs over /var/lib is
	// trusted. This tmpfs is created by snap-confine as a "quirk" to support
	// a particular behavior of LXD.  Once the quirk is migrated to a mount
	// profile (or removed entirely if no longer necessary) the following code
	// fragment can go away.
	return dirName == "/var/lib"
}
