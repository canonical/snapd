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
	"strings"
)

// Entry describes an /etc/fstab-like mount entry.
//
// Fields are named after names in struct returned by getmntent(3).
//
// struct mntent {
//     char *mnt_fsname;   /* name of mounted filesystem */
//     char *mnt_dir;      /* filesystem path prefix */
//     char *mnt_type;     /* mount type (see Mntent.h) */
//     char *mnt_opts;     /* mount options (see Mntent.h) */
//     int   mnt_freq;     /* dump frequency in days */
//     int   mnt_passno;   /* pass number on parallel fsck */
// };
type Entry struct {
	Name    string
	Dir     string
	Type    string
	Options MntOptions

	DumpFrequency   int
	CheckPassNumber int
}

// MntOptions represents mount options in a mount entry.
type MntOptions []string

func (v MntOptions) String() string {
	if len(v) != 0 {
		return escape(strings.Join(v, ","))
	}
	return "defaults"
}

// escape replaces whitespace characters so that getmntent can parse it correctly.
//
// According to the manual page, the following characters need to be escaped.
//  space     => (\040)
//  tab       => (\011)
//  newline   => (\012)
//  backslash => (\134)
func escape(s string) string {
	return whitespaceReplacer.Replace(s)
}

var whitespaceReplacer = strings.NewReplacer(
	" ", `\040`, "\t", `\011`, "\n", `\012`, "\\", `\134`)

func (e Entry) String() string {
	// Name represents name of the device in a mount entry.
	var name string
	if len(e.Name) != 0 {
		name = escape(e.Name)
	} else {
		name = "none"
	}
	var dir string
	// Dir represents mount directory in a mount entry.
	if len(e.Dir) != 0 {
		dir = escape(e.Dir)
	} else {
		dir = "none"
	}
	// Type represents file system type in a mount entry.
	var fsType string
	if len(e.Type) != 0 {
		fsType = escape(e.Type)
	} else {
		fsType = "none"
	}
	return fmt.Sprintf("%s %s %s %s %d %d",
		name, dir, fsType, e.Options, e.DumpFrequency, e.CheckPassNumber)
}
