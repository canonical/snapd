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
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
)

const snapREADME = `
This directory is used by snapd to present installed snap packages. While the
files inside may seem large almost no space is consumed here. The actual space
is used by heavily-compressed .snap files stored in /var/lib/snapd/snap

For more information please visit: https://forum.snapcraft.io/t/the-snap-directory/
`

func writeSnapReadme() error {
	f := filepath.Join(dirs.SnapMountDir, "README")
	if err := os.MkdirAll(dirs.SnapMountDir, 0755); err != nil {
		return err
	}
	return ioutil.WriteFile(f, []byte(snapREADME), 0644)
}
