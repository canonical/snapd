// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

	"github.com/snapcore/snapd/cmd/snapctl/ctl"
	"github.com/snapcore/snapd/cmd/snapctl/snapexec"
)

func main() {
	argv0 := filepath.Base(os.Args[0])

	// dispatch the binary multi entry point
	switch argv0 {
	case "snap-exec":
		snapexec.Main()
	default: // "snapctl"
		ctl.Main()
	}
}
