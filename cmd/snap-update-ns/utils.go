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
	"fmt"
	"os"
	"path"
	"strings"
	"syscall"
)

// not available through syscall
const (
	UMOUNT_NOFOLLOW = 8
)

// For mocking everything during testing.
var (
	osLstat = os.Lstat

	sysClose   = syscall.Close
	sysMkdirat = syscall.Mkdirat
	sysMount   = syscall.Mount
	sysOpen    = syscall.Open
	sysOpenat  = syscall.Openat
	sysUnmount = syscall.Unmount
	sysFchown  = syscall.Fchown

	secureMkdirAll   = SecureMkdirAllImpl
	ensureMountPoint = EnsureMountPointImpl
)

// SecureMkdirAll is the secure variant of os.MkdirAll.
//
// Unlike the regular version this implementation does not follow any symbolic
// links. At all times the new directory segment is created using mkdirat(2)
// while holding an open file descriptor to the parent directory.
//
// The only handled error is mkdirat(2) that fails with EEXIST. All other
// errors are fatal but there is no attempt to undo anything that was created.
//
// The uid and gid are used for the fchown(2) system call which is performed
// after each segment is created and opened. The special value -1 may be used
// to request that ownership is not changed.
func SecureMkdirAllImpl(name string, perm os.FileMode, uid, gid int) error {
	// Declare var and don't assign-declare below to ensure we don't swallow
	// any errors by mistake.
	var err error
	var fd int

	const openFlags = syscall.O_NOFOLLOW | syscall.O_CLOEXEC | syscall.O_DIRECTORY

	if !path.IsAbs(name) {
		return fmt.Errorf("cannot create directory with relative path: %q", name)
	}
	// Open the root directory and start there.
	fd, err = sysOpen("/", openFlags, 0)
	if err != nil {
		return fmt.Errorf("cannot open root directory, %v", err)
	}

	// Split the path by entries and create each element using mkdirat() using
	// the parent directory as reference. Each time we open the newly created
	// segment using the O_NOFOLLOW and O_DIRECTORY flag so that symlink
	// attacks are impossible to carry out.
	segments := strings.Split(path.Clean(name), "/")
	for _, segment := range segments {
		if segment == "" {
			// Skip empty element corresponding to the leading slash.
			continue
		}
		made := true
		if err = sysMkdirat(fd, segment, uint32(perm)); err != nil {
			switch err {
			case syscall.EEXIST:
				made = false
			default:
				return fmt.Errorf("cannot mkdir path segment %q, %v", segment, err)
			}
		}
		previousFd := fd

		fd, err = sysOpenat(fd, segment, openFlags, 0)
		if err := sysClose(previousFd); err != nil {
			return fmt.Errorf("cannot close previous file descriptor, %v", err)
		}
		if err != nil {
			return fmt.Errorf("cannot open path segment %q, %v", segment, err)
		}
		if made {
			// Chown each segment that we made.
			if err := sysFchown(fd, uid, gid); err != nil {
				return fmt.Errorf("cannot chown path segment %q to %d.%d, %v", segment, uid, gid, err)
			}
		}

	}
	if err = sysClose(fd); err != nil {
		return fmt.Errorf("cannot close file descriptor, %v", err)
	}
	return nil
}

func EnsureMountPointImpl(path string, mode os.FileMode, uid int, gid int) error {
	// If the mount point is not present then create a directory in its
	// place.  This is very naive, doesn't handle read-only file systems
	// but it is a good starting point for people working with things like
	// $SNAP_DATA/subdirectory.
	//
	// We use lstat to ensure that we don't follow the symlink in case one
	// was set up by the snap. Note that at the time this is run, all the
	// snap's processes are frozen.
	fi, err := osLstat(path)
	switch {
	case err != nil && os.IsNotExist(err):
		return secureMkdirAll(path, mode, uid, gid)
	case err != nil:
		return fmt.Errorf("cannot inspect %q, %v", path, err)
	case err == nil:
		// Ensure that mount point is a directory.
		if !fi.IsDir() {
			return fmt.Errorf("cannot use %q for mounting, not a directory", path)
		}
	}
	return nil
}
