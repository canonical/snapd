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

package libmount

import (
	"fmt"
	"sort"

	// Syscall package does not have all the constants.
	"golang.org/x/sys/unix"
)

// mountOption binds libmount mount option name to kernel mount flags.
type mountOption struct {
	name      string
	setMask   uint32 // Mount bits set by this option
	clearMask uint32 // Mount bits cleared by this option.
	alias     bool   // This entry is an alias and is not the canonical name.
}

// mountOptions and the bits they set or clear.
// This table comes from https://gitlab.com/apparmor/apparmor/-/blob/master/parser/mount.cc?ref_type=heads
// but the ultimate authority is the kernel since those are all kernel flags with userspace names.
var mountOptions = []mountOption{
	// The read-only vs read-write flag is represented by one bit. Presence of
	// the bit indicates that the read-only mode is in effect. This is why "rw"
	// has zero as the set mask and read-only bit as the clear mask.
	{name: "ro", setMask: unix.MS_RDONLY},
	{name: "r", setMask: unix.MS_RDONLY, alias: true},
	{name: "read-only", setMask: unix.MS_RDONLY, alias: true},
	{name: "rw", clearMask: unix.MS_RDONLY},
	{name: "w", clearMask: unix.MS_RDONLY, alias: true},
	{name: "suid", clearMask: unix.MS_NOSUID},
	{name: "nosuid", setMask: unix.MS_NOSUID},
	{name: "dev", clearMask: unix.MS_NODEV},
	{name: "nodev", setMask: unix.MS_NODEV},
	{name: "exec", clearMask: unix.MS_NOEXEC},
	{name: "noexec", setMask: unix.MS_NOEXEC},
	{name: "sync", setMask: unix.MS_SYNCHRONOUS}, // NOTE: MS_SYNC is for msync(2), not mount(2).
	{name: "async", clearMask: unix.MS_SYNCHRONOUS},
	{name: "remount", setMask: unix.MS_REMOUNT},
	{name: "mand", setMask: unix.MS_MANDLOCK},
	{name: "nomand", clearMask: unix.MS_MANDLOCK},
	{name: "dirsync", setMask: unix.MS_DIRSYNC},
	{name: "symfollow", clearMask: unix.MS_NOSYMFOLLOW},
	{name: "nosymfollow", setMask: unix.MS_NOSYMFOLLOW},
	{name: "atime", clearMask: unix.MS_NOATIME},
	{name: "noatime", setMask: unix.MS_NOATIME},
	{name: "diratime", clearMask: unix.MS_NODIRATIME},
	{name: "nodiratime", setMask: unix.MS_NODIRATIME},
	{name: "bind", setMask: unix.MS_BIND},
	{name: "B", setMask: unix.MS_BIND, alias: true},
	{name: "move", setMask: unix.MS_MOVE},
	{name: "M", setMask: unix.MS_MOVE, alias: true},
	{name: "rbind", setMask: unix.MS_BIND | unix.MS_REC},
	{name: "R", setMask: unix.MS_BIND | unix.MS_REC, alias: true},
	{name: "verbose", setMask: unix.MS_VERBOSE},
	{name: "silent", setMask: unix.MS_SILENT},
	{name: "loud", clearMask: unix.MS_SILENT},
	{name: "acl", setMask: unix.MS_POSIXACL},
	{name: "noacl", clearMask: unix.MS_POSIXACL},
	{name: "unbindable", setMask: unix.MS_UNBINDABLE},
	{name: "make-unbindable", setMask: unix.MS_UNBINDABLE, alias: true},
	{name: "runbindable", setMask: unix.MS_UNBINDABLE | unix.MS_REC},
	{name: "make-runbindable", setMask: unix.MS_UNBINDABLE | unix.MS_REC, alias: true},
	{name: "private", setMask: unix.MS_PRIVATE},
	{name: "make-private", setMask: unix.MS_PRIVATE, alias: true},
	{name: "rprivate", setMask: unix.MS_PRIVATE | unix.MS_REC},
	{name: "make-rprivate", setMask: unix.MS_PRIVATE | unix.MS_REC, alias: true},
	{name: "slave", setMask: unix.MS_SLAVE},
	{name: "make-slave", setMask: unix.MS_SLAVE, alias: true},
	{name: "rslave", setMask: unix.MS_SLAVE | unix.MS_REC},
	{name: "make-rslave", setMask: unix.MS_SLAVE | unix.MS_REC},
	{name: "shared", setMask: unix.MS_SHARED},
	{name: "make-shared", setMask: unix.MS_SHARED},
	{name: "rshared", setMask: unix.MS_SHARED | unix.MS_REC},
	{name: "make-rshared", setMask: unix.MS_SHARED | unix.MS_REC},
	{name: "relatime", setMask: unix.MS_RELATIME},
	{name: "norelatime", clearMask: unix.MS_RELATIME},
	{name: "iversion", setMask: unix.MS_I_VERSION},
	{name: "noiversion", clearMask: unix.MS_I_VERSION},
	{name: "strictatime", setMask: unix.MS_STRICTATIME},
	{name: "nostrictatime", clearMask: unix.MS_STRICTATIME},
	{name: "lazytime", setMask: unix.MS_LAZYTIME},
	{name: "nolazytime", clearMask: unix.MS_LAZYTIME},
	{name: "user", clearMask: xMS_NOUSER},
	{name: "nouser", setMask: xMS_NOUSER},
}

// xMS_NOUSER unix.MS_NOUSER which is missing.
const xMS_NOUSER = 1 << 31

func init() {
	// Sort the slice of mount options by name.
	sort.Slice(mountOptions, func(i, j int) bool {
		return mountOptions[i].name < mountOptions[j].name
	})
}

// findMountOption finds a mount option with the given name.
func findMountOption(name string) (mountOption, bool) {
	i := sort.Search(len(mountOptions), func(i int) bool {
		return mountOptions[i].name >= name
	})
	if i < len(mountOptions) && mountOptions[i].name == name {
		return mountOptions[i], true
	}

	return mountOption{}, false
}

// findMountOptionConflict finds option conflicting with prior set and clear masks.
//
// The result is the first mount option with a canonical name, where the set
// mask or clear mask of the given and returned option are in conflict.
func findMountOptionConflict(opt mountOption, priorSetMask, priorClearMask uint32) (mountOption, bool) {
	if opt.setMask&priorClearMask != 0 || opt.clearMask&priorSetMask != 0 {
		// We now know that at least one option has a conflicting set or clear
		// mask. Given that there are only a handful of mount options, an
		// amount that should easily fit into the cache of even the smallest
		// CPU, this is a simple linear scan without anything more fancy.
		for _, other := range mountOptions {
			// Do not use aliases when providing conflict details. This makes
			// us talk about ro conflicting with rw instead of ro conflicting
			// with read-write.
			if other.alias {
				continue
			}

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
// be called with file-system specific options. Only one error is returned at a
// time. This limitation may be lifted later.
func ValidateMountOptions(opts ...string) error {
	var priorSetMask, priorClearMask uint32

	for _, name := range opts {
		opt, ok := findMountOption(name)
		if !ok {
			return fmt.Errorf("option %s is unknown", name)
		}

		if prior, ok := findMountOptionConflict(opt, priorSetMask, priorClearMask); ok {
			return fmt.Errorf("option %s conflicts with %s", opt.name, prior.name)
		}

		priorSetMask |= opt.setMask
		priorClearMask |= opt.clearMask
	}

	// TODO:GOVERSION: use errors.Join when we update to go 1.20.
	return nil
}
