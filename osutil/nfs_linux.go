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
	"strings"
)

var etcFstab = "/etc/fstab"

// isHomeUsingRemoteFS informs if remote filesystems are defined or mounted under /home.
//
// This returns true of SNAPD_HOME_REMOTE_FS is set to "1" or other values
// recognized as true by strconv.ParseBool. If that value is unset then
// /proc/self/mountinfo and /etc/fstab are interrogated (for current and
// possible mounted filesystems). If either of those describes NFS or CIFS
// filesystem mounted under or beneath /home/ then the return value is true.
var isHomeUsingRemoteFS = func() (bool, error) {
	// This case allows us to have a way to tell snapd that /home is going to
	// be remote but the mount operation happens inside a non-trivial
	// component, such as deep in pam_mount, without having to arrange snapd to
	// be restarte after that mount finishes.
	if GetenvBool("SNAPD_HOME_REMOTE_FS") {
		return true, nil
	}

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
