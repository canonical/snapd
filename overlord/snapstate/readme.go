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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

const snapREADME = `
This directory is used by snapd to present installed snap packages. While the
files inside may seem large almost no space is consumed here. The actual space
is used by heavily-compressed .snap files stored in /var/lib/snapd/snap

For more information please visit: https://forum.snapcraft.io/t/the-snap-directory/
`

func writeSnapReadme() error {
	const fname = "README"
	content := map[string]*osutil.FileState{
		fname: {Content: []byte(snapREADME), Mode: 0644},
	}
	if err := os.MkdirAll(dirs.SnapMountDir, 0755); err != nil {
		return err
	}
	// NOTE: We are using EnsureDirState to not unconditionally write to flash
	// and thus prolong life of the device.
	_, _, err := osutil.EnsureDirState(dirs.SnapMountDir, fname, content)
	return err
}
