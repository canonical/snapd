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

package snapstate

import (
	"os"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

const snapREADME = `
This directory presents installed snap packages.

It has the following structure:

@SNAP_MOUNT_DIR@/bin                   - Symlinks to snap applications.
@SNAP_MOUNT_DIR@/<snapname>/<revision> - Mountpoint for snap content.
@SNAP_MOUNT_DIR@/<snapname>/current    - Symlink to current revision, if enabled.

DISK SPACE USAGE

The disk space consumed by the content under this directory is
minimal as the real snap content never leaves the .snap file.
Snaps are *mounted* rather than unpacked.

For further details please visit
https://forum.snapcraft.io/t/the-snap-directory/2817
`

func snapReadme() string {
	return strings.Replace(snapREADME, "@SNAP_MOUNT_DIR@", dirs.SnapMountDir, -1)
}

func writeSnapReadme() error {
	const fname = "README"
	content := map[string]osutil.FileState{
		fname: &osutil.MemoryFileState{Content: []byte(snapReadme()), Mode: 0444},
	}
	mylog.Check(os.MkdirAll(dirs.SnapMountDir, 0755))

	// NOTE: We are using EnsureDirState to not unconditionally write to flash
	// and thus prolong life of the device.
	_, _ := mylog.Check3(osutil.EnsureDirState(dirs.SnapMountDir, fname, content))
	return err
}
