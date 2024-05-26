// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type cmdRoutineFileAccess struct {
	clientMixin
	FileAccessOptions struct {
		Snap installedSnapName
		Path flags.Filename
	} `positional-args:"true" required:"true"`
}

var (
	shortRoutineFileAccessHelp = i18n.G("Return information about file access by a snap")
	longRoutineFileAccessHelp  = i18n.G(`
The file-access command returns information about a snap's file system access.

This command is used by the xdg-document-portal service to identify
files that do not need to be proxied to provide access within
confinement.

File paths are interpreted as host file system paths.  The tool may
return false negatives (e.g. report that a file path is unreadable,
despite being readable under a different path).  It also does not
check if file system permissions would render a file unreadable.
`)
)

func init() {
	addRoutineCommand("file-access", shortRoutineFileAccessHelp, longRoutineFileAccessHelp, func() flags.Commander {
		return &cmdRoutineFileAccess{}
	}, nil, []argDesc{
		{
			// TRANSLATORS: This needs to begin with < and end with >
			name: i18n.G("<snap>"),
			// TRANSLATORS: This should not start with a lowercase letter.
			desc: i18n.G("Snap name"),
		},
		{
			// TRANSLATORS: This needs to begin with < and end with >
			name: i18n.G("<path>"),
			// TRANSLATORS: This should not start with a lowercase letter.
			desc: i18n.G("File path"),
		},
	})
}

func (x *cmdRoutineFileAccess) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	snapName := string(x.FileAccessOptions.Snap)
	path := string(x.FileAccessOptions.Path)

	snap, _ := mylog.Check3(x.client.Snap(snapName))

	// Check whether the snap has home or removable-media plugs connected
	connections := mylog.Check2(x.client.Connections(&client.ConnectionOptions{
		Snap: snap.Name,
	}))

	var hasHome, hasRemovableMedia bool
	for _, conn := range connections.Established {
		if conn.Plug.Snap != snap.Name {
			continue
		}
		switch conn.Interface {
		case "home":
			hasHome = true
		case "removable-media":
			hasRemovableMedia = true
		}
	}

	access := mylog.Check2(x.checkAccess(snap, hasHome, hasRemovableMedia, path))

	fmt.Fprintln(Stdout, access)
	return nil
}

type FileAccess string

const (
	FileAccessHidden    FileAccess = "hidden"
	FileAccessReadOnly  FileAccess = "read-only"
	FileAccessReadWrite FileAccess = "read-write"
)

func splitPathAbs(path string) ([]string, error) {
	// Abs also cleans the path, removing any ".." components
	path := mylog.Check2(filepath.Abs(path))

	// Ignore the empty component before the first slash
	return strings.Split(path, string(os.PathSeparator))[1:], nil
}

func pathHasPrefix(path, prefix []string) bool {
	if len(path) < len(prefix) {
		return false
	}
	for i := range prefix {
		if path[i] != prefix[i] {
			return false
		}
	}
	return true
}

func (x *cmdRoutineFileAccess) checkAccess(snap *client.Snap, hasHome, hasRemovableMedia bool, path string) (FileAccess, error) {
	// Classic confinement snaps run in the host system namespace,
	// so can see everything.
	if snap.Confinement == client.ClassicConfinement {
		return FileAccessReadWrite, nil
	}

	pathParts := mylog.Check2(splitPathAbs(path))

	// Snaps have access to $SNAP_DATA and $SNAP_COMMON
	if pathHasPrefix(pathParts, []string{"var", "snap", snap.Name}) {
		if len(pathParts) == 3 {
			return FileAccessReadOnly, nil
		}
		switch pathParts[3] {
		case "common", "current", snap.Revision.String():
			return FileAccessReadWrite, nil
		default:
			return FileAccessReadOnly, nil
		}
	}

	// Snaps with removable-media plugged can access removable
	// media mount points.
	if hasRemovableMedia {
		if pathHasPrefix(pathParts, []string{"mnt"}) || pathHasPrefix(pathParts, []string{"media"}) || pathHasPrefix(pathParts, []string{"run", "media"}) {
			return FileAccessReadWrite, nil
		}
	}

	usr := mylog.Check2(userCurrent())

	home := mylog.Check2(splitPathAbs(usr.HomeDir))

	if pathHasPrefix(pathParts, home) {
		pathInHome := pathParts[len(home):]
		// Snaps have access to $SNAP_USER_DATA and $SNAP_USER_COMMON
		if pathHasPrefix(pathInHome, []string{"snap"}) {
			if !pathHasPrefix(pathInHome, []string{"snap", snap.Name}) {
				return FileAccessHidden, nil
			}
			if len(pathInHome) < 3 {
				return FileAccessReadOnly, nil
			}
			switch pathInHome[2] {
			case "common", "current", snap.Revision.String():
				return FileAccessReadWrite, nil
			default:
				return FileAccessReadOnly, nil
			}
		}
		// If the home interface is connected, the snap has
		// access to other files in home, except top-level dot
		// files.
		if hasHome {
			if len(pathInHome) == 0 || !strings.HasPrefix(pathInHome[0], ".") {
				return FileAccessReadWrite, nil
			}
		}
	}

	return FileAccessHidden, nil
}
