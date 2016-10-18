// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

// snapbuild is a minimal executable wrapper around snap building to use for integration tests that need to build snaps under sudo.
package main

import (
	"fmt"
	"os"

	"github.com/snapcore/snapd/snap/snaptest"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "snapbuild: expected sourceDir and targetDir\n")
		os.Exit(1)
	}

	snapPath, err := snaptest.BuildSquashfsSnap(os.Args[1], os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "snapbuild: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "built: %s\n", snapPath)
}
