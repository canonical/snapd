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

	"github.com/ddkwork/golibrary/mylog"
)

var etcFstab = "/etc/fstab"

// isHomeUsingRemoteFS informs if remote filesystems are defined or mounted under /home.
//
// Internally /proc/self/mountinfo and /etc/fstab are interrogated (for current
// and possible mounted filesystems). If either of those describes NFS
// filesystem mounted under or beneath /home/ then the return value is true.
var isHomeUsingRemoteFS = func() (bool, error) {
	mountinfo := mylog.Check2(LoadMountInfo())

	for _, entry := range mountinfo {
		switch entry.FsType {
		case "nfs4", "nfs", "autofs", "cifs":
			if strings.HasPrefix(entry.MountDir, "/home/") || entry.MountDir == "/home" {
				return true, nil
			}
		}
	}
	fstab := mylog.Check2(LoadMountProfile(etcFstab))

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
