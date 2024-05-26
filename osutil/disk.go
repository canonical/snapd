// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"fmt"
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/strutil"
)

var syscallStatfs = syscall.Statfs

type NotEnoughDiskSpaceError struct {
	Path  string
	Delta int64
}

func (e *NotEnoughDiskSpaceError) Error() string {
	return fmt.Sprintf("insufficient space in %q, at least %s more is required", e.Path, strutil.SizeToStr(e.Delta))
}

// diskFree returns free disk space for the given path
func diskFree(path string) (uint64, error) {
	var st syscall.Statfs_t
	mylog.Check(syscallStatfs(path, &st))

	// available blocks * block size
	return st.Bavail * uint64(st.Bsize), nil
}

// CheckFreeSpace checks if there is enough disk space for the given path
func CheckFreeSpace(path string, minSize uint64) error {
	free := mylog.Check2(diskFree(path))

	if free < minSize {
		delta := int64(minSize - free)
		return &NotEnoughDiskSpaceError{Path: path, Delta: delta}
	}
	return nil
}
