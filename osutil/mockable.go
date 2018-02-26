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
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"syscall"
)

const (
	// ProcSelfMountInfo is a path to the mountinfo table of the current process.
	ProcSelfMountInfo = "/proc/self/mountinfo"
)

var (
	userLookup  = user.Lookup
	userCurrent = user.Current

	osReadlink = os.Readlink

	syscallKill    = syscall.Kill
	syscallGetpgid = syscall.Getpgid

	procSelfMountInfo = ProcSelfMountInfo
	etcFstab          = "/etc/fstab"
	sudoersDotD       = "/etc/sudoers.d"
)

//MockMountInfo mocks content of /proc/self/mountinfo read by IsHomeUsingNFS
func MockMountInfo(text string) (restore func()) {
	old := procSelfMountInfo
	f, err := ioutil.TempFile("", "mountinfo")
	if err != nil {
		panic(fmt.Errorf("cannot open temporary file: %s", err))
	}
	if err := ioutil.WriteFile(f.Name(), []byte(text), 0644); err != nil {
		panic(fmt.Errorf("cannot write mock mountinfo file: %s", err))
	}
	procSelfMountInfo = f.Name()
	return func() {
		os.Remove(procSelfMountInfo)
		procSelfMountInfo = old
	}
}

// MockEtcFstab mocks content of /etc/fstab read by IsHomeUsingNFS
func MockEtcFstab(text string) (restore func()) {
	old := etcFstab
	f, err := ioutil.TempFile("", "fstab")
	if err != nil {
		panic(fmt.Errorf("cannot open temporary file: %s", err))
	}
	if err := ioutil.WriteFile(f.Name(), []byte(text), 0644); err != nil {
		panic(fmt.Errorf("cannot write mock fstab file: %s", err))
	}
	etcFstab = f.Name()
	return func() {
		if etcFstab == "/etc/fstab" {
			panic("respectfully refusing to remove /etc/fstab")
		}
		os.Remove(etcFstab)
		etcFstab = old
	}
}
