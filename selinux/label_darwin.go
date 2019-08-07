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
package selinux

// VerifyPathContext checks whether a given path is labeled according to its default
// SELinux context
func VerifyPathContext(aPath string) (bool, error) {
	return true, nil
}

// RestoreContext restores the default SELinux context of given path
func RestoreContext(aPath string, mode RestoreMode) error {
	return nil
}

// SnapMountContext finds out the right context for mounting snaps
func SnapMountContext() string {
	return ""
}
