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
	"github.com/snapcore/snapd/cmd/snapd/preseedtool"
)

func main() {
	argv0 := filepath.Base(os.Args[0])

	switch argv0 {
	case "snapd":
		// Tool dispatch: the C wrapper (snapd-tool-wrap) sets
		// argv[0]="snapd" and argv[1]=<tool-name>. Check argv[1]
		// for known tool names before falling through to the daemon.
		if len(os.Args) > 1 {
			switch os.Args[1] {
			case "snap-preseed":
				os.Args = append(os.Args[:1], os.Args[2:]...)
				preseedtool.Main()
				return
			}
		}
		daemon.Main()
	default:
		cli.Main()
	}
}
