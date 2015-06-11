// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package partition

import (
	"fmt"
	"sort"
)

// MountOption represents how the partition should be mounted, currently
// RO (read-only) and RW (read-write) are supported
type MountOption int

const (
	// RO mounts the partition read-only
	RO MountOption = iota
	// RW mounts the partition read-only
	RW
)

// mountEntry represents a mount this package has created.
type mountEntry struct {
	source string
	target string

	options string

	// true if target refers to a bind mount. We could derive this
	// from options, but this field saves the effort.
	bindMount bool
}

// mountEntryArray represents an array of mountEntry objects.
type mountEntryArray []mountEntry

// current mounts that this package has created.
var mounts mountEntryArray

// Len is part of the sort interface, required to allow sort to work
// with an array of Mount objects.
func (mounts mountEntryArray) Len() int {
	return len(mounts)
}

// Less is part of the sort interface, required to allow sort to work
// with an array of Mount objects.
func (mounts mountEntryArray) Less(i, j int) bool {
	return mounts[i].target < mounts[j].target
}

// Swap is part of the sort interface, required to allow sort to work
// with an array of Mount objects.
func (mounts mountEntryArray) Swap(i, j int) {
	mounts[i], mounts[j] = mounts[j], mounts[i]
}

// removeMountByTarget removes the Mount specified by the target from
// the global mounts array.
func removeMountByTarget(mnts mountEntryArray, target string) (results mountEntryArray) {

	for _, m := range mnts {
		if m.target != target {
			results = append(results, m)
		}
	}

	return results
}

// undoMounts unmounts all mounts this package has mounted optionally
// only unmounting bind mounts and leaving all remaining mounts.
func undoMounts(bindMountsOnly bool) error {

	mountsCopy := make(mountEntryArray, len(mounts), cap(mounts))
	copy(mountsCopy, mounts)

	// reverse sort to ensure unmounts are handled in the correct
	// order.
	sort.Sort(sort.Reverse(mountsCopy))

	// Iterate backwards since we want a reverse-sorted list of
	// mounts to ensure we can unmount in order.
	for _, mount := range mountsCopy {
		if bindMountsOnly && !mount.bindMount {
			continue
		}

		if err := unmountAndRemoveFromGlobalMountList(mount.target); err != nil {
			return err
		}
	}

	return nil
}

// FIXME: use syscall.Mount() here
func mount(source, target, options string) (err error) {
	var args []string

	args = append(args, "/bin/mount")
	if options != "" {
		args = append(args, fmt.Sprintf("-o%s", options))
	}

	args = append(args, source)
	args = append(args, target)

	return runCommand(args...)
}

// Mount the given directory and add it to the global mounts slice
func mountAndAddToGlobalMountList(m mountEntry) (err error) {

	err = mount(m.source, m.target, m.options)
	if err == nil {
		mounts = append(mounts, m)
	}

	return err
}

// Unmount the given directory and remove it from the global "mounts" slice
func unmountAndRemoveFromGlobalMountList(target string) (err error) {
	err = runCommand("/bin/umount", target)
	if err != nil {
		return err

	}

	results := removeMountByTarget(mounts, target)

	// Update global
	mounts = results

	return nil
}
