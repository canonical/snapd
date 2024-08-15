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

// ParseMountEntry parses a fstab-like entry.
func ParseMountEntry(s string) (MountEntry, error) {
	var e MountEntry
	var err error
	var df, cpn int
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '\t' })
	// Look for any inline comments. The first field that starts with '#' is a comment.
	for i, field := range fields {
		if strings.HasPrefix(field, "#") {
			fields = fields[:i]
			break
		}
	}
	// Do all error checks before any assignments to `e'
	if len(fields) < 3 {
		return e, fmt.Errorf("expected at least 3 fields, found %d", len(fields))
	}
	e.Name = unescape(fields[0])
	e.Dir = unescape(fields[1])
	e.Type = unescape(fields[2])
	tailFieldIndex := 4
	// Parse Options if we have at least 4 fields
	if len(fields) > 3 {
		escapedOptions := fields[3]
		if len(fields) > 6 {
			// XXX: Because spaces in options were never expected, the problem
			// of parsing an entry with space-unescaped options, while allowing
			// the dump frequency and check pass number to be optional, is
			// undecidable.
			//
			// An entry with the following text:
			//      name dir type options 5
			// May be then parsed as either (with quoting representing how things are parsed):
			//      name dir type "options 5"
			// Or as:
			//      name dir type "options" 5
			//
			// The problem is further complicated by the 2nd optional field, so
			// imagine seeing 5 6 instead of just 5 in the example above.
			//
			// We try to be pragmatic and observe the following properties:
			//
			// - The /proc/PID/mounts file does not hide optional fields.
			// - The /etc/fstab file doesn't usually use unescaped spaces.
			//
			// As such, the ambiguous cases with five or six fields are handled
			// as if mount options (fs_mntops in fstab(5)) had no spaces.
			tailFieldIndex = len(fields) - 2
			escapedOptions = strings.Join(fields[3:tailFieldIndex], " ")
		}
		e.Options = strings.Split(unescape(escapedOptions), ",")
	}

	// Parse DumpFrequency if we have one tail field.
	if len(fields) > tailFieldIndex {
		f := fields[tailFieldIndex]
		df, err = strconv.Atoi(f)
		if err != nil {
			return e, fmt.Errorf("cannot parse dump frequency: %q", f)
		}
	}
	e.DumpFrequency = df
	// Parse CheckPassNumber if we have two tail fields.
	if len(fields) > tailFieldIndex+1 {
		f := fields[tailFieldIndex+1]
		cpn, err = strconv.Atoi(f)
		if err != nil {
			return e, fmt.Errorf("cannot parse check pass number: %q", f)
		}
	}
	e.CheckPassNumber = cpn
	return e, nil
}

// MountOptsToCommonFlags converts mount options strings to a mount flag,
// returning unparsed flags. The unparsed flags will not contain any snapd-
// specific mount option, those starting with the string "x-snapd."
func MountOptsToCommonFlags(opts []string) (flags int, unparsed []string) {
	for _, opt := range opts {
		switch opt {
		case "rw":
			// There's no flag for rw
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

// MountOptsToFlags converts mount options strings to a mount flag.
func MountOptsToFlags(opts []string) (flags int, err error) {
	flags, unparsed := MountOptsToCommonFlags(opts)
	for _, opt := range unparsed {
		if !strings.HasPrefix(opt, "x-snapd.") {
			return 0, fmt.Errorf("unsupported mount option: %q", opt)
		}
	}
	return flags, nil
}
