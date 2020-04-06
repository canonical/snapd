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
	"path/filepath"
	"syscall"

	"github.com/snapcore/snapd/dirs"
)

func assignProcSelfMountInfo(newRootDir string) {
	ProcSelfMountInfo = filepath.Join(newRootDir, "/proc/self/mountinfo")
}

func init() {
	assignProcSelfMountInfo(dirs.GlobalRootDir)
	dirs.AddRootDirCallback(assignProcSelfMountInfo)
}

var (
	// ProcSelfMountInfo is a path to the mountinfo table of the current process.
	ProcSelfMountInfo string

	userLookup  = user.Lookup
	userCurrent = user.Current

	osReadlink = os.Readlink

	syscallKill    = syscall.Kill
	syscallGetpgid = syscall.Getpgid

	procSelfMountInfo = ProcSelfMountInfo
	etcFstab          = "/etc/fstab"
	sudoersDotD       = "/etc/sudoers.d"
)
