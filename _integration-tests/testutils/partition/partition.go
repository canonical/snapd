// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package partition

import (
	"gopkg.in/check.v1"

	"launchpad.net/snappy/_integration-tests/testutils/cli"
)

var execCommand = cli.ExecCommand

// MakeWritable remounts a path with read and write permissions.
func MakeWritable(c *check.C, path string) {
	execCommand(c, "sudo", "mount", "-o", "remount,rw", path)
}

// MakeReadonly remounts a path with only read permissions.
func MakeReadonly(c *check.C, path string) {
	execCommand(c, "sudo", "mount", "-o", "remount,ro", path)
}
