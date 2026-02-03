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
	"path/filepath"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/dirs"
)

var etcFstab = "/etc/fstab"

// isHomeUsingRemoteFS informs if remote filesystems are defined or mounted under /home.
//
// Internally /proc/self/mountinfo and /etc/fstab are interrogated (for current
// and possible mounted filesystems). If either of those describes NFS
// filesystem mounted under or beneath /home/ then the return value is true.
var isHomeUsingRemoteFS = func() (bool, error) {
	mountinfo, err := LoadMountInfo()
	if err != nil {
		return false, fmt.Errorf("cannot parse mountinfo: %s", err)
	}
	for _, entry := range mountinfo {
		switch entry.FsType {
		case "nfs4", "nfs", "autofs", "cifs":
			if strings.HasPrefix(entry.MountDir, "/home/") || entry.MountDir == "/home" {
				return true, nil
			}
		}
	}
	fstab, err := LoadMountProfile(etcFstab)
	if err != nil {
		return false, fmt.Errorf("cannot parse %s: %s", etcFstab, err)
	}
	for _, entry := range fstab.Entries {
		switch entry.Type {
		case "nfs4", "nfs", "autofs", "cifs":
			if strings.HasPrefix(entry.Dir, "/home/") || entry.Dir == "/home" {
				return true, nil
			}
		}
	}
	return false, nil
}

var dirsAllDataHomeGlobs = dirs.AllDataHomeGlobs

// snapDirsUnderNFSMounts checks if there are any snap user data directories
// in NFS filesystems.
var snapDirsUnderNFSMounts = func() (bool, error) {
	var ds []string
	for _, entry := range dirsAllDataHomeGlobs() {
		entryPaths, err := filepath.Glob(entry)
		if err != nil {
			return false, err
		}
		ds = append(ds, entryPaths...)
	}

	const (
		NFS = 0x6969
	)

	for _, d := range ds {
		var info syscall.Statfs_t
		err := syscallStatfs(d, &info)
		if err != nil {
			return false, err
		}

		if info.Type == NFS {
			return true, nil
		}
	}

	return false, nil
}
