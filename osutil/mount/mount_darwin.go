// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package mount

// MountFlagsToOpts returns the symbolic representation of mount flags.
func MountFlagsToOpts(flags int) (opts []string, unknown int) {
	return nil, 0
}

// UnmountFlagsToOpts returns the symbolic representation of unmount flags.
func UnmountFlagsToOpts(flags int) (opts []string, unknown int) {
	return nil, 0
}
