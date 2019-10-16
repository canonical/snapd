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

	"github.com/snapcore/snapd/cmd/snap-recovery/recover"
	"github.com/snapcore/snapd/osutil"
)

func run(args []string) error {
	if !osutil.GetenvBool("SNAPPY_TESTING") {
		return fmt.Errorf("cannot use outside of tests yet")
	}
	if os.Getuid() != 0 {
		return fmt.Errorf("please run as root")
	}
	if len(os.Args) < 3 {
		// XXX: slightly ugly to return usage as an error but ok for now
		return fmt.Errorf("usage: %s <gadget root> <block device>\n", os.Args[0])
	}

	gadgetRoot := os.Args[1]
	device := os.Args[2]
	options := &recover.Options{}

	recov := recover.New(gadgetRoot, device, options)
	if err := recov.Run(); err != nil {
		return err
	}

	return nil
}

func main() {
	err := run(os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
