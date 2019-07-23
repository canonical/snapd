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
)

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("must specify recovery system directory")
	}

	rsys, err := newRecoverySystem(os.Args[1])
	if err != nil {
		return err
	}

	if err := rsys.loadAssertions(); err != nil {
		return err
	}

	// TODO:
	//  - verify base
	//  - verify kernel
	//  - do we need a way to cross check kernel snap vs extracted
	//    kernel image?
	// any other snaps? other snaps should be verified by snapd itself
	// over install/seeding?

	// verify gadget
	if err := rsys.verifyGadget(); err != nil {
		return err
	}

	fmt.Println("series:", rsys.model.Series())
	fmt.Println("brand-id:", rsys.model.BrandID())
	fmt.Println("model:", rsys.model.Model())
	// TODO: other things to measure

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)

	}
}
