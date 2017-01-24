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

// Snippet describes mount entries that an interface wishes to create.
type Snippet struct {
	MountEntries []Entry `json:"mount-entries,omitempty"`
}

// MountEntry describes an /etc/fstab-like mount entry.
//
// Fields are named after names in struct returned by getmntent(3).
//
// struct mntent {
//     char *mnt_fsname;   /* name of mounted filesystem */
//     char *mnt_dir;      /* filesystem path prefix */
//     char *mnt_type;     /* mount type (see mntent.h) */
//     char *mnt_opts;     /* mount options (see mntent.h) */
//     int   mnt_freq;     /* dump frequency in days */
//     int   mnt_passno;   /* pass number on parallel fsck */
// };
type Entry struct {
	FsName  mntFsName  `json:"fs-name,omitempty"`
	Dir     mntDir     `json:"dir,omitempty"`
	FsType  mntFsType  `json:"fs-type,omitempty"`
	Options mntOptions `json:"options,omitempty"`
	Freq    int        `json:"frequency,omitempty"`
	PassNum int        `json:"pass-number,omitempty"`
}

// mntFsName represents name of the device in a mount entry.
type mntFsName string

func (v mntFsName) String() string {
	if len(v) != 0 {
		return escape(string(v))
	}
	return "none"
}

// mntDir represents mount directory in a mount entry.
type mntDir string

func (v mntDir) String() string {
	if len(v) != 0 {
		return escape(string(v))
	}
	return "none"
}

// mntOptions represents mount options in a mount entry.
type mntOptions []string

func (v mntOptions) String() string {
	if len(v) != 0 {
		return escape(strings.Join(v, ","))
	}
	return "defaults"
}

// mntFsType represents file system type in a mount entry.
type mntFsType string

func (v mntFsType) String() string {
	if len(v) != 0 {
		return escape(string(v))
	}
	return "none"
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
	return fmt.Sprintf("%s %s %s %s %d %d",
		e.FsName, e.Dir, e.FsType, e.Options, e.Freq, e.PassNum)
}
