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

// info produces the SNAPD_PATCH_LEVEL line for /usr/lib/snapd/info so that a
// candidate snapd snap can be inspected for its patch level before it is
// linked, without requiring the daemon to start.
package main

import (
	"fmt"

	"github.com/snapcore/snapd/overlord/patch"
)

func main() {
	fmt.Printf("SNAPD_PATCH_LEVEL=%d\n", patch.Level)
}
