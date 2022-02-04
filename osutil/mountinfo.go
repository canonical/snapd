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

// MountInfoEntry contains data from /proc/$PID/mountinfo
//
// For details please refer to mountinfo documentation at
// https://www.kernel.org/doc/Documentation/filesystems/proc.txt
type MountInfoEntry struct {
	MountID        int
	ParentID       int
	DevMajor       int
	DevMinor       int
	Root           string
	MountDir       string
	MountOptions   map[string]string
	OptionalFields []string
	FsType         string
	MountSource    string
	SuperOptions   map[string]string
}
