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

package osutil

import (
	"os"
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil/sys"
)

// UnlinkMany removes multiple files from a single directory.
//
// If dirname is not a directory, this will fail.
//
// This will abort at the first removal error (but ENOENT is ignored).
//
// Filenames must refer to files. They don't necessarily have to be
// relative paths to the given dirname, but if they aren't why are you
// using this function?
//
// Errors are *os.PathError, for convenience
func UnlinkMany(dirname string, filenames []string) error {
	dirfd := mylog.Check2(syscall.Open(dirname, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_DIRECTORY|sys.O_PATH, 0))

	defer syscall.Close(dirfd)

	return unlinkMany(dirfd, filenames)
}

func unlinkMany(dirfd int, filenames []string) error {
	for _, filename := range filenames {
		if mylog.Check(sysUnlinkat(dirfd, filename)); err != nil && err != syscall.ENOENT {
			return &os.PathError{
				Op:   "remove",
				Path: filename,
				Err:  err,
			}
		}
	}
	return nil
}

// UnlinkManyAt is like UnlinkMany but takes an open directory *os.File
// instead of a dirname.
func UnlinkManyAt(dir *os.File, filenames []string) error {
	return unlinkMany(int(dir.Fd()), filenames)
}
