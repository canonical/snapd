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

package preseed

import (
	"github.com/snapcore/snapd/testutil"
)

var PermitedSysfsOverlays = permitedSysfsOverlays

// MockSysfsRootDir replaces the root the live sysfs tree is read from, so that
// tests can capture a directory mimicking a device instead of the test runner.
func MockSysfsRootDir(newDir string) (restore func()) {
	r := testutil.Backup(&sysfsRootDir)
	sysfsRootDir = newDir
	return r
}
