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
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"syscall"
)

// MockProcSelfMountInfo is meant for tests, where dirs.GlobalRootDir != ""
// and just mocks with the content
func MockProcSelfMountInfo(path, content string) error {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(path, []byte(content), 0644)
}

var (
	userLookup  = user.Lookup
	userCurrent = user.Current

	osReadlink = os.Readlink

	syscallKill    = syscall.Kill
	syscallGetpgid = syscall.Getpgid

	etcFstab    = "/etc/fstab"
	sudoersDotD = "/etc/sudoers.d"
)
