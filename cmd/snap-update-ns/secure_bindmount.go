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
	"path/filepath"
	"syscall"

	"github.com/snapcore/snapd/osutil/sys"
)

// openNoFollow creates a path file descriptor for the given
// directory, making sure no components are symbolic links
func openNoFollow(path string) (int, error) {
	if !filepath.IsAbs(path) {
		return -1, fmt.Errorf("path %v is not absolute", path)
	}
	segments, err := splitIntoSegments(path)
	if err != nil {
		return -1, err
	}
	// We use the following flags to open:
	//  O_PATH: we don't intend to use the fd for IO
	//  O_NOFOLLOW: don't follow symlinks
	//  O_DIRECTORY: we expect to find directories
	flags := sys.O_PATH | syscall.O_NOFOLLOW | syscall.O_DIRECTORY
	parentFd, err := sysOpen("/", flags, 0)
	if err != nil {
		return -1, err
	}
	for _, segment := range segments {
		fd, err := sysOpenat(parentFd, segment, flags, 0)
		if err != nil {
			sysClose(parentFd)
			return -1, err
		}
		sysClose(parentFd)
		parentFd = fd
	}
	return parentFd, nil
}

// SecureBindMount performs a bind mount between two absolute paths
// containing no symlinks, using a private stash directory as an
// intermediate step
func SecureBindMount(sourceDir, targetDir string, flags uint, stashDir string) error {
	// Save source directory, since we use chdir internally
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(cwd)

	// Step 1: acquire file descriptors representing the source
	// and destination directories, ensuring no symlinks are
	// followed.
	sourceFd, err := openNoFollow(sourceDir)
	if err != nil {
		return err
	}
	defer sysClose(sourceFd)
	targetFd, err := openNoFollow(targetDir)
	if err != nil {
		return err
	}
	defer sysClose(targetFd)

	// Step 2: chdir to the source, and bind mount "." to the stash dir
	bindFlags := syscall.MS_BIND
	if flags & syscall.MS_REC != 0 {
		bindFlags |= syscall.MS_REC
	}
	if err := sysFchdir(sourceFd); err != nil {
		return err
	}
	if err := sysMount(".", stashDir, "", uintptr(bindFlags), ""); err != nil {
		return err
	}
	defer sysUnmount(stashDir, syscall.MNT_DETACH);

	// Step 3: optionally change to readonly
	if flags & syscall.MS_RDONLY != 0 {
		remountFlags := syscall.MS_REMOUNT | syscall.MS_BIND | syscall.MS_RDONLY
		if flags & syscall.MS_REC != 0 {
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
