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
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/logger"
)

// Assumptions track the assumptions about the state of the filesystem.
//
// Assumptions constitute the global part of the write restriction management.
// Assumptions are global in the sense that they span multiple distinct write
// operations. In contrast, Restrictions track per-operation state.
type Assumptions struct {
	unrestrictedPaths []string
	pastChanges       []*Change

	// verifiedDevices represents the set of devices that are verified as a tmpfs
	// that was mounted by snapd. Those are only discovered on-demand. The
	// major:minor number is packed into one uint64 as in syscall.Stat_t.Dev
	// field.
	verifiedDevices map[uint64]bool

	// modeHints overrides implicit 0755 mode of directories created while
	// ensuring source and target paths exist.
	modeHints []ModeHint
}

// ModeHint provides mode for directories created to satisfy mount changes.
type ModeHint struct {
	PathGlob string
	Mode     os.FileMode
}

// AddUnrestrictedPaths adds a list of directories where writing is allowed
// even if it would hit the real host filesystem (or transit through the host
// filesystem). This is intended to be used with certain well-known locations
// such as /tmp, $SNAP_DATA and $SNAP.
func (as *Assumptions) AddUnrestrictedPaths(paths ...string) {
	as.unrestrictedPaths = append(as.unrestrictedPaths, paths...)
}

// AddModeHint adds a path glob and mode used when creating path elements.
func (as *Assumptions) AddModeHint(pathGlob string, mode os.FileMode) {
	as.modeHints = append(as.modeHints, ModeHint{PathGlob: pathGlob, Mode: mode})
}

// ModeForPath returns the mode for creating a directory at a given path.
//
// The default mode is 0755 but AddModeHint calls can influence the mode at a
// specific path. When matching path elements, "*" does not match the directory
// separator. In effect it can only be used as a wildcard for a specific
// directory name. This constraint makes hints easier to model in practice.
//
// When multiple hints match the given path, ModeForPath panics.
func (as *Assumptions) ModeForPath(path string) os.FileMode {
	mode := os.FileMode(0755)
	var foundHint *ModeHint
	for _, hint := range as.modeHints {
		if ok, _ := filepath.Match(hint.PathGlob, path); ok {
			if foundHint == nil {
				mode = hint.Mode
				foundHint = &hint
			} else {
				panic(fmt.Errorf("cannot find unique mode for path %q: %q and %q both provide hints",
					path, foundHint.PathGlob, foundHint.PathGlob))
			}
		}
	}
	return mode
}

// isRestricted checks whether a path falls under restricted writing scheme.
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
//  1. The directory is in one of the explicitly permitted locations.
//     This is the strongest permission as it explicitly allows writing to
//     places that may show up on the host, one of the examples being $SNAP_DATA.
//  2. The directory is on a read-only filesystem.
//  3. The directory is on a tmpfs created by snapd.
func (as *Assumptions) canWriteToDirectory(dirFd int, dirName string) (bool, error) {
	if !as.isRestricted(dirName) {
		return true, nil
	}
	var fsData syscall.Statfs_t
	mylog.Check(sysFstatfs(dirFd, &fsData))

	var fileData syscall.Stat_t
	mylog.Check(sysFstat(dirFd, &fileData))

	// Writing to read only directories is allowed because EROFS is handled
	// by each of the writing helpers already.
	if ok := isReadOnly(dirName, &fsData); ok {
		return true, nil
	}
	// Writing to a trusted tmpfs is allowed because those are not leaking to
	// the host. Also, each time we find a good tmpfs we explicitly remember the device major/minor,
	if as.verifiedDevices[fileData.Dev] {
		return true, nil
	}
	if ok := isPrivateTmpfsCreatedBySnapd(dirName, &fsData, &fileData, as.pastChanges); ok {
		if as.verifiedDevices == nil {
			as.verifiedDevices = make(map[uint64]bool)
		}
		// Don't record 0:0 as those are all to easy to add in tests and would
		// skew tests using zero-initialized structures. Real device numbers
		// are not zero either so this is not a test-only conditional.
		if fileData.Dev != 0 {
			as.verifiedDevices[fileData.Dev] = true
		}
		return true, nil
	}
	// If writing is not not allowed by one of the three rules above then it is
	// disallowed.
	return false, nil
}

// RestrictionsFor computes restrictions for the desired path.
func (as *Assumptions) RestrictionsFor(desiredPath string) *Restrictions {
	// Writing to a restricted path results in step-by-step validation of each
	// directory, starting from the root of the file system. Unless writing is
	// allowed a mimic must be constructed to ensure that writes are not visible in
	// undesired locations of the host filesystem.
	if as.isRestricted(desiredPath) {
		return &Restrictions{assumptions: as, desiredPath: desiredPath, restricted: true}
	}
	return nil
}

// Restrictions contains meta-data of a compound write operation.
//
// This structure helps functions that write to the filesystem to keep track of
// the ultimate destination across several calls (e.g. the function that
// creates a file needs to call helpers to create subsequent directories).
// Keeping track of the desired path aids in constructing useful error
// messages.
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

// Check verifies whether writing to a directory would trespass on the host.
//
// The check is only performed in restricted mode. If the check fails a
// TrespassingError is returned.
func (rs *Restrictions) Check(dirFd int, dirName string) error {
	if rs == nil || !rs.restricted {
		return nil
	}
	// In restricted mode check the directory before attempting to write to it.
	ok := mylog.Check2(rs.assumptions.canWriteToDirectory(dirFd, dirName))
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
	logger.Debugf("trespassing violated %q while striving to %q", dirName, rs.desiredPath)
	logger.Debugf("restricted mode: %#v", rs.restricted)
	logger.Debugf("unrestricted paths: %q", rs.assumptions.unrestrictedPaths)
	logger.Debugf("verified devices: %v", rs.assumptions.verifiedDevices)
	logger.Debugf("past changes: %v", rs.assumptions.pastChanges)
	return &TrespassingError{ViolatedPath: filepath.Clean(dirName), DesiredPath: rs.desiredPath}
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

// isReadOnly checks whether the underlying filesystem is read only or is mounted as such.
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

// isPrivateTmpfsCreatedBySnapd checks whether a directory resides on a tmpfs mounted by snapd
//
// The function inspects the directory and a list of changes that were applied
// to the mount namespace. A directory is trusted if it is a tmpfs that was
// mounted by snap-confine or snapd-update-ns. Note that sub-directories of a
// trusted tmpfs are not considered trusted by this function.
func isPrivateTmpfsCreatedBySnapd(dirName string, fsData *syscall.Statfs_t, fileData *syscall.Stat_t, changes []*Change) bool {
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
			return change.Action == Mount || change.Action == Keep
		}
	}
	return false
}
