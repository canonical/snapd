// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package main

import (
	"fmt"
	"os"

	"github.com/snapcore/snapd/cmd/snap-install/install"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Printf("usage: %s <gadget root> <block device>\n", os.Args[0])
		os.Exit(0)
	}

	gadgetRoot := os.Args[1]
	device := os.Args[2]

	options := &install.Options{
		// FIXME: get this from a command line parameter
		Encrypt: true,
	}

	inst := install.New(gadgetRoot, device, options)
	if err := inst.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
