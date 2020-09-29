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
	"os/user"
	"syscall"
)

// MockMountInfo is meant for tests to mock the content of /proc/self/mountinfo.
func MockMountInfo(content string) (restore func()) {
	old := mockedMountInfo
	mockedMountInfo = &content
	return func() {
		mockedMountInfo = old
	}
}

func MockFindUid(f func(string) (uint64, error)) (restore func()) {
	old := FindUid
	FindUid = f
	return func() {
		FindUid = old
	}
}

func MockFindGid(f func(string) (uint64, error)) (restore func()) {
	old := FindGid
	FindGid = f
	return func() {
		FindGid = old
	}
}

var (
	mockedMountInfo *string

	userLookup  = user.Lookup
	userCurrent = user.Current

	osReadlink = os.Readlink

	syscallKill    = syscall.Kill
	syscallGetpgid = syscall.Getpgid

	etcFstab    = "/etc/fstab"
	sudoersDotD = "/etc/sudoers.d"

	procSelfMountInfo = "/proc/self/mountinfo"
)
