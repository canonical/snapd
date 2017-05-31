// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

var (
	Stdout = os.Stdout
	Stderr = os.Stderr
)

func main() {
	var err error

	// FIXME: use proper cmdline parser
	if len(os.Args) < 2 {
		fmt.Fprintf(Stderr, "snap-repair is used to repair\n")
		os.Exit(10)
	}

	cmd := os.Args[1]
	switch cmd {
	case "run":
		err = runRepair()
	default:
		err = fmt.Errorf("unknown command")
	}

	if err != nil {
		fmt.Fprintf(Stderr, "cannot %s: %s\n", cmd, err)
		os.Exit(1)
	}
}
