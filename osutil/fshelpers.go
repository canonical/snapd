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
	"os"
	"syscall"

	"github.com/snapcore/snapd/osutil/sys"
)

// FindGIDOwning obtains UNIX group ID and name owning file `path`.
func FindGIDOwning(path string) (sys.GroupID, error) {
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		if err == syscall.ENOENT {
			return 0, os.ErrNotExist
		}
		return 0, err
	}

	return sys.GroupID(stat.Gid), nil
}
