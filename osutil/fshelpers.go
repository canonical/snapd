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

	"github.com/ddkwork/golibrary/mylog"
)

// FindGidOwning obtains UNIX group ID and name owning file `path`.
func FindGidOwning(path string) (uint64, error) {
	var stat syscall.Stat_t
	mylog.Check(syscall.Stat(path, &stat))

	gid := uint64(stat.Gid)
	return gid, nil
}
