// -*- Mode: Go; indent-tabs-mode: t -*-

//go:build !linux

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

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/cmd/snapd/cli"
)

func main() {
	argv0 := filepath.Base(os.Args[0])

	switch argv0 {
	case "snap":
		// we only expose the 'snap' command on non Linux so that folks can
		// pack snaps
		cli.Main()
	default:
		fmt.Fprintf(os.Stderr, "error: %q mode is not supported on this system\n", argv0)
		os.Exit(1)
	}
}
