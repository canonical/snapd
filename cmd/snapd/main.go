// -*- Mode: Go; indent-tabs-mode: t -*-

//go:build linux

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
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/cmd/snapd/cli"
	"github.com/snapcore/snapd/cmd/snapd/daemon"
)

func main() {
	argv0 := filepath.Base(os.Args[0])

	// dispatch the binary multi entry point
	// TODO add snap-preseed
	switch argv0 {
	case "snapd":
		daemon.Main()
	default:
		// "snap" needs to be handled last, as it's a special entrypoint for
		// snap application execution through symlinks at /snap/bin/<name>
		cli.Main()
	}
}
