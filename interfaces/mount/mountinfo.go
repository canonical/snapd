// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package mount

import (
	"fmt"
	"strconv"
	"strings"
)

// InfoEntry contains data from /proc/$PID/mountinfo
//
// For details please refer to mountinfo documentation at
// https://www.kernel.org/doc/Documentation/filesystems/proc.txt
type InfoEntry struct {
	MountID      int
	ParentID     int
	DevMajor     int
	DevMinor     int
	Root         string
	MountDir     string
	MountOpts    string
	OptionalFlds string
	FsType       string
	MountSource  string
	SuperOpts    string
}

// ParseInfoEntry parses a single line of /proc/$PID/mountinfo file.
func ParseInfoEntry(s string) (InfoEntry, error) {
	var e InfoEntry
	var err error
	fields := strings.Fields(s)
	// The format is variable-length, but at least 10 fields are mandatory.
	// The (7) below is a list of optional field which is terminated with (8).
	// 36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue
	// (1)(2)(3)   (4)   (5)      (6)      (7)   (8) (9)   (10)         (11)
	if len(fields) < 10 {
		return e, fmt.Errorf("incorrect number of fields, expected at least 10 but found %d", len(fields))
	}
	// Parse MountID (decimal number).
	e.MountID, err = strconv.Atoi(fields[0])
	if err != nil {
		return e, fmt.Errorf("cannot parse mount ID: %q", fields[0])
	}
	// Parse ParentID (decimal number).
	e.ParentID, err = strconv.Atoi(fields[1])
	if err != nil {
		return e, fmt.Errorf("cannot parse parent mount ID: %q", fields[1])
	}
	// Parses DevMajor:DevMinor pair (decimal numbers separated by colon).
	subFields := strings.FieldsFunc(fields[2], func(r rune) bool { return r == ':' })
	if len(subFields) != 2 {
		return e, fmt.Errorf("cannot parse device major:minor number pair: %q", fields[2])
	}
	e.DevMajor, err = strconv.Atoi(subFields[0])
	if err != nil {
		return e, fmt.Errorf("cannot parse device major number: %q", subFields[0])
	}
	e.DevMinor, err = strconv.Atoi(subFields[1])
	if err != nil {
		return e, fmt.Errorf("cannot parse device minor number: %q", subFields[1])
	}
	// NOTE: All string fields use the same escape/unescape logic as fstab files.
	// Parse Root, MountDir and MountOpts fields.
	e.Root = unescape(fields[3])
	e.MountDir = unescape(fields[4])
	e.MountOpts = unescape(fields[5])
	// Optional fields are terminated with a "-" value and start
	// after the mount options field. Skip ahead until we see the "-"
	// marker.
	var i int
	for i = 6; i < len(fields) && fields[i] != "-"; i++ {
	}
	if i == len(fields) {
		return e, fmt.Errorf("list of optional fields is not terminated properly")
	}
	e.OptionalFlds = strings.Join(fields[6:i], " ")
	// Parse the last three fixed fields.
	tailFields := fields[i+1:]
	if len(tailFields) != 3 {
		return e, fmt.Errorf("incorrect number of tail fields, expected 3 but found %d", len(tailFields))
	}
	e.FsType = unescape(tailFields[0])
	e.MountSource = unescape(tailFields[1])
	e.SuperOpts = unescape(tailFields[2])
	return e, nil
}
