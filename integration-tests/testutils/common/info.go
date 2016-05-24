// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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

package common

import (
	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

// dependency aliasing
var snappyCmd = "/usr/bin/snappy"

// Release returns the release of the current snappy image
func Release(c *check.C) string {
	// FIXME: use `snap info` once it is available again
	if osutil.FileExists(snappyCmd) {
		return "15.04"
	}

	return "rolling"
}
