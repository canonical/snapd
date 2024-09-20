// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package clientutil

import (
	"path/filepath"
	"strconv"

	"github.com/snapcore/snapd/dirs"
)

// AvailableUserSessions returns a list of available user-session targets for
// snapd, by probing the available snapd-session-agent sockets in the
// XDG runtime directory.
func AvailableUserSessions() ([]int, error) {
	sockets, err := filepath.Glob(filepath.Join(dirs.XdgRuntimeDirGlob, "snapd-session-agent.socket"))
	if err != nil {
		return nil, err
	}

	var uids []int
	for _, sock := range sockets {
		uidStr := filepath.Base(filepath.Dir(sock))
		uid, err := strconv.Atoi(uidStr)
		if err != nil {
			// Ignore directories that do not
			// appear to be valid XDG runtime dirs
			// (i.e. /run/user/NNNN).
			continue
		}
		uids = append(uids, uid)
	}
	return uids, nil
}
