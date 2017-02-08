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
	FsName  MntFsName
	Dir     MntDir
	FsType  MntFsType
	Options MntOptions
	Freq    int
	PassNum int
}

// MntFsName represents name of the device in a mount entry.
type MntFsName string

func (v MntFsName) String() string {
	if len(v) != 0 {
		return escape(string(v))
	}
	return "none"
}

// MntDir represents mount directory in a mount entry.
type MntDir string

func (v MntDir) String() string {
	if len(v) != 0 {
		return escape(string(v))
	}
	return "none"
}

// MntOptions represents mount options in a mount entry.
type MntOptions []string

func (v MntOptions) String() string {
	if len(v) != 0 {
		return escape(strings.Join(v, ","))
	}
	return "defaults"
}

// MntFsType represents file system type in a mount entry.
type MntFsType string

func (v MntFsType) String() string {
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
