// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package snap

import (
	"github.com/snapcore/snapd/strutil"
)

var snapIDsSnapd = []string{
	// production
	"PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4",
	// staging
	"Z44rtQD1v4r1LXGPCDZAJO3AOw1EDGqy",
}

func IsSnapd(snapID string) bool {
	return strutil.ListContains(snapIDsSnapd, snapID)
}

func MockSnapdSnapID(snapID string) (restore func()) {
	old := snapIDsSnapd
	snapIDsSnapd = append(snapIDsSnapd, snapID)
	return func() {
		snapIDsSnapd = old
	}
}
