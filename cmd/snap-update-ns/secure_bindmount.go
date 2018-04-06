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
	"os"
	"syscall"
)

// BindMount performs a bind mount between two absolute paths containing no
// symlinks, using a private stash directory as an intermediate step.
//
// Since this function uses chdir() internally, it should not be called in
// parallel with code that depends on the current working directory.
func (sec *Secure) BindMount(sourceDir, targetDir string, flags uint, stashDir string) error {
	// The kernel doesn't support recursively switching a tree of
	// bind mounts to read only, and we haven't written a work
	// around.
	if flags&syscall.MS_RDONLY != 0 && flags&syscall.MS_REC != 0 {
		return fmt.Errorf("cannot use MS_RDONLY and MS_REC together")
	}

	// Save current directory, since we use chdir internally
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(cwd)

	// Step 1: acquire file descriptors representing the source
	// and destination directories, ensuring no symlinks are
	// followed.
	sourceFd, err := sec.OpenPath(sourceDir)
	if err != nil {
		return err
	}
	defer sysClose(sourceFd)
	targetFd, err := sec.OpenPath(targetDir)
	if err != nil {
		return err
	}
	defer sysClose(targetFd)

	// Step 2: chdir to the source, and bind mount "." to the stash dir
	bindFlags := syscall.MS_BIND | (flags & syscall.MS_REC)
	if err := sysFchdir(sourceFd); err != nil {
		return err
	}
	if err := sysMount(".", stashDir, "", uintptr(bindFlags), ""); err != nil {
		return err
	}
	defer sysUnmount(stashDir, syscall.MNT_DETACH|umountNoFollow)

	// Step 3: optionally change to readonly
	if flags&syscall.MS_RDONLY != 0 {
		remountFlags := syscall.MS_REMOUNT | syscall.MS_BIND | syscall.MS_RDONLY
		if flags&syscall.MS_REC != 0 {
			remountFlags |= syscall.MS_REC
		}
		if err := sysMount("none", stashDir, "", uintptr(remountFlags), ""); err != nil {
			return err
		}
	}

	// Step 4: chdir to the destination, and move mount the stash to "."
	if err := sysFchdir(targetFd); err != nil {
		return err
	}
	// Ideally this would be a move rather than a second bind, but
	// that fails for shared mount namespaces
	if err := sysMount(stashDir, ".", "", uintptr(bindFlags), ""); err != nil {
		return err
	}

	return nil
}
