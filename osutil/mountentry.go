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

package osutil

import (
	"fmt"
	"strconv"
	"strings"
	"syscall"
)

// MountEntry describes an /etc/fstab-like mount entry.
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
type MountEntry struct {
	Name    string
	Dir     string
	Type    string
	Options []string

	DumpFrequency   int
	CheckPassNumber int
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Equal checks if one entry is equal to another
func (e *MountEntry) Equal(o *MountEntry) bool {
	return (e.Name == o.Name && e.Dir == o.Dir && e.Type == o.Type &&
		equalStrings(e.Options, o.Options) && e.DumpFrequency == o.DumpFrequency &&
		e.CheckPassNumber == o.CheckPassNumber)
}

// escape replaces whitespace characters so that getmntent can parse it correctly.
var escape = strings.NewReplacer(
	" ", `\040`,
	"\t", `\011`,
	"\n", `\012`,
	"\\", `\134`,
).Replace

// unescape replaces escape sequences used by setmnt with whitespace characters.
var unescape = strings.NewReplacer(
	`\040`, " ",
	`\011`, "\t",
	`\012`, "\n",
	`\134`, "\\",
).Replace

func Escape(path string) string {
	return escape(path)
}

func Unescape(path string) string {
	return unescape(path)
}

func (e MountEntry) String() string {
	// Name represents name of the device in a mount entry.
	name := "none"
	if e.Name != "" {
		name = escape(e.Name)
	}
	// Dir represents mount directory in a mount entry.
	dir := "none"
	if e.Dir != "" {
		dir = escape(e.Dir)
	}
	// Type represents file system type in a mount entry.
	fsType := "none"
	if e.Type != "" {
		fsType = escape(e.Type)
	}
	// Options represents mount options in a mount entry.
	options := "defaults"
	if len(e.Options) != 0 {
		options = escape(strings.Join(e.Options, ","))
	}
	return fmt.Sprintf("%s %s %s %s %d %d",
		name, dir, fsType, options, e.DumpFrequency, e.CheckPassNumber)
}

// ParseMountEntry parses a fstab-like entry.
func ParseMountEntry(s string) (MountEntry, error) {
	var e MountEntry
	var err error
	var df, cpn int
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '\t' })
	// do all error checks before any assignments to `e'
	if len(fields) < 4 || len(fields) > 6 {
		return e, fmt.Errorf("expected between 4 and 6 fields, found %d", len(fields))
	}
	// Parse DumpFrequency if we have at least 5 fields
	if len(fields) > 4 {
		df, err = strconv.Atoi(fields[4])
		if err != nil {
			return e, fmt.Errorf("cannot parse dump frequency: %q", fields[4])
		}
	}
	// Parse CheckPassNumber if we have at least 6 fields
	if len(fields) > 5 {
		cpn, err = strconv.Atoi(fields[5])
		if err != nil {
			return e, fmt.Errorf("cannot parse check pass number: %q", fields[5])
		}
	}
	e.Name = unescape(fields[0])
	e.Dir = unescape(fields[1])
	e.Type = unescape(fields[2])
	e.Options = strings.Split(unescape(fields[3]), ",")
	e.DumpFrequency = df
	e.CheckPassNumber = cpn
	return e, nil
}

// OptsToCommonFlags converts mount options strings to a mount flag, returning unparsed flags.
// The unparsed flags will not contain any snapd-specific mount option, those
// starting with the string "x-snapd."
func MountOptsToCommonFlags(opts []string) (flags int, unparsed []string) {
	for _, opt := range opts {
		switch opt {
		case "ro":
			flags |= syscall.MS_RDONLY
		case "nosuid":
			flags |= syscall.MS_NOSUID
		case "nodev":
			flags |= syscall.MS_NODEV
		case "noexec":
			flags |= syscall.MS_NOEXEC
		case "sync":
			flags |= syscall.MS_SYNCHRONOUS
		case "remount":
			flags |= syscall.MS_REMOUNT
		case "mand":
			flags |= syscall.MS_MANDLOCK
		case "dirsync":
			flags |= syscall.MS_DIRSYNC
		case "noatime":
			flags |= syscall.MS_NOATIME
		case "nodiratime":
			flags |= syscall.MS_NODIRATIME
		case "bind":
			flags |= syscall.MS_BIND
		case "rbind":
			flags |= syscall.MS_BIND | syscall.MS_REC
		case "move":
			flags |= syscall.MS_MOVE
		case "silent":
			flags |= syscall.MS_SILENT
		case "acl":
			flags |= syscall.MS_POSIXACL
		case "private":
			flags |= syscall.MS_PRIVATE
		case "rprivate":
			flags |= syscall.MS_PRIVATE | syscall.MS_REC
		case "slave":
			flags |= syscall.MS_SLAVE
		case "rslave":
			flags |= syscall.MS_SLAVE | syscall.MS_REC
		case "shared":
			flags |= syscall.MS_SHARED
		case "rshared":
			flags |= syscall.MS_SHARED | syscall.MS_REC
		case "relatime":
			flags |= syscall.MS_RELATIME
		case "strictatime":
			flags |= syscall.MS_STRICTATIME
		default:
			if !strings.HasPrefix(opt, "x-snapd.") {
				unparsed = append(unparsed, opt)
			}
		}
	}
	return flags, unparsed
}

// OptsToFlags converts mount options strings to a mount flag.
func MountOptsToFlags(opts []string) (flags int, err error) {
	flags, unparsed := MountOptsToCommonFlags(opts)
	for _, opt := range unparsed {
		if !strings.HasPrefix(opt, "x-snapd.") {
			return 0, fmt.Errorf("unsupported mount option: %q", opt)
		}
	}
	return flags, nil
}

// OptStr returns the value part of a key=value mount option.
// The name of the option must not contain the trailing "=" character.
func (e *MountEntry) OptStr(name string) (string, bool) {
	prefix := name + "="
	for _, opt := range e.Options {
		if strings.HasPrefix(opt, prefix) {
			kv := strings.SplitN(opt, "=", 2)
			return kv[1], true
		}
	}
	return "", false
}

// OptBool returns true if a given mount option is present.
func (e *MountEntry) OptBool(name string) bool {
	for _, opt := range e.Options {
		if opt == name {
			return true
		}
	}
	return false
}

// XSnapdKindSymlink returns the string "x-snapd.kind=symlink".
func XSnapdKindSymlink() string {
	return "x-snapd.kind=symlink"
}

// XSnapdKindFile returns the string "x-snapd.kind=file".
func XSnapdKindFile() string {
	return "x-snapd.kind=file"
}

// XSnapdUser returns the string "x-snapd.user=%d".
func XSnapdUser(uid int) string {
	return fmt.Sprintf("x-snapd.user=%d", uid)
}

// XSnapdGroup returns the string "x-snapd.group=%d".
func XSnapdGroup(gid int) string {
	return fmt.Sprintf("x-snapd.group=%d", gid)
}

// XSnapdMode returns the string "x-snapd.mode=%#o".
func XSnapdMode(mode uint32) string {
	return fmt.Sprintf("x-snapd.mode=%#o", mode)
}

// XSnapdSymlink returns the string "x-snapd.symlink=%s".
func XSnapdSymlink(oldname string) string {
	return fmt.Sprintf("x-snapd.symlink=%s", oldname)
}
