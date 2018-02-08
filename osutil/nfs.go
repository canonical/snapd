// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"strings"
)

var (
	procSelfMountInfo = ProcSelfMountInfo
	etcFstab          = "/etc/fstab"
)

// IsHomeUsingNFS returns true if NFS mounts are defined or mounted under /home.
//
// Internally /proc/self/mountinfo and /etc/fstab are interrogated (for current
// and possible mounted filesystems).  If either of those describes NFS
// filesystem mounted under or beneath /home/ then the return value is true.
func IsHomeUsingNFS() (bool, error) {
	mountinfo, err := LoadMountInfo(procSelfMountInfo)
	if err != nil {
		return false, fmt.Errorf("cannot parse %s: %s", procSelfMountInfo, err)
	}
	for _, entry := range mountinfo {
		if (entry.FsType == "nfs4" || entry.FsType == "nfs") && (strings.HasPrefix(entry.MountDir, "/home/") || entry.MountDir == "/home") {
			return true, nil
		}
	}
	fstab, err := LoadMountProfile(etcFstab)
	if err != nil {
		return false, fmt.Errorf("cannot parse %s: %s", etcFstab, err)
	}
	for _, entry := range fstab.Entries {
		if (entry.Type == "nfs4" || entry.Type == "nfs") && (strings.HasPrefix(entry.Dir, "/home/") || entry.Dir == "/home") {
			return true, nil
		}
	}
	return false, nil
}

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
