// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"syscall"
)

// BindMount performs a bind mount between two absolute paths containing no
// symlinks.
func (sec *Secure) BindMount(sourceDir, targetDir string, flags uint) error {
	// This function only attempts to handle bind mounts. Expanding to other
	// mounts will require examining do_mount() from fs/namespace.c of the
	// kernel that called functions (eventually) verify `DCACHE_CANT_MOUNT` is
	// not set (eg, by calling lock_mount()).
	if flags&syscall.MS_BIND == 0 {
		return fmt.Errorf("cannot perform non-bind mount operation")
	}

	// The kernel doesn't support recursively switching a tree of bind mounts
	// to read only, and we haven't written a work around.
	if flags&syscall.MS_RDONLY != 0 && flags&syscall.MS_REC != 0 {
		return fmt.Errorf("cannot use MS_RDONLY and MS_REC together")
	}

	// Step 1: acquire file descriptors representing the source and destination
	// directories, ensuring no symlinks are followed.
	sourceFd, err := OpenPath(sourceDir)
	if err != nil {
		return err
	}
	defer sysClose(sourceFd)
	targetFd, err := OpenPath(targetDir)
	if err != nil {
		return err
	}
	defer sysClose(targetFd)

	// Step 2: perform a bind mount between the paths identified by the two
	// file descriptors. We primarily care about privilege escalation here and
	// trying to race the sysMount() by removing any part of the dir (sourceDir
	// or targetDir) after we have an open file descriptor to it (sourceFd or
	// targetFd) to then replace an element of the dir's path with a symlink
	// will cause the fd path (ie, sourceFdPath or targetFdPath) to be marked
	// as unmountable within the kernel (this path is also changed to show as
	// '(deleted)'). Alternatively, simply renaming the dir (sourceDir or
	// targetDir) after we have an open file descriptor to it (sourceFd or
	// targetFd) causes the mount to happen with the newly renamed path, but
	// this rename is controlled by DAC so while the user could race the mount
	// source or target, this rename can't be used to gain privileged access to
	// files. For systems with AppArmor enabled, this raced rename would be
	// denied by the per-snap snap-update-ns AppArmor profle.
	sourceFdPath := fmt.Sprintf("/proc/self/fd/%d", sourceFd)
	targetFdPath := fmt.Sprintf("/proc/self/fd/%d", targetFd)
	bindFlags := syscall.MS_BIND | (flags & syscall.MS_REC)
	if err := sysMount(sourceFdPath, targetFdPath, "", uintptr(bindFlags), ""); err != nil {
		return err
	}

	// Step 3: optionally change to readonly
	if flags&syscall.MS_RDONLY != 0 {
		// We need to look up the target directory a second time, because
		// targetFd refers to the path shadowed by the mount point.
		mountFd, err := OpenPath(targetDir)
		if err != nil {
			// FIXME: the mount occurred, but the user moved the target
			// somewhere
			return err
		}
		defer sysClose(mountFd)
		mountFdPath := fmt.Sprintf("/proc/self/fd/%d", mountFd)
		remountFlags := syscall.MS_REMOUNT | syscall.MS_BIND | syscall.MS_RDONLY
		if err := sysMount("none", mountFdPath, "", uintptr(remountFlags), ""); err != nil {
			sysUnmount(mountFdPath, syscall.MNT_DETACH|umountNoFollow)
			return err
		}
	}
	return nil
}
