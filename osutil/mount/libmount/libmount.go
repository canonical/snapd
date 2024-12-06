// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

// Package libmount contains validation logic for user-level mount options
// typically handled by libmount. This is in contrast with the parent mount
// package which deals with the low-level kernel interface.
package libmount

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"syscall"
)

// mountOption binds libmount mount option name to kernel mount flags.
type mountOption struct {
	name      string
	setMask   uint32 // Mount bits set by this option
	clearMask uint32 // Mount bits cleared by this option.
	canonical bool   // Canonical spelling of the mount option (arbitrary).
}

var mountOptions = []mountOption{
	// The read-only vs read-write flag is represented by one bit. Presence of
	// the bit indicates that the read-only mode is in effect. This is why "rw"
	// has zero as the set mask and read-only bit as the clear mask.
	{name: "ro", setMask: syscall.MS_RDONLY, canonical: true},
	{name: "r", setMask: syscall.MS_RDONLY},
	{name: "read-only", setMask: syscall.MS_RDONLY},
	{name: "rw", clearMask: syscall.MS_RDONLY, canonical: true},
	{name: "w", clearMask: syscall.MS_RDONLY},
	// TODO: add remaining mount options.
}

// canonicalMountOptions contains all the options that with a canonical name.
var canonicalMountOptions []mountOption

func init() {
	// Sort the slice of mount options by name.
	slices.SortFunc(mountOptions, func(a, b mountOption) int {
		return strings.Compare(a.name, b.name)
	})

	// Fill the slice of canonical mount options.
	var numCanonical int
	for i := range mountOptions {
		if mountOptions[i].canonical {
			numCanonical++
		}
	}

	canonicalMountOptions = make([]mountOption, 0, numCanonical)

	for i := range mountOptions {
		if mountOptions[i].canonical {
			canonicalMountOptions = append(canonicalMountOptions, mountOptions[i])
		}
	}
}

// findMountOption finds a mount option with the given name.
func findMountOption(name string) (mountOption, bool) {
	i, ok := sort.Find(len(mountOptions), func(i int) int {
		return strings.Compare(name, mountOptions[i].name)
	})

	if ok {
		return mountOptions[i], true
	}

	return mountOption{}, false
}

// findCanonicalMountOptionConflict finds option conflicting with prior set and clear masks.
//
// The result is the first mount option with a canonical name, where the set
// mask or clear mask of the given and returned option are in conflict.
func findCanonicalMountOptionConflict(opt mountOption, priorSetMask, priorClearMask uint32) (mountOption, bool) {
	if opt.setMask&priorClearMask != 0 || opt.clearMask&priorSetMask != 0 {
		// We now know that at least one option has a conflicting set or clear
		// mask. Given that there are only a handful of mount options, an
		// amount that should easily fit into the cache of even the smallest
		// CPU, this is a simple linear scan without anything more fancy.
		for _, other := range canonicalMountOptions {
			if opt.setMask&other.clearMask != 0 || opt.clearMask&other.setMask != 0 {
				return other, true
			}
		}
	}

	return mountOption{}, false
}

// ValidateMountOptions looks for unknown or conflicting options for libmount-style APIs.
//
// The returned error describes all the problems in the given slice of mount
// options, including:
//
// - unknown options
// - options that conflict with other options given earlier.
//
// Note that only well-known options are recognized. This function should not
// be called with file-system specific options.
func ValidateMountOptions(opts ...string) error {
	var errs []error
	var priorSetMask, priorClearMask uint32

	for _, name := range opts {
		opt, ok := findMountOption(name)
		if !ok {
			errs = append(errs, fmt.Errorf("option %s is unknown", name))
			continue
		}

		if prior, ok := findCanonicalMountOptionConflict(opt, priorSetMask, priorClearMask); ok {
			errs = append(errs, fmt.Errorf("option %s conflicts with %s", opt.name, prior.name))
		}

		priorSetMask |= opt.setMask
		priorClearMask |= opt.clearMask
	}

	return errors.Join(errs...)
}
