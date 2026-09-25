// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

func IsHomeUsingRemoteFS() (bool, error) {
	return isHomeUsingRemoteFS()
}

// SnapDirsUnderNFSMounts checks if there are any snap user data directories
// in NFS filesystems. The directories to check are described by dataHomeGlobs,
// a list of glob patterns matching snap data directories under user home
// directories.
func SnapDirsUnderNFSMounts(dataHomeGlobs []string) (bool, error) {
	return snapDirsUnderNFSMounts(dataHomeGlobs)
}

// MockIsHomeUsingRemoteFS mocks the real implementation of osutil.IsHomeUsingRemoteFS.
// This is exported so that other packages that indirectly interact with this
// functionality can mock IsHomeUsingRemoteFS.
func MockIsHomeUsingRemoteFS(new func() (bool, error)) (restore func()) {
	old := isHomeUsingRemoteFS
	isHomeUsingRemoteFS = new
	return func() {
		isHomeUsingRemoteFS = old
	}
}

func MockSnapDirsUnderNFSMounts(new func([]string) (bool, error)) (restore func()) {
	old := snapDirsUnderNFSMounts
	snapDirsUnderNFSMounts = new
	return func() {
		snapDirsUnderNFSMounts = old
	}
}
