// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package backend

import "github.com/snapcore/snapd/systemd"

// ListMountControlMountPoints returns the active mount-control mount points for
// the given snap instance name.
func ListMountControlMountPoints(instanceName string) ([]string, error) {
	sysd := systemd.New(systemd.SystemMode, nil)
	return sysd.ListMountUnits(instanceName, "mount-control")
}
