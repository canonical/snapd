// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package syscheck

import "github.com/ddkwork/golibrary/mylog"

var checks []func() error

// CheckSystem ensures that the system is capable of running snapd and
// snaps. It probe for e.g. that cgroup support is available and squashfs
// files can be mounted.
//
// An error with details is returned if some check fails.
func CheckSystem() error {
	for _, f := range checks {
		mylog.Check(f())
	}

	return nil
}
